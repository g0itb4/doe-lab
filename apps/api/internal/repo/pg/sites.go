package pg

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
	"doelab/api/internal/repo/pagetoken"
	"doelab/api/internal/repo/pg/gen"
	"doelab/api/internal/service"
)

func (r *repos) GetSite(ctx context.Context, id uuid.UUID) (domain.Site, error) {
	row, err := r.q.GetSite(ctx, id)
	return one(row, err, siteFromRow)
}

func (r *repos) GetSiteByNMI(ctx context.Context, nmi string) (domain.Site, error) {
	row, err := r.q.GetSiteByNMI(ctx, nmi)
	return one(row, err, siteFromRow)
}

func (r *repos) ListSites(ctx context.Context, feederID uuid.UUID, phase *int16, page domain.Page) ([]domain.Site, string, error) {
	after, err := pagetoken.Decode(page.Token, 1)
	if err != nil {
		return nil, "", err
	}
	rows, err := r.q.ListSites(ctx, gen.ListSitesParams{
		FeederID: feederID, AfterNmi: after[0], Phase: phase, PageSize: page.Size + 1,
	})
	out, err := many(rows, err, siteFromRow)
	if err != nil {
		return nil, "", err
	}
	out, next := pagetoken.Next(out, page.Size, func(s domain.Site) []string { return []string{s.NMI} })
	return out, next, nil
}

func (r *repos) ListAllSites(ctx context.Context, feederID uuid.UUID) ([]domain.Site, error) {
	rows, err := r.q.ListAllSites(ctx, feederID)
	return many(rows, err, siteFromRow)
}

func (r *repos) CreateSite(ctx context.Context, s domain.Site) (domain.Site, error) {
	row, err := r.q.CreateSite(ctx, gen.CreateSiteParams{
		Nmi: s.NMI, FeederID: s.FeederID, NodeID: s.NodeID, Name: s.Name, Phase: s.Phase,
		PvKw: s.PVKW, InverterKva: s.InverterKVA, ExportCapW: s.ExportCapW, ImportCapW: s.ImportCapW,
		HasBattery: s.HasBattery, BatteryKwh: s.BatteryKWh, HasEv: s.HasEV,
		ProfileCustomer: s.ProfileCustomer,
	})
	return one(row, err, siteFromRow)
}

func (r *repos) UpdateSite(ctx context.Context, s domain.Site) (domain.Site, error) {
	row, err := r.q.UpdateSite(ctx, gen.UpdateSiteParams{
		ID: s.ID, PvKw: s.PVKW, InverterKva: s.InverterKVA,
		ExportCapW: s.ExportCapW, ImportCapW: s.ImportCapW,
		HasBattery: s.HasBattery, BatteryKwh: s.BatteryKWh, HasEv: s.HasEV,
	})
	return one(row, err, siteFromRow)
}

func (r *repos) SoftDeleteSite(ctx context.Context, id uuid.UUID) error {
	_, err := r.q.SoftDeleteSite(ctx, id)
	return pgErr(err)
}

func (r *repos) ListSiteProfiles(ctx context.Context, siteID uuid.UUID, from, to time.Time, page domain.Page) ([]domain.SiteProfile, string, error) {
	after, err := pagetoken.Decode(page.Token, 1)
	if err != nil {
		return nil, "", err
	}
	if after[0] != "" {
		last, err := time.Parse(time.RFC3339Nano, after[0])
		if err != nil {
			return nil, "", fmt.Errorf("%w: malformed page token", domain.ErrInvalid)
		}
		// Postgres keeps microseconds: the next row is at least one later.
		from = last.Add(time.Microsecond)
	}
	rows, err := r.q.ListSiteProfiles(ctx, gen.ListSiteProfilesParams{
		SiteID: siteID, FromTs: from, ToTs: to, PageSize: page.Size + 1,
	})
	out, err := many(rows, err, siteProfileFromRow)
	if err != nil {
		return nil, "", err
	}
	out, next := pagetoken.Next(out, page.Size,
		func(p domain.SiteProfile) []string { return []string{p.TS.UTC().Format(time.RFC3339Nano)} })
	return out, next, nil
}

func (r *repos) ListFeederProfiles(ctx context.Context, feederID uuid.UUID, from, to time.Time) ([]domain.SiteProfile, error) {
	rows, err := r.q.ListFeederProfiles(ctx, gen.ListFeederProfilesParams{FeederID: feederID, FromTs: from, ToTs: to})
	return many(rows, err, siteProfileFromRow)
}

func (r *repos) ReplaceSiteProfiles(ctx context.Context, siteID uuid.UUID, rows []domain.SiteProfile) error {
	if err := r.q.DeleteSiteProfiles(ctx, siteID); err != nil {
		return pgErr(err)
	}
	params := make([]gen.InsertSiteProfilesParams, len(rows))
	for i, p := range rows {
		params[i] = gen.InsertSiteProfilesParams{
			SiteID: siteID, Ts: p.TS, LoadW: p.LoadW, PvW: p.PVW, ControlledLoadW: p.ControlledLoadW,
		}
	}
	_, err := r.q.InsertSiteProfiles(ctx, params)
	return pgErr(err)
}

func (r *repos) GetDevice(ctx context.Context, id uuid.UUID) (domain.Device, error) {
	row, err := r.q.GetDevice(ctx, id)
	return one(row, err, deviceFromRow)
}

func (r *repos) ListDevices(ctx context.Context, filter service.DeviceFilter, page domain.Page) ([]domain.Device, string, error) {
	after, err := pagetoken.Decode(page.Token, 1)
	if err != nil {
		return nil, "", err
	}
	afterID := uuid.Nil
	if after[0] != "" {
		if afterID, err = uuid.Parse(after[0]); err != nil {
			return nil, "", fmt.Errorf("%w: malformed page token", domain.ErrInvalid)
		}
	}
	rows, err := r.q.ListDevices(ctx, gen.ListDevicesParams{
		AfterID: afterID, SiteID: filter.SiteID, DerType: (*gen.DerType)(filter.DERType), PageSize: page.Size + 1,
	})
	out, err := many(rows, err, deviceFromRow)
	if err != nil {
		return nil, "", err
	}
	out, next := pagetoken.Next(out, page.Size, func(d domain.Device) []string { return []string{d.ID.String()} })
	return out, next, nil
}

func (r *repos) CreateDevice(ctx context.Context, d domain.Device) (domain.Device, error) {
	row, err := r.q.CreateDevice(ctx, gen.CreateDeviceParams{SiteID: d.SiteID, DerType: gen.DerType(d.DERType), RatedW: d.RatedW})
	return one(row, err, deviceFromRow)
}

func (r *repos) UpdateDevice(ctx context.Context, d domain.Device) (domain.Device, error) {
	row, err := r.q.UpdateDevice(ctx, gen.UpdateDeviceParams{ID: d.ID, RatedW: d.RatedW})
	return one(row, err, deviceFromRow)
}

func (r *repos) SoftDeleteDevice(ctx context.Context, id uuid.UUID) error {
	_, err := r.q.SoftDeleteDevice(ctx, id)
	return pgErr(err)
}

func (r *repos) GetEnvelopeConfig(ctx context.Context, id uuid.UUID) (domain.EnvelopeConfig, error) {
	row, err := r.q.GetEnvelopeConfig(ctx, id)
	return one(row, err, envelopeConfigFromRow)
}

func (r *repos) GetActiveEnvelopeConfig(ctx context.Context, feederID uuid.UUID) (domain.EnvelopeConfig, error) {
	row, err := r.q.GetActiveEnvelopeConfig(ctx, feederID)
	return one(row, err, envelopeConfigFromRow)
}

// noVersionLimit is above every version.
const noVersionLimit = int32(1<<31 - 1)

func (r *repos) ListEnvelopeConfigs(ctx context.Context, feederID uuid.UUID, page domain.Page) ([]domain.EnvelopeConfig, string, error) {
	after, err := pagetoken.Decode(page.Token, 1)
	if err != nil {
		return nil, "", err
	}
	before := noVersionLimit
	if after[0] != "" {
		v, err := strconv.ParseInt(after[0], 10, 32)
		if err != nil {
			return nil, "", fmt.Errorf("%w: malformed page token", domain.ErrInvalid)
		}
		before = int32(v)
	}
	rows, err := r.q.ListEnvelopeConfigs(ctx, gen.ListEnvelopeConfigsParams{
		FeederID: feederID, BeforeVersion: before, PageSize: page.Size + 1,
	})
	out, err := many(rows, err, envelopeConfigFromRow)
	if err != nil {
		return nil, "", err
	}
	out, next := pagetoken.Next(out, page.Size,
		func(c domain.EnvelopeConfig) []string { return []string{strconv.Itoa(int(c.Version))} })
	return out, next, nil
}

func (r *repos) CreateEnvelopeConfig(ctx context.Context, c domain.EnvelopeConfig) (domain.EnvelopeConfig, error) {
	row, err := r.q.CreateEnvelopeConfig(ctx, gen.CreateEnvelopeConfigParams{
		FeederID: c.FeederID, Policy: gen.EnvelopePolicy(c.Policy),
		VMinPu: c.VMinPU, VMaxPu: c.VMaxPU,
		TransformerLimitPct: c.TransformerLimitPct, LineLimitPct: c.LineLimitPct,
		PvScale: c.PVScale, StaticLimitW: c.StaticLimitW,
		IntervalMinutes: c.IntervalMinutes, HorizonIntervals: c.HorizonIntervals,
		BreachGraceSeconds: c.BreachGraceSeconds, OfflineAfterSeconds: c.OfflineAfterSeconds,
		Note: c.Note, CreatedBy: c.CreatedBy,
	})
	return one(row, err, envelopeConfigFromRow)
}
