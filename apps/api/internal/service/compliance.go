package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
)

// Compliance judges what the fleet does against what it was told: it opens an
// alert when a site exports above its limit for longer than the grace period,
// or a device stops reporting, and resolves the alert when the condition
// clears.
type Compliance struct {
	store Store
	clock Clock

	mu sync.Mutex
	// over holds, for each site that is above its export limit now, when
	// that began and the worst it has been. It is the one piece of state
	// that is not in the store: after a restart a breach in progress is
	// timed from the first reading the new process sees.
	over map[uuid.UUID]excess
}

type excess struct {
	since time.Time
	peakW float64
}

// NewCompliance builds the service.
func NewCompliance(store Store, clock Clock) *Compliance {
	return &Compliance{store: store, clock: clock, over: map[uuid.UUID]excess{}}
}

// ExportToleranceW is how far above its limit a site may read before it
// counts as over: a meter and an inverter's control loop are not exact.
const ExportToleranceW = 50.0

// Defaults for a feeder that has no config yet.
const (
	defaultBreachGrace  = 60 * time.Second
	defaultOfflineAfter = 300 * time.Second
)

// thresholds reads the grace and offline periods of a feeder's active config.
func thresholds(ctx context.Context, r Repos, feederID uuid.UUID) (grace, offline time.Duration, err error) {
	config, err := r.GetActiveEnvelopeConfig(ctx, feederID)
	if errors.Is(err, domain.ErrNotFound) {
		return defaultBreachGrace, defaultOfflineAfter, nil
	}
	if err != nil {
		return 0, 0, err
	}
	return time.Duration(config.BreachGraceSeconds) * time.Second, time.Duration(config.OfflineAfterSeconds) * time.Second, nil
}

// Observe judges the latest reading of a site. It runs inside the transaction
// that stores the reading, with that transaction's repositories.
func (c *Compliance) Observe(ctx context.Context, r Repos, site domain.Site, reading domain.Reading, grace time.Duration) error {
	// The site's device has spoken: it is not offline.
	if _, err := r.ResolveAlert(ctx, site.ID, domain.AlertDeviceOffline, reading.TS); err != nil {
		return err
	}

	envelope, err := r.GetCurrentEnvelope(ctx, site.ID, reading.TS)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	// With no envelope in force there is no limit to break.
	within := err != nil || reading.NetExportW <= envelope.ExportLimitW+ExportToleranceW

	c.mu.Lock()
	state, wasOver := c.over[site.ID]
	if within {
		delete(c.over, site.ID)
	} else {
		if !wasOver {
			state = excess{since: reading.TS, peakW: reading.NetExportW}
		}
		state.peakW = max(state.peakW, reading.NetExportW)
		c.over[site.ID] = state
	}
	c.mu.Unlock()

	if within {
		_, err := r.ResolveAlert(ctx, site.ID, domain.AlertConstraintBreach, reading.TS)
		return err
	}
	if reading.TS.Sub(state.since) < grace {
		return nil
	}
	severity := domain.SeverityWarning
	if envelope.Source == domain.SourceBackstop {
		// Exporting through an emergency backstop is the serious case.
		severity = domain.SeverityCritical
	}
	_, _, err = r.OpenAlert(ctx, domain.Alert{
		SiteID: site.ID, FeederID: site.FeederID, Kind: domain.AlertConstraintBreach, Severity: severity,
		OpenedAt: state.since, LimitW: &envelope.ExportLimitW, PeakW: &state.peakW,
		Detail: fmt.Sprintf("Net export above the %.0f W limit for more than %s.", envelope.ExportLimitW, grace),
	})
	return err
}

// Sweep looks for devices that have gone silent, on every feeder: a device
// that has reported before and has not for longer than the offline period
// gets a device_offline alert on its site. It returns how many alerts it
// opened. The API runs it on a timer.
func (c *Compliance) Sweep(ctx context.Context) (int, error) {
	opened := 0
	now := c.clock.Now()
	for token := ""; ; {
		feeders, next, err := c.store.ListFeeders(ctx, domain.Page{Size: DefaultPageSize, Token: token})
		if err != nil {
			return opened, err
		}
		for _, feeder := range feeders {
			n, err := c.sweepFeeder(ctx, feeder.ID, now)
			opened += n
			if err != nil {
				return opened, fmt.Errorf("feeder %s: %w", feeder.Code, err)
			}
		}
		if token = next; token == "" {
			return opened, nil
		}
	}
}

func (c *Compliance) sweepFeeder(ctx context.Context, feederID uuid.UUID, now time.Time) (int, error) {
	opened := 0
	err := c.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		_, offlineAfter, err := thresholds(ctx, r, feederID)
		if err != nil {
			return err
		}
		states, err := r.ListDeviceStates(ctx, feederID)
		if err != nil {
			return err
		}
		for _, state := range states {
			// A device that has never reported is not "gone": it has not
			// arrived.
			if state.LastSeenAt == nil || now.Sub(*state.LastSeenAt) <= offlineAfter {
				continue
			}
			_, isNew, err := r.OpenAlert(ctx, domain.Alert{
				SiteID: state.SiteID, FeederID: feederID, DeviceID: &state.DeviceID,
				Kind: domain.AlertDeviceOffline, Severity: domain.SeverityInfo,
				OpenedAt: state.LastSeenAt.Add(offlineAfter),
				Detail:   fmt.Sprintf("No reading from the %s device for more than %s.", state.DERType, offlineAfter),
			})
			if err != nil {
				return err
			}
			if isNew {
				opened++
			}
		}
		return nil
	})
	return opened, err
}
