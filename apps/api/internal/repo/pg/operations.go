package pg

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
	"doelab/api/internal/repo/pagetoken"
	"doelab/api/internal/repo/pg/gen"
	"doelab/api/internal/service"
)

func runIntervalFromRow(r gen.EnvelopeRunInterval) domain.EnvelopeRunInterval {
	return domain.EnvelopeRunInterval{
		EnvelopeRunID: r.EnvelopeRunID, FeederID: r.FeederID, ValidFrom: r.ValidFrom, ValidTo: r.ValidTo,
		ForecastNetLoadW: r.ForecastNetLoadW, ForecastLoadingPct: r.ForecastLoadingPct,
		ForecastVMinPU: r.ForecastVMinPu, ForecastVMaxPU: r.ForecastVMaxPu,
		ExportLimitTotalW: r.ExportLimitTotalW, ImportLimitTotalW: r.ImportLimitTotalW,
		StaticLimitTotalW: r.StaticLimitTotalW, StaticVMaxPU: r.StaticVMaxPu,
		StaticBinding: domain.BindingConstraint(r.StaticBinding), StaticBindingElement: r.StaticBindingElement,
		CreatedAt: r.CreatedAt, EnvelopeVMaxPU: r.EnvelopeVMaxPu,
	}
}

func readingFromRow(r gen.Reading) domain.Reading {
	return domain.Reading{
		DeviceID: r.DeviceID, SiteID: r.SiteID, TS: r.Ts, PowerW: r.PowerW, NetExportW: r.NetExportW,
		SOCPct: r.SocPct, VoltageV: r.VoltageV, ReceivedAt: r.ReceivedAt,
	}
}

func sitePowerFromRow(r gen.SitePower1m) domain.SitePower {
	return domain.SitePower{
		SiteID: r.SiteID, Bucket: r.Bucket, AvgNetExportW: r.AvgNetExportW, MaxNetExportW: r.MaxNetExportW,
		AvgSOCPct: r.AvgSocPct, AvgVoltageV: r.AvgVoltageV, ReadingCount: r.ReadingCount,
	}
}

func fleetMinuteFromRow(r gen.Fleet1m) domain.FleetMinute {
	return domain.FleetMinute{
		FeederID: r.FeederID, Bucket: r.Bucket, ExportW: r.ExportW, ImportW: r.ImportW,
		AvgSOCPct: r.AvgSocPct, ReportingSites: r.ReportingSites, ReadingCount: r.ReadingCount,
	}
}

func alertFromRow(r gen.Alert) domain.Alert {
	return domain.Alert{
		ID: r.ID, SiteID: r.SiteID, FeederID: r.FeederID, DeviceID: r.DeviceID,
		Kind: domain.AlertKind(r.Kind), Severity: domain.AlertSeverity(r.Severity),
		OpenedAt: r.OpenedAt, ResolvedAt: r.ResolvedAt,
		AcknowledgedAt: r.AcknowledgedAt, AcknowledgedBy: r.AcknowledgedBy,
		LimitW: r.LimitW, PeakW: r.PeakW, Detail: r.Detail,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func backstopEventFromRow(r gen.BackstopEvent) domain.BackstopEvent {
	return domain.BackstopEvent{
		ID: r.ID, FeederID: r.FeederID, Reason: r.Reason, ExportLimitW: r.ExportLimitW,
		TriggeredBy: r.TriggeredBy, TriggeredAt: r.TriggeredAt, ClearedBy: r.ClearedBy, ClearedAt: r.ClearedAt,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func (r *repos) CreateEnvelopeRunIntervals(ctx context.Context, rows []domain.EnvelopeRunInterval) error {
	params := make([]gen.InsertEnvelopeRunIntervalsParams, len(rows))
	for i, row := range rows {
		params[i] = gen.InsertEnvelopeRunIntervalsParams{
			EnvelopeRunID: row.EnvelopeRunID, FeederID: row.FeederID, ValidFrom: row.ValidFrom, ValidTo: row.ValidTo,
			ForecastNetLoadW: row.ForecastNetLoadW, ForecastLoadingPct: row.ForecastLoadingPct,
			ForecastVMinPu: row.ForecastVMinPU, ForecastVMaxPu: row.ForecastVMaxPU,
			ExportLimitTotalW: row.ExportLimitTotalW, ImportLimitTotalW: row.ImportLimitTotalW,
			StaticLimitTotalW: row.StaticLimitTotalW, StaticVMaxPu: row.StaticVMaxPU,
			StaticBinding: gen.BindingConstraint(row.StaticBinding), StaticBindingElement: row.StaticBindingElement,
			EnvelopeVMaxPu: row.EnvelopeVMaxPU,
		}
	}
	_, err := r.q.InsertEnvelopeRunIntervals(ctx, params)
	return pgErr(err)
}

func (r *repos) ListEnvelopeRunIntervals(ctx context.Context, runID uuid.UUID) ([]domain.EnvelopeRunInterval, error) {
	rows, err := r.q.ListEnvelopeRunIntervals(ctx, runID)
	return many(rows, err, runIntervalFromRow)
}

func (r *repos) ListFeederIntervals(ctx context.Context, feederID uuid.UUID, from, to time.Time) ([]domain.EnvelopeRunInterval, error) {
	rows, err := r.q.ListFeederIntervals(ctx, gen.ListFeederIntervalsParams{FeederID: feederID, FromTs: from, ToTs: to})
	return many(rows, err, runIntervalFromRow)
}

// orNaN is how an absent value travels in a float8[] parameter; the query
// turns it back into NULL.
func orNaN(v *float64) float64 {
	if v == nil {
		return math.NaN()
	}
	return *v
}

func (r *repos) InsertReadings(ctx context.Context, readings []domain.Reading) (int, error) {
	n := len(readings)
	in := gen.InsertReadingsParams{
		DeviceIds: make([]uuid.UUID, n), Tss: make([]time.Time, n),
		PowerWs: make([]float64, n), NetExportWs: make([]float64, n),
		SocPcts: make([]float64, n), VoltageVs: make([]float64, n),
	}
	for i, reading := range readings {
		in.DeviceIds[i], in.Tss[i] = reading.DeviceID, reading.TS
		in.PowerWs[i], in.NetExportWs[i] = reading.PowerW, reading.NetExportW
		in.SocPcts[i], in.VoltageVs[i] = orNaN(reading.SOCPct), orNaN(reading.VoltageV)
	}
	stored, err := r.q.InsertReadings(ctx, in)
	if err != nil {
		return 0, pgErr(err)
	}

	// The status of each device moves to its latest reading of the batch.
	// One row per device: an upsert cannot touch a row twice in a statement.
	latest := map[uuid.UUID]domain.Reading{}
	for _, reading := range readings {
		if have, ok := latest[reading.DeviceID]; !ok || reading.TS.After(have.TS) {
			latest[reading.DeviceID] = reading
		}
	}
	status := gen.UpsertDeviceStatusParams{}
	for device, reading := range latest {
		status.DeviceIds = append(status.DeviceIds, device)
		status.Tss = append(status.Tss, reading.TS)
		status.PowerWs = append(status.PowerWs, reading.PowerW)
		status.NetExportWs = append(status.NetExportWs, reading.NetExportW)
	}
	if err := r.q.UpsertDeviceStatus(ctx, status); err != nil {
		return 0, pgErr(err)
	}
	return int(stored), nil
}

func (r *repos) ListReadings(ctx context.Context, deviceID uuid.UUID, from, to time.Time, page domain.Page) ([]domain.Reading, string, error) {
	after, err := pagetoken.Decode(page.Token, 1)
	if err != nil {
		return nil, "", err
	}
	afterTime := beginning
	if after[0] != "" {
		if afterTime, err = time.Parse(time.RFC3339Nano, after[0]); err != nil {
			return nil, "", fmt.Errorf("%w: malformed page token", domain.ErrInvalid)
		}
	}
	rows, err := r.q.ListReadings(ctx, gen.ListReadingsParams{
		DeviceID: deviceID, FromTs: from, ToTs: to, AfterTs: afterTime, PageSize: page.Size + 1,
	})
	out, err := many(rows, err, readingFromRow)
	if err != nil {
		return nil, "", err
	}
	out, next := pagetoken.Next(out, page.Size, func(reading domain.Reading) []string { return []string{timeKey(reading.TS)} })
	return out, next, nil
}

func (r *repos) ListSitePower(ctx context.Context, siteID uuid.UUID, from, to time.Time) ([]domain.SitePower, error) {
	rows, err := r.q.ListSitePower(ctx, gen.ListSitePowerParams{SiteID: siteID, FromTs: from, ToTs: to})
	return many(rows, err, sitePowerFromRow)
}

func (r *repos) ListFleetSeries(ctx context.Context, feederID uuid.UUID, from, to time.Time) ([]domain.FleetMinute, error) {
	rows, err := r.q.ListFleetSeries(ctx, gen.ListFleetSeriesParams{FeederID: feederID, FromTs: from, ToTs: to})
	return many(rows, err, fleetMinuteFromRow)
}

func (r *repos) ListDeviceStates(ctx context.Context, feederID uuid.UUID) ([]domain.DeviceState, error) {
	rows, err := r.q.ListDeviceStates(ctx, feederID)
	return many(rows, err, func(row gen.ListDeviceStatesRow) domain.DeviceState {
		state := domain.DeviceState{
			DeviceID: row.DeviceID, SiteID: row.SiteID, DERType: domain.DERType(row.DerType), NMI: row.Nmi,
			LastSeenAt: row.LastSeenAt,
		}
		if row.PowerW != nil && row.NetExportW != nil {
			state.PowerW, state.NetExportW = *row.PowerW, *row.NetExportW
		}
		return state
	})
}

func (r *repos) GetAlert(ctx context.Context, id uuid.UUID) (domain.Alert, error) {
	row, err := r.q.GetAlert(ctx, id)
	return one(row, err, alertFromRow)
}

func (r *repos) ListAlerts(ctx context.Context, feederID uuid.UUID, filter service.AlertFilter, page domain.Page) ([]domain.Alert, string, error) {
	beforeTime, beforeID, err := timeAndID(page.Token, end, lastID)
	if err != nil {
		return nil, "", err
	}
	rows, err := r.q.ListAlerts(ctx, gen.ListAlertsParams{
		FeederID: feederID, BeforeOpenedAt: beforeTime, BeforeID: beforeID,
		SiteID: filter.SiteID, Kind: (*gen.AlertKind)(filter.Kind), OpenOnly: filter.OpenOnly, PageSize: page.Size + 1,
	})
	out, err := many(rows, err, alertFromRow)
	if err != nil {
		return nil, "", err
	}
	out, next := pagetoken.Next(out, page.Size,
		func(a domain.Alert) []string { return []string{timeKey(a.OpenedAt), a.ID.String()} })
	return out, next, nil
}

func (r *repos) OpenAlert(ctx context.Context, alert domain.Alert) (domain.Alert, bool, error) {
	row, err := r.q.OpenAlert(ctx, gen.OpenAlertParams{
		SiteID: alert.SiteID, FeederID: alert.FeederID, DeviceID: alert.DeviceID,
		Kind: gen.AlertKind(alert.Kind), Severity: gen.AlertSeverity(alert.Severity),
		OpenedAt: alert.OpenedAt, LimitW: alert.LimitW, PeakW: alert.PeakW, Detail: alert.Detail,
	})
	if err != nil {
		return domain.Alert{}, false, pgErr(err)
	}
	return domain.Alert{
		ID: row.ID, SiteID: row.SiteID, FeederID: row.FeederID, DeviceID: row.DeviceID,
		Kind: domain.AlertKind(row.Kind), Severity: domain.AlertSeverity(row.Severity),
		OpenedAt: row.OpenedAt, ResolvedAt: row.ResolvedAt,
		AcknowledgedAt: row.AcknowledgedAt, AcknowledgedBy: row.AcknowledgedBy,
		LimitW: row.LimitW, PeakW: row.PeakW, Detail: row.Detail,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, row.Opened, nil
}

func (r *repos) ResolveAlert(ctx context.Context, siteID uuid.UUID, kind domain.AlertKind, at time.Time) (bool, error) {
	n, err := r.q.ResolveAlert(ctx, gen.ResolveAlertParams{SiteID: siteID, Kind: gen.AlertKind(kind), ResolvedAt: at})
	return n > 0, pgErr(err)
}

func (r *repos) AcknowledgeAlert(ctx context.Context, id uuid.UUID, by string) (domain.Alert, error) {
	row, err := r.q.AcknowledgeAlert(ctx, gen.AcknowledgeAlertParams{ID: id, AcknowledgedBy: &by})
	if err == nil {
		return alertFromRow(row), nil
	}
	if !errors.Is(pgErr(err), domain.ErrNotFound) {
		return domain.Alert{}, pgErr(err)
	}
	// Nothing was updated: the alert is missing, or it is acknowledged
	// already, in which case it is returned as it is.
	return r.GetAlert(ctx, id)
}

func (r *repos) CountOpenAlerts(ctx context.Context, feederID uuid.UUID) (int, error) {
	n, err := r.q.CountOpenAlerts(ctx, feederID)
	return int(n), pgErr(err)
}

func (r *repos) GetBackstopEvent(ctx context.Context, id uuid.UUID) (domain.BackstopEvent, error) {
	row, err := r.q.GetBackstopEvent(ctx, id)
	return one(row, err, backstopEventFromRow)
}

func (r *repos) GetActiveBackstopEvent(ctx context.Context, feederID uuid.UUID) (domain.BackstopEvent, error) {
	row, err := r.q.GetActiveBackstopEvent(ctx, feederID)
	return one(row, err, backstopEventFromRow)
}

func (r *repos) ListBackstopEvents(ctx context.Context, feederID uuid.UUID, page domain.Page) ([]domain.BackstopEvent, string, error) {
	beforeTime, beforeID, err := timeAndID(page.Token, end, lastID)
	if err != nil {
		return nil, "", err
	}
	rows, err := r.q.ListBackstopEvents(ctx, gen.ListBackstopEventsParams{
		FeederID: feederID, BeforeTriggeredAt: beforeTime, BeforeID: beforeID, PageSize: page.Size + 1,
	})
	out, err := many(rows, err, backstopEventFromRow)
	if err != nil {
		return nil, "", err
	}
	out, next := pagetoken.Next(out, page.Size,
		func(e domain.BackstopEvent) []string { return []string{timeKey(e.TriggeredAt), e.ID.String()} })
	return out, next, nil
}

func (r *repos) CreateBackstopEvent(ctx context.Context, event domain.BackstopEvent, siteIDs []uuid.UUID) (domain.BackstopEvent, error) {
	row, err := r.q.CreateBackstopEvent(ctx, gen.CreateBackstopEventParams{
		FeederID: event.FeederID, Reason: event.Reason, ExportLimitW: event.ExportLimitW,
		TriggeredBy: event.TriggeredBy, TriggeredAt: event.TriggeredAt,
	})
	if err != nil {
		return domain.BackstopEvent{}, pgErr(err)
	}
	if err := r.q.InsertBackstopEventSites(ctx, gen.InsertBackstopEventSitesParams{BackstopEventID: row.ID, SiteIds: siteIDs}); err != nil {
		return domain.BackstopEvent{}, pgErr(err)
	}
	return backstopEventFromRow(row), nil
}

func (r *repos) ListBackstopEventSiteIDs(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error) {
	ids, err := r.q.ListBackstopEventSiteIDs(ctx, id)
	return ids, pgErr(err)
}

func (r *repos) ClearBackstopEvent(ctx context.Context, id uuid.UUID, by string, at time.Time) (domain.BackstopEvent, error) {
	row, err := r.q.ClearBackstopEvent(ctx, gen.ClearBackstopEventParams{ID: id, ClearedBy: &by, ClearedAt: at})
	if err == nil {
		return backstopEventFromRow(row), nil
	}
	if !errors.Is(pgErr(err), domain.ErrNotFound) {
		return domain.BackstopEvent{}, pgErr(err)
	}
	// Nothing was updated: the backstop is missing, or already cleared.
	if _, getErr := r.q.GetBackstopEvent(ctx, id); getErr != nil {
		return domain.BackstopEvent{}, pgErr(getErr)
	}
	return domain.BackstopEvent{}, fmt.Errorf("%w: backstop %s is already cleared", domain.ErrFailedPrecondition, id)
}

func (r *repos) ListActiveEnvelopes(ctx context.Context, siteIDs []uuid.UUID, from time.Time) ([]domain.Envelope, error) {
	rows, err := r.q.ListActiveEnvelopes(ctx, gen.ListActiveEnvelopesParams{SiteIds: siteIDs, FromTs: from})
	return many(rows, err, envelopeFromRow)
}

func (r *repos) ListLatestEngineEnvelopes(ctx context.Context, siteIDs []uuid.UUID, from time.Time) ([]domain.Envelope, error) {
	rows, err := r.q.ListLatestEngineEnvelopes(ctx, gen.ListLatestEngineEnvelopesParams{SiteIds: siteIDs, FromTs: from})
	return many(rows, err, envelopeFromRow)
}

func (r *repos) SupersedeBackstopEnvelopes(ctx context.Context, eventID uuid.UUID, from time.Time) (int, error) {
	n, err := r.q.SupersedeBackstopEnvelopes(ctx, gen.SupersedeBackstopEnvelopesParams{BackstopEventID: &eventID, FromTs: from})
	return int(n), pgErr(err)
}

func (r *repos) PurgeBefore(ctx context.Context, readingsBefore, envelopesBefore, alertsBefore time.Time) (int, error) {
	deleted, err := r.q.PurgeBefore(ctx, gen.PurgeBeforeParams{
		ReadingsBefore: readingsBefore, EnvelopesBefore: envelopesBefore, AlertsBefore: alertsBefore,
	})
	return int(deleted), pgErr(err)
}
