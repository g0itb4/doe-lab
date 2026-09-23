package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
	"doelab/api/internal/profile"
)

// Feeders reads the network model and applies the two changes an operator may
// make to it.
type Feeders struct {
	store Store
}

// NewFeeders builds the service.
func NewFeeders(store Store) *Feeders {
	return &Feeders{store: store}
}

// Get returns a feeder by id.
func (s *Feeders) Get(ctx context.Context, id uuid.UUID) (domain.Feeder, error) {
	return s.store.GetFeeder(ctx, id)
}

// GetByCode returns a feeder by its code.
func (s *Feeders) GetByCode(ctx context.Context, code string) (domain.Feeder, error) {
	return s.store.GetFeederByCode(ctx, code)
}

// List returns a page of feeders in code order.
func (s *Feeders) List(ctx context.Context, p domain.Page) ([]domain.Feeder, string, error) {
	return s.store.ListFeeders(ctx, page(p))
}

// FeederPatch names the fields of a feeder to change. A nil field is left as
// it is.
type FeederPatch struct {
	Name  *string
	TapPU *float64
}

// Update applies a patch to a feeder.
func (s *Feeders) Update(ctx context.Context, id uuid.UUID, patch FeederPatch) (domain.Feeder, error) {
	var out domain.Feeder
	err := s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		f, err := r.GetFeeder(ctx, id)
		if err != nil {
			return err
		}
		if patch.Name != nil {
			f.Name = *patch.Name
		}
		if patch.TapPU != nil {
			f.TapPU = *patch.TapPU
		}
		out, err = r.UpdateFeeder(ctx, f)
		return err
	})
	return out, err
}

// GetNode returns a node by id.
func (s *Feeders) GetNode(ctx context.Context, id uuid.UUID) (domain.FeederNode, error) {
	return s.store.GetFeederNode(ctx, id)
}

// ListNodes returns a page of a feeder's nodes in name order.
func (s *Feeders) ListNodes(ctx context.Context, feederID uuid.UUID, p domain.Page) ([]domain.FeederNode, string, error) {
	if _, err := s.store.GetFeeder(ctx, feederID); err != nil {
		return nil, "", fmt.Errorf("feeder %s: %w", feederID, err)
	}
	return s.store.ListFeederNodes(ctx, feederID, page(p))
}

// GetLine returns a line by id.
func (s *Feeders) GetLine(ctx context.Context, id uuid.UUID) (domain.FeederLine, error) {
	return s.store.GetFeederLine(ctx, id)
}

// ListLines returns a page of a feeder's lines in name order.
func (s *Feeders) ListLines(ctx context.Context, feederID uuid.UUID, p domain.Page) ([]domain.FeederLine, string, error) {
	if _, err := s.store.GetFeeder(ctx, feederID); err != nil {
		return nil, "", fmt.Errorf("feeder %s: %w", feederID, err)
	}
	return s.store.ListFeederLines(ctx, feederID, page(p))
}

// SetLineAmpacity sets a line's current rating, which becomes the operator's
// rating. A nil rating makes the line unrated. A switch has no rating to set.
func (s *Feeders) SetLineAmpacity(ctx context.Context, id uuid.UUID, ampacityA *float64) (domain.FeederLine, error) {
	var out domain.FeederLine
	err := s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		line, err := r.GetFeederLine(ctx, id)
		if err != nil {
			return err
		}
		if line.IsSwitch && ampacityA != nil {
			return fmt.Errorf("%w: %s is a switch and takes no rating", domain.ErrFailedPrecondition, line.Name)
		}
		out, err = r.UpdateFeederLineAmpacity(ctx, id, ampacityA)
		return err
	})
	return out, err
}

// ForecastPoint is the forecast of one site for one half hour.
type ForecastPoint struct {
	SiteID uuid.UUID
	// TS is the start of the half hour, in feeder time.
	TS time.Time
	// LoadW is consumption: general load plus controlled load.
	LoadW float64
	// PVW is gross PV generation, before any scaling.
	PVW float64
}

// halfHour is the grain of the profiles.
const halfHour = 30 * time.Minute

// MaxForecast is the longest range a forecast may cover.
const MaxForecast = 7 * 24 * time.Hour

// Forecast returns the load and PV of every site of a feeder for each half
// hour that starts in [from, to): time order, and within a half hour NMI
// order.
//
// A forecast for a date is read from the profile year, at the same local
// month, day and time. from is rounded down to a half hour.
func (s *Feeders) Forecast(ctx context.Context, feederID uuid.UUID, from, to time.Time) ([]ForecastPoint, error) {
	feeder, err := s.store.GetFeeder(ctx, feederID)
	if err != nil {
		return nil, err
	}
	if !to.After(from) || to.Sub(from) > MaxForecast {
		return nil, fmt.Errorf("%w: a forecast covers more than nothing and at most 7 days", domain.ErrInvalid)
	}
	zone, err := time.LoadLocation(feeder.Timezone)
	if err != nil {
		return nil, fmt.Errorf("feeder %s: time zone %q: %w", feeder.Code, feeder.Timezone, err)
	}
	year := profile.Year{Start: profile.DefaultYearStart, Location: zone}
	sites, err := s.store.ListAllSites(ctx, feederID)
	if err != nil {
		return nil, err
	}

	// Each half hour of the range, and the half hour of the profile year it
	// reads from.
	type slot struct{ at, source time.Time }
	var slots []slot
	earliest, latest := year.To(), year.From()
	for at := from.Truncate(halfHour); at.Before(to); at = at.Add(halfHour) {
		source := year.At(at).UTC()
		slots = append(slots, slot{at: at.UTC(), source: source})
		if source.Before(earliest) {
			earliest = source
		}
		if source.After(latest) {
			latest = source
		}
	}

	// One read covers every source half hour. A range that crosses 1 July
	// reads the whole profile year, which is the simple answer to a wrap.
	rows, err := s.store.ListFeederProfiles(ctx, feederID, earliest, latest.Add(halfHour))
	if err != nil {
		return nil, err
	}
	type key struct {
		site uuid.UUID
		ts   int64
	}
	bySlot := make(map[key]domain.SiteProfile, len(rows))
	for _, row := range rows {
		bySlot[key{row.SiteID, row.TS.Unix()}] = row
	}

	points := make([]ForecastPoint, 0, len(slots)*len(sites))
	for _, sl := range slots {
		for _, site := range sites {
			row, ok := bySlot[key{site.ID, sl.source.Unix()}]
			if !ok {
				return nil, fmt.Errorf("%w: site %s has no profile for %s; run the import",
					domain.ErrFailedPrecondition, site.NMI, sl.source.Format(time.RFC3339))
			}
			points = append(points, ForecastPoint{
				SiteID: site.ID, TS: sl.at, LoadW: row.LoadW + row.ControlledLoadW, PVW: row.PVW,
			})
		}
	}
	return points, nil
}
