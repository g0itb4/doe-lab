package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
)

// DailyReport is a day of a feeder in numbers.
type DailyReport struct {
	// From and To bound the local calendar day, as feeder time.
	From, To      time.Time
	EnrolledSites int
	// Intervals is the number of intervals of the day that have envelopes.
	Intervals int
	// PotentialExportKWh is what the enrolled sites' PV could have exported
	// with no limit: generation above their own load, by the forecast.
	PotentialExportKWh float64
	// What the envelopes let out and held back, and what a fixed limit would
	// have.
	EnvelopeExportKWh    float64
	EnvelopeCurtailedKWh float64
	StaticExportKWh      float64
	StaticCurtailedKWh   float64
	// StaticViolationIntervals is the number of intervals in which the fixed
	// limit would have broken a network limit.
	StaticViolationIntervals int
	StaticLimitW             float64
	ConstraintBreaches       int
	DeviceOfflineAlerts      int
	BackstopEvents           int
}

// Report returns the day that holds the instant `day`, as the local calendar
// day of the feeder.
//
// The energy figures compare two ways of limiting the same forecast. For each
// enrolled site and each interval that has an envelope, the site's potential
// export is its forecast PV above its forecast load. Under the envelope it
// exports the lesser of that and its export limit; under a fixed limit, the
// lesser of that and the fixed limit. What is left over is curtailed.
func (s *Telemetry) Report(ctx context.Context, feederID uuid.UUID, day time.Time) (DailyReport, error) {
	var out DailyReport
	feeder, err := s.store.GetFeeder(ctx, feederID)
	if err != nil {
		return out, err
	}
	zone, err := time.LoadLocation(feeder.Timezone)
	if err != nil {
		return out, fmt.Errorf("feeder %s: time zone %q: %w", feeder.Code, feeder.Timezone, err)
	}
	local := day.In(zone)
	out.From = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, zone).UTC()
	out.To = time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, zone).UTC()

	config, err := s.store.GetActiveEnvelopeConfig(ctx, feederID)
	if errors.Is(err, domain.ErrNotFound) {
		return out, fmt.Errorf("%w: feeder %s has no envelope config", domain.ErrFailedPrecondition, feeder.Code)
	}
	if err != nil {
		return out, err
	}
	out.StaticLimitW = config.StaticLimitW

	sites, err := s.store.ListAllSites(ctx, feederID)
	if err != nil {
		return out, err
	}
	exportCap := map[uuid.UUID]float64{}
	for _, site := range sites {
		if site.ExportCapW > 0 {
			exportCap[site.ID] = site.ExportCapW
		}
	}
	out.EnrolledSites = len(exportCap)

	envelopes, err := s.store.ListFeederEnvelopes(ctx, feederID, out.From, out.To)
	if err != nil {
		return out, err
	}
	// The intervals of the day that have envelopes: what the report covers.
	intervals := map[int64]bool{}
	if len(envelopes) > 0 {
		points, err := forecast(ctx, s.store, feederID, out.From, out.To)
		if err != nil {
			return out, err
		}
		type key struct {
			site uuid.UUID
			ts   int64
		}
		potential := make(map[key]float64, len(points))
		for _, p := range points {
			potential[key{p.SiteID, p.TS.Unix()}] = max(0, p.PVW*config.PVScale-p.LoadW)
		}
		for _, e := range envelopes {
			siteCap, enrolled := exportCap[e.SiteID]
			if !enrolled {
				continue
			}
			intervals[e.ValidFrom.Unix()] = true
			hours := e.ValidTo.Sub(e.ValidFrom).Hours()
			// The forecast is half-hourly; an interval reads the half hour it
			// starts in.
			p := potential[key{e.SiteID, e.ValidFrom.Truncate(halfHour).Unix()}]
			out.PotentialExportKWh += p * hours / 1000
			out.EnvelopeExportKWh += min(p, e.ExportLimitW) * hours / 1000
			out.StaticExportKWh += min(p, config.StaticLimitW, siteCap) * hours / 1000
		}
		out.Intervals = len(intervals)
		out.EnvelopeCurtailedKWh = out.PotentialExportKWh - out.EnvelopeExportKWh
		out.StaticCurtailedKWh = out.PotentialExportKWh - out.StaticExportKWh
	}

	forecasts, err := s.store.ListFeederIntervals(ctx, feederID, out.From, out.To)
	if err != nil {
		return out, err
	}
	for _, f := range forecasts {
		// Counted over the same intervals as the energy, so that "n of m"
		// means something.
		if intervals[f.ValidFrom.Unix()] && f.StaticBinding != domain.BindingNone {
			out.StaticViolationIntervals++
		}
	}

	// Alerts and backstops that began in the day. Both lists are newest
	// first, so reading stops at the first row before the day.
	for token := ""; ; {
		alerts, next, err := s.store.ListAlerts(ctx, feederID, AlertFilter{}, domain.Page{Size: 500, Token: token})
		if err != nil {
			return out, err
		}
		done := false
		for _, a := range alerts {
			switch {
			case a.OpenedAt.Before(out.From):
				done = true
			case !a.OpenedAt.Before(out.To):
			case a.Kind == domain.AlertConstraintBreach:
				out.ConstraintBreaches++
			default:
				out.DeviceOfflineAlerts++
			}
		}
		if token = next; token == "" || done {
			break
		}
	}
	for token := ""; ; {
		events, next, err := s.store.ListBackstopEvents(ctx, feederID, domain.Page{Size: 500, Token: token})
		if err != nil {
			return out, err
		}
		done := false
		for _, e := range events {
			switch {
			case e.TriggeredAt.Before(out.From):
				done = true
			case e.TriggeredAt.Before(out.To):
				out.BackstopEvents++
			}
		}
		if token = next; token == "" || done {
			break
		}
	}
	return out, nil
}
