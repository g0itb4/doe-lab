package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/auth"
	"doelab/api/internal/domain"
)

// Backstops is the emergency backstop: an operator overrides the envelopes of
// a feeder's sites with a fixed export limit, at once, until it is cleared.
type Backstops struct {
	store Store
	bus   EnvelopeBus
	clock Clock
}

// NewBackstops builds the service.
func NewBackstops(store Store, bus EnvelopeBus, clock Clock) *Backstops {
	return &Backstops{store: store, bus: bus, clock: clock}
}

// Get returns a backstop and the sites it covers.
func (s *Backstops) Get(ctx context.Context, id uuid.UUID) (domain.BackstopEvent, []uuid.UUID, error) {
	event, err := s.store.GetBackstopEvent(ctx, id)
	if err != nil {
		return domain.BackstopEvent{}, nil, err
	}
	sites, err := s.store.ListBackstopEventSiteIDs(ctx, id)
	return event, sites, err
}

// List returns a page of a feeder's backstops, newest first.
func (s *Backstops) List(ctx context.Context, feederID uuid.UUID, p domain.Page) ([]domain.BackstopEvent, string, error) {
	if _, err := s.store.GetFeeder(ctx, feederID); err != nil {
		return nil, "", fmt.Errorf("feeder %s: %w", feederID, err)
	}
	return s.store.ListBackstopEvents(ctx, feederID, page(p))
}

// Defaults for a feeder that has no config yet.
const (
	defaultInterval = 30 * time.Minute
	defaultHorizon  = 48
)

// Trigger starts a backstop on a feeder. siteIDs are the sites to cover;
// none means every enrolled site of the feeder.
//
// It takes effect at once: from the interval in force onwards, each covered
// site's envelope is replaced by one with the backstop's export limit, for as
// far ahead as the feeder's horizon, and the open subscriptions are told. A
// feeder has one active backstop at most.
func (s *Backstops) Trigger(ctx context.Context, event domain.BackstopEvent, siteIDs []uuid.UUID) (domain.BackstopEvent, []uuid.UUID, error) {
	now := s.clock.Now()
	var out domain.BackstopEvent
	err := s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		if _, err := r.GetFeeder(ctx, event.FeederID); err != nil {
			return fmt.Errorf("%w: feeder %s does not exist", domain.ErrFailedPrecondition, event.FeederID)
		}
		sites, err := r.ListAllSites(ctx, event.FeederID)
		if err != nil {
			return err
		}
		byID := make(map[uuid.UUID]domain.Site, len(sites))
		for _, site := range sites {
			byID[site.ID] = site
		}
		if len(siteIDs) == 0 {
			for _, site := range sites {
				if site.ExportCapW > 0 {
					siteIDs = append(siteIDs, site.ID)
				}
			}
			if len(siteIDs) == 0 {
				return fmt.Errorf("%w: feeder %s has no enrolled site to back-stop", domain.ErrFailedPrecondition, event.FeederID)
			}
		}
		for _, id := range siteIDs {
			if _, ok := byID[id]; !ok {
				return fmt.Errorf("%w: site %s is not a site of feeder %s", domain.ErrFailedPrecondition, id, event.FeederID)
			}
		}

		event.TriggeredBy, event.TriggeredAt = auth.ActorName(ctx), now
		if out, err = r.CreateBackstopEvent(ctx, event, siteIDs); err != nil {
			if errors.Is(err, domain.ErrAlreadyExists) {
				return fmt.Errorf("%w: the feeder already has an active backstop; clear it first", domain.ErrAlreadyExists)
			}
			return err
		}
		return s.cover(ctx, r, out, siteIDs, byID, now)
	})
	if err != nil {
		return domain.BackstopEvent{}, nil, err
	}
	// After the commit, so a device that looks finds the backstop envelope.
	_ = s.bus.Notify(ctx, siteIDs)
	return out, siteIDs, nil
}

// cover writes backstop envelopes for every interval from the one in force
// to the feeder's horizon, for every covered site that does not have one
// there already.
func (s *Backstops) cover(ctx context.Context, r Repos, event domain.BackstopEvent, siteIDs []uuid.UUID, byID map[uuid.UUID]domain.Site, now time.Time) error {
	interval, horizon := defaultInterval, defaultHorizon
	config, err := r.GetActiveEnvelopeConfig(ctx, event.FeederID)
	switch {
	case err == nil:
		interval, horizon = time.Duration(config.IntervalMinutes)*time.Minute, int(config.HorizonIntervals)
	case !errors.Is(err, domain.ErrNotFound):
		return err
	}

	// What is active now, per site and interval: the import limit to keep,
	// and whether the backstop already holds the interval.
	active, err := r.ListActiveEnvelopes(ctx, siteIDs, now)
	if err != nil {
		return err
	}
	type slot struct {
		site uuid.UUID
		from int64
	}
	current := make(map[slot]domain.Envelope, len(active))
	for _, e := range active {
		current[slot{e.SiteID, e.ValidFrom.Unix()}] = e
	}

	start := now.Truncate(interval)
	var envelopes []domain.Envelope
	for k := range horizon {
		from := start.Add(time.Duration(k) * interval)
		for _, id := range siteIDs {
			have, exists := current[slot{id, from.Unix()}]
			if exists && have.BackstopEventID != nil && *have.BackstopEventID == event.ID {
				continue
			}
			// A backstop limits export. Import keeps the limit it had, or
			// the site's own cap where there was none.
			importW := byID[id].ImportCapW
			if exists {
				importW = have.ImportLimitW
			}
			envelopes = append(envelopes, domain.Envelope{
				SiteID: id, ValidFrom: from, ValidTo: from.Add(interval),
				ExportLimitW: event.ExportLimitW, ImportLimitW: importW,
				Source: domain.SourceBackstop, BackstopEventID: &event.ID,
				ExportBinding: domain.BindingNone, ImportBinding: domain.BindingNone,
			})
		}
	}
	if len(envelopes) == 0 {
		return nil
	}
	_, _, err = r.ReplaceEnvelopes(ctx, envelopes)
	return err
}

// Clear ends a backstop and gives the sites back the envelopes the engine
// last computed for them, from the interval in force onwards. Clearing a
// backstop that is already cleared changes nothing.
func (s *Backstops) Clear(ctx context.Context, id uuid.UUID) (domain.BackstopEvent, error) {
	now := s.clock.Now()
	var out domain.BackstopEvent
	var siteIDs []uuid.UUID
	err := s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		event, err := r.GetBackstopEvent(ctx, id)
		if err != nil {
			return err
		}
		if event.ClearedAt != nil {
			out = event
			return nil
		}
		if siteIDs, err = r.ListBackstopEventSiteIDs(ctx, id); err != nil {
			return err
		}
		if out, err = r.ClearBackstopEvent(ctx, id, auth.ActorName(ctx), now); err != nil {
			return err
		}

		// End the backstop's envelopes, then put the engine's back. An
		// interval the engine never covered is left with no envelope, and a
		// device falls back to its default there until the next run.
		if _, err := r.SupersedeBackstopEnvelopes(ctx, id, now); err != nil {
			return err
		}
		latest, err := r.ListLatestEngineEnvelopes(ctx, siteIDs, now)
		if err != nil {
			return err
		}
		restored := make([]domain.Envelope, len(latest))
		for i, e := range latest {
			restored[i] = domain.Envelope{
				SiteID: e.SiteID, ValidFrom: e.ValidFrom, ValidTo: e.ValidTo,
				ExportLimitW: e.ExportLimitW, ImportLimitW: e.ImportLimitW,
				Source: domain.SourceEngine, EnvelopeRunID: e.EnvelopeRunID,
				ExportBinding: e.ExportBinding, ExportBindingElement: e.ExportBindingElement,
				ImportBinding: e.ImportBinding, ImportBindingElement: e.ImportBindingElement,
			}
		}
		if len(restored) == 0 {
			return nil
		}
		_, _, err = r.ReplaceEnvelopes(ctx, restored)
		return err
	})
	if err != nil {
		return domain.BackstopEvent{}, err
	}
	_ = s.bus.Notify(ctx, siteIDs)
	return out, nil
}

// Extend keeps every active backstop covering its feeder's horizon as time
// moves on: a backstop that outlasts the envelopes written when it was
// triggered must not lapse. The API runs it on a timer, with Compliance's
// sweep. It returns the number of active backstops.
func (s *Backstops) Extend(ctx context.Context) (int, error) {
	now := s.clock.Now()
	active := 0
	for token := ""; ; {
		feeders, next, err := s.store.ListFeeders(ctx, domain.Page{Size: DefaultPageSize, Token: token})
		if err != nil {
			return active, err
		}
		for _, feeder := range feeders {
			err := s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
				event, err := r.GetActiveBackstopEvent(ctx, feeder.ID)
				if errors.Is(err, domain.ErrNotFound) {
					return nil
				}
				if err != nil {
					return err
				}
				active++
				siteIDs, err := r.ListBackstopEventSiteIDs(ctx, event.ID)
				if err != nil {
					return err
				}
				sites, err := r.ListAllSites(ctx, feeder.ID)
				if err != nil {
					return err
				}
				byID := make(map[uuid.UUID]domain.Site, len(sites))
				for _, site := range sites {
					byID[site.ID] = site
				}
				return s.cover(ctx, r, event, siteIDs, byID, now)
			})
			if err != nil {
				return active, fmt.Errorf("feeder %s: %w", feeder.Code, err)
			}
		}
		if token = next; token == "" {
			return active, nil
		}
	}
}
