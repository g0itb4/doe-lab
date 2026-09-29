package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/auth"
	"doelab/api/internal/domain"
	"doelab/api/internal/profile"
)

// Telemetry takes readings from devices, and shows what the fleet and the
// feeder are doing.
type Telemetry struct {
	store      Store
	clock      Clock
	compliance *Compliance
	// Metrics is told of the readings that are stored.
	Metrics Recorder
	// WatchEvery is how often WatchFleet sends a summary, on the wall clock.
	WatchEvery time.Duration
	after      func(time.Duration) <-chan time.Time
}

// NewTelemetry builds the service.
func NewTelemetry(store Store, clock Clock, compliance *Compliance) *Telemetry {
	return &Telemetry{store: store, clock: clock, compliance: compliance, Metrics: NoRecorder{}, WatchEvery: time.Second, after: time.After}
}

// Ingest stores a batch of readings from the devices of the site at nmi, and
// judges the site's latest reading against its envelope. The caller must hold
// the device token of the site, and every reading must be from one of the
// site's devices. It returns how many readings were stored; the rest were
// already there.
func (s *Telemetry) Ingest(ctx context.Context, nmi string, readings []domain.Reading) (int, error) {
	if err := auth.RequireDevice(ctx, nmi); err != nil {
		return 0, err
	}
	if len(readings) == 0 {
		return 0, nil
	}
	stored := 0
	err := s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		site, err := r.GetSiteByNMI(ctx, nmi)
		if err != nil {
			return err
		}
		devices, _, err := r.ListDevices(ctx, DeviceFilter{SiteID: &site.ID}, domain.Page{Size: 500})
		if err != nil {
			return err
		}
		ofSite := make(map[uuid.UUID]bool, len(devices))
		for _, d := range devices {
			ofSite[d.ID] = true
		}
		latest := readings[0]
		for i := range readings {
			if !ofSite[readings[i].DeviceID] {
				return fmt.Errorf("%w: device %s is not a device of site %s", domain.ErrPermissionDenied, readings[i].DeviceID, nmi)
			}
			readings[i].SiteID = site.ID
			if readings[i].TS.After(latest.TS) {
				latest = readings[i]
			}
		}
		if stored, err = r.InsertReadings(ctx, readings); err != nil {
			return err
		}
		grace, _, err := thresholds(ctx, r, site.FeederID)
		if err != nil {
			return err
		}
		return s.compliance.Observe(ctx, r, site, latest, grace)
	})
	if err == nil {
		s.Metrics.ReadingsStored(ctx, stored)
	}
	return stored, err
}

// ListReadings returns a page of a device's readings with from <= ts < to.
func (s *Telemetry) ListReadings(ctx context.Context, deviceID uuid.UUID, from, to time.Time, p domain.Page) ([]domain.Reading, string, error) {
	if _, err := s.store.GetDevice(ctx, deviceID); err != nil {
		return nil, "", err
	}
	return s.store.ListReadings(ctx, deviceID, from, to, page(p))
}

// FleetSummary is the fleet of a feeder at one instant.
type FleetSummary struct {
	FeederID       uuid.UUID
	At             time.Time
	EnrolledSites  int
	ReportingSites int
	Devices        int
	DevicesOnline  int
	ExportW        float64
	ImportW        float64
	// ExportLimitW is the sum of the export limits in force.
	ExportLimitW   float64
	SitesOverLimit int
	OpenAlerts     int
	// BackstopEventID is the backstop in force, if any.
	BackstopEventID *uuid.UUID
	// LatestRun is the newest run of the engine, if there is one.
	LatestRun *domain.EnvelopeRun
}

// Summary returns the fleet of a feeder now.
func (s *Telemetry) Summary(ctx context.Context, feederID uuid.UUID) (FleetSummary, error) {
	now := s.clock.Now()
	out := FleetSummary{FeederID: feederID, At: now}
	err := s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		if _, err := r.GetFeeder(ctx, feederID); err != nil {
			return err
		}
		_, offlineAfter, err := thresholds(ctx, r, feederID)
		if err != nil {
			return err
		}
		sites, err := r.ListAllSites(ctx, feederID)
		if err != nil {
			return err
		}
		var enrolled []uuid.UUID
		for _, site := range sites {
			if site.ExportCapW > 0 || site.ImportCapW > 0 {
				enrolled = append(enrolled, site.ID)
			}
		}
		out.EnrolledSites = len(enrolled)

		// A site's net flow is what its most recently heard device says: the
		// devices of a site share one meter.
		states, err := r.ListDeviceStates(ctx, feederID)
		if err != nil {
			return err
		}
		type live struct {
			seen time.Time
			netW float64
		}
		reporting := map[uuid.UUID]live{}
		out.Devices = len(states)
		for _, state := range states {
			if state.LastSeenAt == nil || now.Sub(*state.LastSeenAt) > offlineAfter {
				continue
			}
			out.DevicesOnline++
			if have, ok := reporting[state.SiteID]; !ok || state.LastSeenAt.After(have.seen) {
				reporting[state.SiteID] = live{seen: *state.LastSeenAt, netW: state.NetExportW}
			}
		}
		out.ReportingSites = len(reporting)
		for _, l := range reporting {
			out.ExportW += max(l.netW, 0)
			out.ImportW += max(-l.netW, 0)
		}

		envelopes, err := r.ListActiveEnvelopes(ctx, enrolled, now)
		if err != nil {
			return err
		}
		for _, e := range envelopes {
			if e.ValidFrom.After(now) {
				continue // not in force yet
			}
			out.ExportLimitW += e.ExportLimitW
			if l, ok := reporting[e.SiteID]; ok && l.netW > e.ExportLimitW+ExportToleranceW {
				out.SitesOverLimit++
			}
		}

		if out.OpenAlerts, err = r.CountOpenAlerts(ctx, feederID); err != nil {
			return err
		}
		backstop, err := r.GetActiveBackstopEvent(ctx, feederID)
		switch {
		case err == nil:
			out.BackstopEventID = &backstop.ID
		case !errors.Is(err, domain.ErrNotFound):
			return err
		}
		runs, _, err := r.ListEnvelopeRuns(ctx, feederID, nil, domain.Page{Size: 1})
		if err != nil {
			return err
		}
		if len(runs) > 0 {
			out.LatestRun = &runs[0]
		}
		return nil
	})
	return out, err
}

// Watch calls send with the summary of a feeder now, and again every
// WatchEvery, until ctx ends or a send fails. It returns nil when ctx ends.
func (s *Telemetry) Watch(ctx context.Context, feederID uuid.UUID, send func(FleetSummary) error) error {
	for {
		summary, err := s.Summary(ctx, feederID)
		if err != nil {
			return err
		}
		if err := send(summary); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-s.after(s.WatchEvery):
		}
	}
}

// FeederPoint is one interval of a feeder: forecast, allowed and measured.
type FeederPoint struct {
	domain.EnvelopeRunInterval
	// MeasuredExportW and MeasuredImportW are what the reporting sites did,
	// averaged over the interval; nil for an interval with no telemetry.
	MeasuredExportW *float64
	MeasuredImportW *float64
}

// FeederSeries is a feeder through time, with the limits it is judged by.
type FeederSeries struct {
	Points         []FeederPoint
	VMinPU         float64
	VMaxPU         float64
	TransformerKVA float64
	StaticLimitW   float64
}

// MaxSeries is the longest range a series may cover.
const MaxSeries = 7 * 24 * time.Hour

func checkRange(from, to time.Time) error {
	if !to.After(from) || to.Sub(from) > MaxSeries {
		return fmt.Errorf("%w: a series covers more than nothing and at most 7 days", domain.ErrInvalid)
	}
	return nil
}

// Series returns a feeder through time: for each interval that starts in
// [from, to), what the engine forecast and allowed, and what the fleet did.
func (s *Telemetry) Series(ctx context.Context, feederID uuid.UUID, from, to time.Time) (FeederSeries, error) {
	var out FeederSeries
	if err := checkRange(from, to); err != nil {
		return out, err
	}
	feeder, err := s.store.GetFeeder(ctx, feederID)
	if err != nil {
		return out, err
	}
	out.TransformerKVA = feeder.TransformerKVA
	config, err := s.store.GetActiveEnvelopeConfig(ctx, feederID)
	switch {
	case err == nil:
		out.VMinPU, out.VMaxPU, out.StaticLimitW = config.VMinPU, config.VMaxPU, config.StaticLimitW
	case !errors.Is(err, domain.ErrNotFound):
		return out, err
	}

	intervals, err := s.store.ListFeederIntervals(ctx, feederID, from, to)
	if err != nil {
		return out, err
	}
	fleet, err := s.store.ListFleetSeries(ctx, feederID, from, to)
	if err != nil {
		return out, err
	}
	out.Points = make([]FeederPoint, len(intervals))
	next := 0 // fleet is in time order, and so are the intervals
	for i, interval := range intervals {
		point := FeederPoint{EnvelopeRunInterval: interval}
		var exportW, importW float64
		minutes := 0
		for ; next < len(fleet) && fleet[next].Bucket.Before(interval.ValidTo); next++ {
			if fleet[next].Bucket.Before(interval.ValidFrom) {
				continue
			}
			exportW += fleet[next].ExportW
			importW += fleet[next].ImportW
			minutes++
		}
		if minutes > 0 {
			exportW, importW = exportW/float64(minutes), importW/float64(minutes)
			point.MeasuredExportW, point.MeasuredImportW = &exportW, &importW
		}
		out.Points[i] = point
	}
	return out, nil
}

// SiteForecast is the forecast of a site for one half hour, with the config's
// PV scale applied.
type SiteForecast struct {
	TS    time.Time
	LoadW float64
	PVW   float64
}

// SiteSeries is one site through time.
type SiteSeries struct {
	Power    []domain.SitePower
	Forecast []SiteForecast
}

// SiteSeries returns a site's telemetry by the minute and its forecast by the
// half hour, over [from, to).
func (s *Telemetry) SiteSeries(ctx context.Context, siteID uuid.UUID, from, to time.Time) (SiteSeries, error) {
	var out SiteSeries
	if err := checkRange(from, to); err != nil {
		return out, err
	}
	site, err := s.store.GetSite(ctx, siteID)
	if err != nil {
		return out, err
	}
	if out.Power, err = s.store.ListSitePower(ctx, siteID, from, to); err != nil {
		return out, err
	}

	feeder, err := s.store.GetFeeder(ctx, site.FeederID)
	if err != nil {
		return out, err
	}
	zone, err := time.LoadLocation(feeder.Timezone)
	if err != nil {
		return out, fmt.Errorf("feeder %s: time zone %q: %w", feeder.Code, feeder.Timezone, err)
	}
	pvScale := 1.0
	config, err := s.store.GetActiveEnvelopeConfig(ctx, site.FeederID)
	switch {
	case err == nil:
		pvScale = config.PVScale
	case !errors.Is(err, domain.ErrNotFound):
		return out, err
	}

	// The same mapping as the feeder's forecast: each half hour reads the
	// half hour of the profile year with the same local date and time.
	year := profile.Year{Start: profile.DefaultYearStart, Location: zone}
	rows, _, err := s.store.ListSiteProfiles(ctx, siteID, year.From(), year.To(), domain.Page{Size: int32(year.HalfHours())}) //nolint:gosec // G115: 17,568 at most
	if err != nil {
		return out, err
	}
	byTS := make(map[int64]domain.SiteProfile, len(rows))
	for _, row := range rows {
		byTS[row.TS.Unix()] = row
	}
	for at := from.Truncate(halfHour); at.Before(to); at = at.Add(halfHour) {
		// A site with no profile has no forecast; that is not an error here,
		// the chart simply has no forecast line.
		if row, ok := byTS[year.At(at).Unix()]; ok {
			out.Forecast = append(out.Forecast, SiteForecast{
				TS: at.UTC(), LoadW: row.LoadW + row.ControlledLoadW, PVW: row.PVW * pvScale,
			})
		}
	}
	return out, nil
}
