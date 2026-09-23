package service

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/auth"
	"doelab/api/internal/domain"
)

// Envelopes publishes operating envelopes and dispatches them to devices.
type Envelopes struct {
	store Store
	bus   EnvelopeBus
	clock Clock
	// Keepalive is how often a subscription repeats its envelope when
	// nothing changes, on the wall clock.
	Keepalive time.Duration
	// after is time.After, replaceable in tests.
	after func(time.Duration) <-chan time.Time
}

// DefaultKeepalive is the keepalive period of a subscription.
const DefaultKeepalive = 15 * time.Second

// NewEnvelopes builds the service.
func NewEnvelopes(store Store, bus EnvelopeBus, clock Clock) *Envelopes {
	return &Envelopes{store: store, bus: bus, clock: clock, Keepalive: DefaultKeepalive, after: time.After}
}

// Current returns the envelope in force for a site at an instant of feeder
// time; a zero instant means now. It returns nil when the site has no
// envelope then, which is not an error.
func (s *Envelopes) Current(ctx context.Context, siteID uuid.UUID, at time.Time) (*domain.Envelope, error) {
	if _, err := s.store.GetSite(ctx, siteID); err != nil {
		return nil, err
	}
	return s.current(ctx, siteID, at)
}

// CurrentByNMI is Current for a site named by its NMI.
func (s *Envelopes) CurrentByNMI(ctx context.Context, nmi string, at time.Time) (*domain.Envelope, error) {
	site, err := s.store.GetSiteByNMI(ctx, nmi)
	if err != nil {
		return nil, err
	}
	return s.current(ctx, site.ID, at)
}

func (s *Envelopes) current(ctx context.Context, siteID uuid.UUID, at time.Time) (*domain.Envelope, error) {
	if at.IsZero() {
		at = s.clock.Now()
	}
	e, err := s.store.GetCurrentEnvelope(ctx, siteID, at)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// List returns a page of a site's envelopes whose interval starts in
// [from, to), in time order.
func (s *Envelopes) List(ctx context.Context, siteID uuid.UUID, from, to time.Time, includeSuperseded bool, p domain.Page) ([]domain.Envelope, string, error) {
	if _, err := s.store.GetSite(ctx, siteID); err != nil {
		return nil, "", err
	}
	return s.store.ListEnvelopes(ctx, siteID, from, to, includeSuperseded, page(p))
}

// PublishResult is what a publish did.
type PublishResult struct {
	Published  int
	Superseded int
	// Replayed is true when the key had been used with the same batch
	// before, and nothing was written.
	Replayed bool
}

// publishScope is the idempotency scope of Publish.
const publishScope = "publish_envelopes"

// Publish stores a batch of envelopes from a run as the active envelopes of
// their sites and intervals, superseding what was active there, and tells the
// open subscriptions.
//
// The batch is all or nothing. It is refused when the run is not running, a
// site is not one of the run's feeder, an interval is not one of the run's
// horizon and config, or an interval appears twice. The same key with the
// same batch writes nothing; the same key with another batch is refused.
func (s *Envelopes) Publish(ctx context.Context, runID uuid.UUID, key string, envelopes []domain.Envelope) (PublishResult, error) {
	var result PublishResult
	hash := hashBatch(runID, envelopes)

	err := s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		run, err := r.GetEnvelopeRun(ctx, runID)
		if err != nil {
			return fmt.Errorf("%w: envelope run %s does not exist", domain.ErrFailedPrecondition, runID)
		}

		held, claimed, err := r.ClaimIdempotencyKey(ctx, domain.IdempotencyKey{
			Scope: publishScope, Key: key, RequestHash: hash, EnvelopeRunID: &runID,
		})
		if err != nil {
			return err
		}
		if !claimed {
			if string(held.RequestHash) != string(hash) {
				return fmt.Errorf("%w: idempotency key %q was used for a different batch", domain.ErrAlreadyExists, key)
			}
			result.Replayed = true
			return nil
		}

		if run.Status != domain.RunRunning {
			return fmt.Errorf("%w: envelope run %s is %s, not running", domain.ErrFailedPrecondition, runID, run.Status)
		}
		config, err := r.GetEnvelopeConfig(ctx, run.EnvelopeConfigID)
		if err != nil {
			return err
		}
		sites, err := r.ListAllSites(ctx, run.FeederID)
		if err != nil {
			return err
		}
		inFeeder := make(map[uuid.UUID]string, len(sites))
		for _, site := range sites {
			inFeeder[site.ID] = site.NMI
		}

		interval := time.Duration(config.IntervalMinutes) * time.Minute
		for i := range envelopes {
			e := &envelopes[i]
			if _, ok := inFeeder[e.SiteID]; !ok {
				return fmt.Errorf("%w: site %s is not a site of the run's feeder", domain.ErrFailedPrecondition, e.SiteID)
			}
			if e.ValidTo.Sub(e.ValidFrom) != interval || e.ValidFrom.UnixNano()%int64(interval) != 0 {
				return fmt.Errorf("%w: envelope for %s is not a %d-minute interval on the grid",
					domain.ErrInvalid, e.ValidFrom.UTC().Format(time.RFC3339), config.IntervalMinutes)
			}
			if e.ValidFrom.Before(run.HorizonFrom) || e.ValidTo.After(run.HorizonTo) {
				return fmt.Errorf("%w: envelope for %s is outside the run's horizon",
					domain.ErrInvalid, e.ValidFrom.UTC().Format(time.RFC3339))
			}
			e.Source, e.EnvelopeRunID, e.BackstopEventID = domain.SourceEngine, &runID, nil
		}

		_, superseded, err := r.ReplaceEnvelopes(ctx, envelopes)
		if errors.Is(err, domain.ErrAlreadyExists) {
			return fmt.Errorf("%w: the batch has two envelopes for one site and interval", domain.ErrInvalid)
		}
		if err != nil {
			return err
		}
		result.Published, result.Superseded = len(envelopes), superseded
		return r.AddEnvelopeRunCount(ctx, runID, int32(len(envelopes))) //nolint:gosec // G115: a batch is at most 5000
	})
	if err != nil || result.Replayed {
		return result, err
	}

	// After the commit, so a subscriber that looks finds the new rows. A
	// signal that is lost costs a subscriber nothing but time: it looks
	// again at its next keepalive.
	seen := map[uuid.UUID]bool{}
	var changed []uuid.UUID
	for _, e := range envelopes {
		if !seen[e.SiteID] {
			seen[e.SiteID] = true
			changed = append(changed, e.SiteID)
		}
	}
	_ = s.bus.Notify(ctx, changed)
	return result, nil
}

// hashBatch is the SHA-256 of a batch: what makes "the same request" exact.
// It covers every field that the client sets.
func hashBatch(runID uuid.UUID, envelopes []domain.Envelope) []byte {
	h := sha256.New()
	h.Write(runID[:])
	var buf [8]byte
	number := func(v uint64) {
		binary.BigEndian.PutUint64(buf[:], v)
		h.Write(buf[:])
	}
	text := func(s string) {
		number(uint64(len(s)))
		h.Write([]byte(s))
	}
	for _, e := range envelopes {
		h.Write(e.SiteID[:])
		number(uint64(e.ValidFrom.UnixNano()))
		number(uint64(e.ValidTo.UnixNano()))
		number(math.Float64bits(e.ExportLimitW))
		number(math.Float64bits(e.ImportLimitW))
		text(string(e.ExportBinding))
		text(e.ExportBindingElement)
		text(string(e.ImportBinding))
		text(e.ImportBindingElement)
	}
	return h.Sum(nil)
}

// Follow calls send with the envelope in force for the site at nmi, and again
// whenever it changes: a newer envelope was published, a backstop began or
// ended, or the interval rolled over. The envelope is nil while the site has
// none. When nothing changes for a keepalive period, it calls send again with
// keepalive true.
//
// It returns nil when ctx ends, which is how a client leaves, and the error of
// send when a send fails. The caller must hold the device token of the site.
func (s *Envelopes) Follow(ctx context.Context, nmi string, send func(envelope *domain.Envelope, now time.Time, keepalive bool) error) error {
	if err := auth.RequireDevice(ctx, nmi); err != nil {
		return err
	}
	site, err := s.store.GetSiteByNMI(ctx, nmi)
	if err != nil {
		return err
	}

	signal, cancel := s.bus.Subscribe(site.ID)
	defer cancel()

	var sent uuid.UUID // the id of the envelope last sent; Nil for "none"
	first := true
	for {
		now := s.clock.Now()
		envelope, err := s.current(ctx, site.ID, now)
		if err != nil {
			return err
		}
		id := uuid.Nil
		if envelope != nil {
			id = envelope.ID
		}
		changed := first || id != sent
		if err := send(envelope, now, !changed); err != nil {
			return err
		}
		sent, first = id, false

		// Wake at the end of the interval, at the keepalive, or when told,
		// whichever comes first.
		wait := s.Keepalive
		if envelope != nil {
			// Never zero: an interval that has just ended is re-read after a
			// moment, not in a busy loop.
			wait = min(wait, max(s.clock.Until(envelope.ValidTo), 10*time.Millisecond))
		}
		select {
		case <-ctx.Done():
			return nil
		case <-signal:
		case <-s.after(wait):
		}
	}
}
