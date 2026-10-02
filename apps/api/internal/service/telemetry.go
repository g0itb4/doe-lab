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

// SiteState is one site at an instant: the envelope in force, and what its
// devices last said.
type SiteState struct {
	SiteID uuid.UUID
	// Envelope is the envelope in force; nil when the site has none.
	Envelope *domain.Envelope
	// Reporting says that a device of the site was heard within the offline
	// period. NetExportW is what it said the site's net flow was, positive
	// for export, and is meaningful only for a reporting site.
	Reporting  bool
	NetExportW float64
	// OverLimit says that the net flow is above the export limit in force.
	OverLimit bool
	// OpenAlert is the open alert that matters most; nil when there is none.
	OpenAlert *domain.Alert
}

// Summary returns the fleet of a feeder now.
func (s *Telemetry) Summary(ctx context.Context, feederID uuid.UUID) (FleetSummary, error) {
	var out FleetSummary
	err := s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		var err error
		out, _, err = s.feederState(ctx, r, feederID, s.clock.Now())
		return err
	})
	return out, err
}

// feederState reads the fleet of a feeder at now: its summary, and the state
// of each of its sites that has a place on the map, in NMI order.
func (s *Telemetry) feederState(ctx context.Context, r Repos, feederID uuid.UUID, now time.Time) (FleetSummary, []SiteState, error) {
	out := FleetSummary{FeederID: feederID, At: now}
	if _, err := r.GetFeeder(ctx, feederID); err != nil {
		return out, nil, err
	}
	_, offlineAfter, err := thresholds(ctx, r, feederID)
	if err != nil {
		return out, nil, err
	}
	sites, err := r.ListAllSites(ctx, feederID)
	if err != nil {
		return out, nil, err
	}
	var enrolled []uuid.UUID
	// The located sites, and where each is in the result.
	var states []SiteState
	located := map[uuid.UUID]int{}
	for _, site := range sites {
		if site.ExportCapW > 0 || site.ImportCapW > 0 {
			enrolled = append(enrolled, site.ID)
		}
		if site.LatitudeDeg != nil {
			located[site.ID] = len(states)
			states = append(states, SiteState{SiteID: site.ID})
		}
	}
	out.EnrolledSites = len(enrolled)

	// A site's net flow is what its most recently heard device says: the
	// devices of a site share one meter.
	devices, err := r.ListDeviceStates(ctx, feederID)
	if err != nil {
		return out, nil, err
	}
	type live struct {
		seen time.Time
		netW float64
	}
	reporting := map[uuid.UUID]live{}
	out.Devices = len(devices)
	for _, state := range devices {
		if state.LastSeenAt == nil || now.Sub(*state.LastSeenAt) > offlineAfter {
			continue
		}
		out.DevicesOnline++
		if have, ok := reporting[state.SiteID]; !ok || state.LastSeenAt.After(have.seen) {
			reporting[state.SiteID] = live{seen: *state.LastSeenAt, netW: state.NetExportW}
		}
	}
	out.ReportingSites = len(reporting)
	for id, l := range reporting {
		out.ExportW += max(l.netW, 0)
		out.ImportW += max(-l.netW, 0)
		if i, ok := located[id]; ok {
			states[i].Reporting, states[i].NetExportW = true, l.netW
		}
	}

	envelopes, err := r.ListActiveEnvelopes(ctx, enrolled, now)
	if err != nil {
		return out, nil, err
	}
	for _, e := range envelopes {
		if e.ValidFrom.After(now) {
			continue // not in force yet
		}
		out.ExportLimitW += e.ExportLimitW
		l, heard := reporting[e.SiteID]
		over := heard && l.netW > e.ExportLimitW+ExportToleranceW
		if over {
			out.SitesOverLimit++
		}
		if i, ok := located[e.SiteID]; ok {
			states[i].Envelope, states[i].OverLimit = &e, over
		}
	}

	if out.OpenAlerts, err = r.CountOpenAlerts(ctx, feederID); err != nil {
		return out, nil, err
	}
	backstop, err := r.GetActiveBackstopEvent(ctx, feederID)
	switch {
	case err == nil:
		out.BackstopEventID = &backstop.ID
	case !errors.Is(err, domain.ErrNotFound):
		return out, nil, err
	}
	runs, _, err := r.ListEnvelopeRuns(ctx, feederID, nil, domain.Page{Size: 1})
	if err != nil {
		return out, nil, err
	}
	if len(runs) > 0 {
		out.LatestRun = &runs[0]
	}
	return out, states, nil
}

// FleetState is every feeder at one instant, with the sites that a map draws.
type FleetState struct {
	At time.Time
	// Feeders is the summary of each feeder, in code order.
	Feeders []FleetSummary
	// Sites is the state of every site that has a location: feeder by
	// feeder, and within a feeder in NMI order.
	Sites []SiteState
}

// maxOpenAlerts is how many open alerts of a feeder the fleet state reads. A
// feeder with more shows the newest.
const maxOpenAlerts = 500

// severityRank orders the alert severities.
var severityRank = map[domain.AlertSeverity]int{
	domain.SeverityInfo: 1, domain.SeverityWarning: 2, domain.SeverityCritical: 3,
}

// FleetState returns every feeder now: the summary of each, and the state of
// each site that has a place on the map.
func (s *Telemetry) FleetState(ctx context.Context) (FleetState, error) {
	out := FleetState{At: s.clock.Now()}
	err := s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		for token := ""; ; {
			feeders, next, err := r.ListFeeders(ctx, domain.Page{Size: 500, Token: token})
			if err != nil {
				return err
			}
			for _, feeder := range feeders {
				summary, sites, err := s.feederState(ctx, r, feeder.ID, out.At)
				if err != nil {
					return fmt.Errorf("feeder %s: %w", feeder.Code, err)
				}
				// The open alerts, newest first: a site shows the gravest of
				// its own, and of two as grave the newer.
				alerts, _, err := r.ListAlerts(ctx, feeder.ID, AlertFilter{OpenOnly: true}, domain.Page{Size: maxOpenAlerts})
				if err != nil {
					return fmt.Errorf("feeder %s: %w", feeder.Code, err)
				}
				gravest := map[uuid.UUID]*domain.Alert{}
				for _, alert := range alerts {
					if have := gravest[alert.SiteID]; have == nil || severityRank[alert.Severity] > severityRank[have.Severity] {
						gravest[alert.SiteID] = &alert
					}
				}
				for i := range sites {
					sites[i].OpenAlert = gravest[sites[i].SiteID]
				}
				out.Feeders = append(out.Feeders, summary)
				out.Sites = append(out.Sites, sites...)
			}
			if token = next; token == "" {
				return nil
			}
		}
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

// FeederState is one feeder at an instant, bus by bus and line by line, with
// the limits it is judged against.
type FeederState struct {
	At time.Time
	// Nodes and Lines are the states of the interval that holds At; empty
	// when the engine has solved no interval that holds it.
	Nodes []domain.FeederNodeState
	Lines []domain.FeederLineState
	// The limits of the active config; zero for a feeder that has none.
	VMinPU              float64
	VMaxPU              float64
	LineLimitPct        float64
	TransformerLimitPct float64
}

// FeederState returns what the engine solved for a feeder at an instant of
// feeder time: now, when at is nil.
func (s *Telemetry) FeederState(ctx context.Context, feederID uuid.UUID, at *time.Time) (FeederState, error) {
	out := FeederState{At: s.clock.Now()}
	if at != nil {
		out.At = *at
	}
	err := s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		if _, err := r.GetFeeder(ctx, feederID); err != nil {
			return err
		}
		var err error
		if out.Nodes, err = r.ListFeederNodeStates(ctx, feederID, out.At); err != nil {
			return err
		}
		if out.Lines, err = r.ListFeederLineStates(ctx, feederID, out.At); err != nil {
			return err
		}
		config, err := r.GetActiveEnvelopeConfig(ctx, feederID)
		switch {
		case err == nil:
			out.VMinPU, out.VMaxPU = config.VMinPU, config.VMaxPU
			out.LineLimitPct, out.TransformerLimitPct = config.LineLimitPct, config.TransformerLimitPct
		case !errors.Is(err, domain.ErrNotFound):
			return err
		}
		return nil
	})
	return out, err
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
