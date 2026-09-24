package mem

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
	"doelab/api/internal/repo/pagetoken"
	"doelab/api/internal/service"
)

func (r *repos) CreateEnvelopeRunIntervals(_ context.Context, rows []domain.EnvelopeRunInterval) error {
	defer r.lock()()
	type key struct {
		run  uuid.UUID
		from int64
	}
	have := map[key]bool{}
	for _, row := range r.s.st.intervals {
		have[key{row.EnvelopeRunID, row.ValidFrom.UnixNano()}] = true
	}
	now := r.now()
	added := make([]domain.EnvelopeRunInterval, 0, len(rows))
	for _, row := range rows {
		if _, ok := r.s.st.runs[row.EnvelopeRunID]; !ok {
			return fmt.Errorf("envelope_run_intervals_run_fkey: %w", domain.ErrFailedPrecondition)
		}
		k := key{row.EnvelopeRunID, row.ValidFrom.UnixNano()}
		if have[k] {
			return fmt.Errorf("envelope_run_intervals_pkey: %w", domain.ErrAlreadyExists)
		}
		if !row.ValidTo.After(row.ValidFrom) || row.ForecastVMinPU <= 0 || row.ForecastVMinPU > row.ForecastVMaxPU {
			return fmt.Errorf("envelope_run_intervals_voltage_ordered: %w", domain.ErrInvalid)
		}
		have[k] = true
		row.ValidFrom, row.ValidTo, row.CreatedAt = row.ValidFrom.UTC(), row.ValidTo.UTC(), now
		added = append(added, row)
	}
	r.s.st.intervals = append(r.s.st.intervals, added...)
	return nil
}

func (r *repos) ListEnvelopeRunIntervals(_ context.Context, runID uuid.UUID) ([]domain.EnvelopeRunInterval, error) {
	defer r.lock()()
	var out []domain.EnvelopeRunInterval
	for _, row := range r.s.st.intervals {
		if row.EnvelopeRunID == runID {
			out = append(out, row)
		}
	}
	slices.SortFunc(out, func(a, b domain.EnvelopeRunInterval) int { return a.ValidFrom.Compare(b.ValidFrom) })
	return out, nil
}

func (r *repos) ListFeederIntervals(_ context.Context, feederID uuid.UUID, from, to time.Time) ([]domain.EnvelopeRunInterval, error) {
	defer r.lock()()
	// The latest row of each interval: rows are appended in the order they
	// were created, so a later row replaces an earlier one.
	latest := map[int64]domain.EnvelopeRunInterval{}
	for _, row := range r.s.st.intervals {
		if row.FeederID == feederID && !row.ValidFrom.Before(from) && row.ValidFrom.Before(to) {
			latest[row.ValidFrom.UnixNano()] = row
		}
	}
	out := make([]domain.EnvelopeRunInterval, 0, len(latest))
	for _, row := range latest {
		out = append(out, row)
	}
	slices.SortFunc(out, func(a, b domain.EnvelopeRunInterval) int { return a.ValidFrom.Compare(b.ValidFrom) })
	return out, nil
}

func (r *repos) InsertReadings(_ context.Context, readings []domain.Reading) (int, error) {
	defer r.lock()()
	for _, reading := range readings {
		if reading.SOCPct != nil && (*reading.SOCPct < 0 || *reading.SOCPct > 100) {
			return 0, fmt.Errorf("readings_soc_range: %w", domain.ErrInvalid)
		}
		if reading.VoltageV != nil && *reading.VoltageV <= 0 {
			return 0, fmt.Errorf("readings_voltage_positive: %w", domain.ErrInvalid)
		}
	}
	now := r.now()
	stored := 0
	for _, reading := range readings {
		device, ok := r.s.st.devices[reading.DeviceID]
		if !ok || device.DeletedAt != nil {
			continue
		}
		ts := reading.TS.UTC()
		if r.s.st.readings[device.ID] == nil {
			r.s.st.readings[device.ID] = map[int64]domain.Reading{}
		}
		if _, dup := r.s.st.readings[device.ID][ts.UnixNano()]; !dup {
			reading.SiteID, reading.TS, reading.ReceivedAt = device.SiteID, ts, now
			r.s.st.readings[device.ID][ts.UnixNano()] = reading
			stored++
		}
		// An older reading that arrives late does not move the status back.
		if status, seen := r.s.st.status[device.ID]; !seen || ts.After(status.LastSeenAt) {
			r.s.st.status[device.ID] = domain.DeviceStatus{
				DeviceID: device.ID, LastSeenAt: ts, PowerW: reading.PowerW, NetExportW: reading.NetExportW,
			}
		}
	}
	return stored, nil
}

func (r *repos) ListReadings(_ context.Context, deviceID uuid.UUID, from, to time.Time, page domain.Page) ([]domain.Reading, string, error) {
	defer r.lock()()
	after, err := pagetoken.Decode(page.Token, 1)
	if err != nil {
		return nil, "", err
	}
	var afterTime time.Time
	if after[0] != "" {
		if afterTime, err = parseTimeKey(after[0]); err != nil {
			return nil, "", err
		}
	}
	var rows []domain.Reading
	for _, reading := range r.s.st.readings[deviceID] {
		if !reading.TS.Before(from) && reading.TS.Before(to) && reading.TS.After(afterTime) {
			rows = append(rows, reading)
		}
	}
	slices.SortFunc(rows, func(a, b domain.Reading) int { return a.TS.Compare(b.TS) })
	rows, next := pagetoken.Next(limit(rows, page.Size+1), page.Size,
		func(reading domain.Reading) []string { return []string{timeKey(reading.TS)} })
	return rows, next, nil
}

// sitePower rolls a site's readings up by the minute, as site_power_1m does.
func (r *repos) sitePower(siteID uuid.UUID, from, to time.Time) []domain.SitePower {
	type sums struct {
		net, maxNet, soc, volts float64
		n, socN, voltsN         int
	}
	buckets := map[int64]*sums{}
	for _, device := range r.s.st.devices {
		if device.SiteID != siteID {
			continue
		}
		for _, reading := range r.s.st.readings[device.ID] {
			bucket := reading.TS.Truncate(time.Minute)
			if bucket.Before(from) || !bucket.Before(to) {
				continue
			}
			b := buckets[bucket.Unix()]
			if b == nil {
				b = &sums{maxNet: reading.NetExportW}
				buckets[bucket.Unix()] = b
			}
			b.net += reading.NetExportW
			b.maxNet = max(b.maxNet, reading.NetExportW)
			b.n++
			if reading.SOCPct != nil {
				b.soc += *reading.SOCPct
				b.socN++
			}
			if reading.VoltageV != nil {
				b.volts += *reading.VoltageV
				b.voltsN++
			}
		}
	}
	out := make([]domain.SitePower, 0, len(buckets))
	for unix, b := range buckets {
		row := domain.SitePower{
			SiteID: siteID, Bucket: time.Unix(unix, 0).UTC(),
			AvgNetExportW: b.net / float64(b.n), MaxNetExportW: b.maxNet, ReadingCount: int64(b.n),
		}
		if b.socN > 0 {
			soc := b.soc / float64(b.socN)
			row.AvgSOCPct = &soc
		}
		if b.voltsN > 0 {
			volts := b.volts / float64(b.voltsN)
			row.AvgVoltageV = &volts
		}
		out = append(out, row)
	}
	slices.SortFunc(out, func(a, b domain.SitePower) int { return a.Bucket.Compare(b.Bucket) })
	return out
}

func (r *repos) ListSitePower(_ context.Context, siteID uuid.UUID, from, to time.Time) ([]domain.SitePower, error) {
	defer r.lock()()
	return r.sitePower(siteID, from, to), nil
}

func (r *repos) ListFleetSeries(_ context.Context, feederID uuid.UUID, from, to time.Time) ([]domain.FleetMinute, error) {
	defer r.lock()()
	type sums struct {
		row  domain.FleetMinute
		soc  float64
		socN int
	}
	buckets := map[int64]*sums{}
	for _, site := range r.feederSites(feederID, nil, "") {
		for _, p := range r.sitePower(site.ID, from, to) {
			b := buckets[p.Bucket.Unix()]
			if b == nil {
				b = &sums{row: domain.FleetMinute{FeederID: feederID, Bucket: p.Bucket}}
				buckets[p.Bucket.Unix()] = b
			}
			b.row.ExportW += max(p.AvgNetExportW, 0)
			b.row.ImportW += max(-p.AvgNetExportW, 0)
			b.row.ReportingSites++
			b.row.ReadingCount += p.ReadingCount
			if p.AvgSOCPct != nil {
				b.soc += *p.AvgSOCPct
				b.socN++
			}
		}
	}
	out := make([]domain.FleetMinute, 0, len(buckets))
	for _, b := range buckets {
		if b.socN > 0 {
			soc := b.soc / float64(b.socN)
			b.row.AvgSOCPct = &soc
		}
		out = append(out, b.row)
	}
	slices.SortFunc(out, func(a, b domain.FleetMinute) int { return a.Bucket.Compare(b.Bucket) })
	return out, nil
}

func (r *repos) ListDeviceStates(_ context.Context, feederID uuid.UUID) ([]domain.DeviceState, error) {
	defer r.lock()()
	var out []domain.DeviceState
	for _, site := range r.feederSites(feederID, nil, "") {
		for _, device := range r.s.st.devices {
			if device.SiteID != site.ID || device.DeletedAt != nil {
				continue
			}
			state := domain.DeviceState{DeviceID: device.ID, SiteID: site.ID, DERType: device.DERType, NMI: site.NMI}
			if status, seen := r.s.st.status[device.ID]; seen {
				at := status.LastSeenAt
				state.LastSeenAt, state.PowerW, state.NetExportW = &at, status.PowerW, status.NetExportW
			}
			out = append(out, state)
		}
	}
	slices.SortFunc(out, func(a, b domain.DeviceState) int {
		return cmp.Or(cmp.Compare(a.NMI, b.NMI), cmp.Compare(a.DERType, b.DERType))
	})
	return out, nil
}

func (r *repos) GetAlert(_ context.Context, id uuid.UUID) (domain.Alert, error) {
	defer r.lock()()
	a, ok := r.s.st.alerts[id]
	if !ok {
		return domain.Alert{}, notFound("alert", id)
	}
	return a, nil
}

func (r *repos) ListAlerts(_ context.Context, feederID uuid.UUID, filter service.AlertFilter, page domain.Page) ([]domain.Alert, string, error) {
	defer r.lock()()
	after, err := pagetoken.Decode(page.Token, 2)
	if err != nil {
		return nil, "", err
	}
	var beforeTime time.Time
	if after[0] != "" {
		if beforeTime, err = parseTimeKey(after[0]); err != nil {
			return nil, "", err
		}
	}
	rows := sorted(r.s.st.alerts,
		func(a domain.Alert) bool {
			if a.FeederID != feederID {
				return false
			}
			if (filter.SiteID != nil && a.SiteID != *filter.SiteID) || (filter.Kind != nil && a.Kind != *filter.Kind) ||
				(filter.OpenOnly && a.ResolvedAt != nil) {
				return false
			}
			if after[0] == "" {
				return true
			}
			if c := a.OpenedAt.Compare(beforeTime); c != 0 {
				return c < 0
			}
			return a.ID.String() < after[1]
		},
		func(a, b domain.Alert) int {
			return cmp.Or(b.OpenedAt.Compare(a.OpenedAt), cmp.Compare(b.ID.String(), a.ID.String()))
		})
	rows, next := pagetoken.Next(limit(rows, page.Size+1), page.Size,
		func(a domain.Alert) []string { return []string{timeKey(a.OpenedAt), a.ID.String()} })
	return rows, next, nil
}

// severityRank orders the severities as the alert_severity enum does.
var severityRank = map[domain.AlertSeverity]int{domain.SeverityInfo: 0, domain.SeverityWarning: 1, domain.SeverityCritical: 2}

func (r *repos) OpenAlert(_ context.Context, alert domain.Alert) (domain.Alert, bool, error) {
	defer r.lock()()
	// The site and its feeder are one foreign key.
	if site, ok := r.s.st.sites[alert.SiteID]; !ok || site.FeederID != alert.FeederID {
		return domain.Alert{}, false, fmt.Errorf("alerts_site_fkey: %w", domain.ErrFailedPrecondition)
	}
	if alert.DeviceID != nil {
		if device, ok := r.s.st.devices[*alert.DeviceID]; !ok || device.SiteID != alert.SiteID {
			return domain.Alert{}, false, fmt.Errorf("alerts_device_fkey: %w", domain.ErrFailedPrecondition)
		}
	}
	if alert.Kind == domain.AlertConstraintBreach && (alert.LimitW == nil || alert.PeakW == nil || *alert.PeakW < *alert.LimitW) {
		return domain.Alert{}, false, fmt.Errorf("alerts_breach_measured: %w", domain.ErrInvalid)
	}
	if alert.Kind == domain.AlertDeviceOffline && alert.DeviceID == nil {
		return domain.Alert{}, false, fmt.Errorf("alerts_offline_names_device: %w", domain.ErrInvalid)
	}
	now := r.now()
	for id, open := range r.s.st.alerts {
		if open.SiteID == alert.SiteID && open.Kind == alert.Kind && open.ResolvedAt == nil {
			if alert.PeakW != nil && (open.PeakW == nil || *alert.PeakW > *open.PeakW) {
				open.PeakW, open.UpdatedAt = alert.PeakW, now
			}
			if severityRank[alert.Severity] > severityRank[open.Severity] {
				open.Severity, open.UpdatedAt = alert.Severity, now
			}
			r.s.st.alerts[id] = open
			return open, false, nil
		}
	}
	alert.ID = uuid.Must(uuid.NewV7())
	alert.OpenedAt = alert.OpenedAt.UTC()
	alert.CreatedAt, alert.UpdatedAt = now, now
	alert.ResolvedAt, alert.AcknowledgedAt, alert.AcknowledgedBy = nil, nil, nil
	r.s.st.alerts[alert.ID] = alert
	return alert, true, nil
}

func (r *repos) ResolveAlert(_ context.Context, siteID uuid.UUID, kind domain.AlertKind, at time.Time) (bool, error) {
	defer r.lock()()
	for id, open := range r.s.st.alerts {
		if open.SiteID == siteID && open.Kind == kind && open.ResolvedAt == nil {
			// An alert cannot end before it began.
			resolved := at.UTC()
			if resolved.Before(open.OpenedAt) {
				resolved = open.OpenedAt
			}
			open.ResolvedAt, open.UpdatedAt = &resolved, r.now()
			r.s.st.alerts[id] = open
			return true, nil
		}
	}
	return false, nil
}

func (r *repos) AcknowledgeAlert(_ context.Context, id uuid.UUID, by string) (domain.Alert, error) {
	defer r.lock()()
	a, ok := r.s.st.alerts[id]
	if !ok {
		return domain.Alert{}, notFound("alert", id)
	}
	if a.AcknowledgedAt == nil {
		now := r.now()
		a.AcknowledgedAt, a.AcknowledgedBy, a.UpdatedAt = &now, &by, now
		r.s.st.alerts[id] = a
	}
	return a, nil
}

func (r *repos) CountOpenAlerts(_ context.Context, feederID uuid.UUID) (int, error) {
	defer r.lock()()
	n := 0
	for _, a := range r.s.st.alerts {
		if a.FeederID == feederID && a.ResolvedAt == nil {
			n++
		}
	}
	return n, nil
}

func (r *repos) GetBackstopEvent(_ context.Context, id uuid.UUID) (domain.BackstopEvent, error) {
	defer r.lock()()
	e, ok := r.s.st.backstops[id]
	if !ok {
		return domain.BackstopEvent{}, notFound("backstop event", id)
	}
	return e, nil
}

func (r *repos) GetActiveBackstopEvent(_ context.Context, feederID uuid.UUID) (domain.BackstopEvent, error) {
	defer r.lock()()
	for _, e := range r.s.st.backstops {
		if e.FeederID == feederID && e.ClearedAt == nil {
			return e, nil
		}
	}
	return domain.BackstopEvent{}, notFound("active backstop of feeder", feederID)
}

func (r *repos) ListBackstopEvents(_ context.Context, feederID uuid.UUID, page domain.Page) ([]domain.BackstopEvent, string, error) {
	defer r.lock()()
	after, err := pagetoken.Decode(page.Token, 2)
	if err != nil {
		return nil, "", err
	}
	var beforeTime time.Time
	if after[0] != "" {
		if beforeTime, err = parseTimeKey(after[0]); err != nil {
			return nil, "", err
		}
	}
	rows := sorted(r.s.st.backstops,
		func(e domain.BackstopEvent) bool {
			if e.FeederID != feederID {
				return false
			}
			if after[0] == "" {
				return true
			}
			if c := e.TriggeredAt.Compare(beforeTime); c != 0 {
				return c < 0
			}
			return e.ID.String() < after[1]
		},
		func(a, b domain.BackstopEvent) int {
			return cmp.Or(b.TriggeredAt.Compare(a.TriggeredAt), cmp.Compare(b.ID.String(), a.ID.String()))
		})
	rows, next := pagetoken.Next(limit(rows, page.Size+1), page.Size,
		func(e domain.BackstopEvent) []string { return []string{timeKey(e.TriggeredAt), e.ID.String()} })
	return rows, next, nil
}

func (r *repos) CreateBackstopEvent(_ context.Context, event domain.BackstopEvent, siteIDs []uuid.UUID) (domain.BackstopEvent, error) {
	defer r.lock()()
	if _, ok := r.s.st.feeders[event.FeederID]; !ok {
		return domain.BackstopEvent{}, fmt.Errorf("backstop_events_feeder_fkey: %w", domain.ErrFailedPrecondition)
	}
	if event.Reason == "" || event.ExportLimitW < 0 {
		return domain.BackstopEvent{}, fmt.Errorf("backstop_events_reason_present: %w", domain.ErrInvalid)
	}
	for _, e := range r.s.st.backstops {
		if e.FeederID == event.FeederID && e.ClearedAt == nil {
			return domain.BackstopEvent{}, fmt.Errorf("backstop_events_one_active_key: %w", domain.ErrAlreadyExists)
		}
	}
	for _, id := range siteIDs {
		if _, ok := r.s.st.sites[id]; !ok {
			return domain.BackstopEvent{}, fmt.Errorf("backstop_event_sites_site_fkey: %w", domain.ErrFailedPrecondition)
		}
	}
	now := r.now()
	event.ID = uuid.Must(uuid.NewV7())
	event.TriggeredAt = event.TriggeredAt.UTC()
	event.ClearedAt, event.ClearedBy = nil, nil
	event.CreatedAt, event.UpdatedAt = now, now
	r.s.st.backstops[event.ID] = event
	sites := slices.Clone(siteIDs)
	slices.SortFunc(sites, func(a, b uuid.UUID) int { return cmp.Compare(a.String(), b.String()) })
	r.s.st.backstopSites[event.ID] = sites
	return event, nil
}

func (r *repos) ListBackstopEventSiteIDs(_ context.Context, id uuid.UUID) ([]uuid.UUID, error) {
	defer r.lock()()
	return slices.Clone(r.s.st.backstopSites[id]), nil
}

func (r *repos) ClearBackstopEvent(_ context.Context, id uuid.UUID, by string, at time.Time) (domain.BackstopEvent, error) {
	defer r.lock()()
	e, ok := r.s.st.backstops[id]
	if !ok {
		return domain.BackstopEvent{}, notFound("backstop event", id)
	}
	if e.ClearedAt != nil {
		return domain.BackstopEvent{}, fmt.Errorf("%w: backstop %s is already cleared", domain.ErrFailedPrecondition, id)
	}
	// A backstop cannot end before, or at the instant, it began.
	cleared := at.UTC()
	if !cleared.After(e.TriggeredAt) {
		cleared = e.TriggeredAt.Add(time.Microsecond)
	}
	e.ClearedAt, e.ClearedBy, e.UpdatedAt = &cleared, &by, r.now()
	r.s.st.backstops[id] = e
	return e, nil
}

func (r *repos) ListActiveEnvelopes(_ context.Context, siteIDs []uuid.UUID, from time.Time) ([]domain.Envelope, error) {
	defer r.lock()()
	rows := sorted(r.s.st.envelopes,
		func(e domain.Envelope) bool {
			return slices.Contains(siteIDs, e.SiteID) && e.SupersededAt == nil && e.ValidTo.After(from)
		},
		func(a, b domain.Envelope) int {
			return cmp.Or(cmp.Compare(a.SiteID.String(), b.SiteID.String()), a.ValidFrom.Compare(b.ValidFrom))
		})
	return rows, nil
}

func (r *repos) ListLatestEngineEnvelopes(_ context.Context, siteIDs []uuid.UUID, from time.Time) ([]domain.Envelope, error) {
	defer r.lock()()
	type slot struct {
		site uuid.UUID
		from int64
	}
	latest := map[slot]domain.Envelope{}
	for _, e := range r.s.st.envelopes {
		if !slices.Contains(siteIDs, e.SiteID) || e.Source != domain.SourceEngine || !e.ValidTo.After(from) {
			continue
		}
		k := slot{e.SiteID, e.ValidFrom.UnixNano()}
		// The id is a UUIDv7: a later row has a greater id.
		if have, ok := latest[k]; !ok || e.CreatedAt.After(have.CreatedAt) ||
			(e.CreatedAt.Equal(have.CreatedAt) && e.ID.String() > have.ID.String()) {
			latest[k] = e
		}
	}
	rows := make([]domain.Envelope, 0, len(latest))
	for _, e := range latest {
		rows = append(rows, e)
	}
	slices.SortFunc(rows, func(a, b domain.Envelope) int {
		return cmp.Or(cmp.Compare(a.SiteID.String(), b.SiteID.String()), a.ValidFrom.Compare(b.ValidFrom))
	})
	return rows, nil
}

func (r *repos) SupersedeBackstopEnvelopes(_ context.Context, eventID uuid.UUID, from time.Time) (int, error) {
	defer r.lock()()
	now := r.now()
	n := 0
	for id, e := range r.s.st.envelopes {
		if e.BackstopEventID != nil && *e.BackstopEventID == eventID && e.SupersededAt == nil && e.ValidTo.After(from) {
			e.SupersededAt = &now
			r.s.st.envelopes[id] = e
			n++
		}
	}
	return n, nil
}
