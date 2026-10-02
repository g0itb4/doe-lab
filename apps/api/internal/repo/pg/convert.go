package pg

import (
	"doelab/api/internal/domain"
	"doelab/api/internal/repo/pg/gen"
)

// Row to domain, one function per table. Field by field on purpose: when a
// column is added, the compiler points here.

func substationFromRow(r gen.Substation) domain.Substation {
	return domain.Substation{
		ID: r.ID, Code: r.Code, Name: r.Name, DNSP: r.Dnsp, State: r.State,
		LatitudeDeg: r.LatitudeDeg, LongitudeDeg: r.LongitudeDeg,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func feederFromRow(r gen.Feeder) domain.Feeder {
	return domain.Feeder{
		ID: r.ID, Code: r.Code, Name: r.Name,
		NominalVoltageV: r.NominalVoltageV, TransformerKVA: r.TransformerKva,
		SourceVoltageV: r.SourceVoltageV, SourceAngleDeg: r.SourceAngleDeg,
		SourceROhm: r.SourceROhm, SourceXOhm: r.SourceXOhm, TapPU: r.TapPu,
		Timezone: r.Timezone, Attribution: r.Attribution,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		SubstationID: r.SubstationID,
	}
}

func feederNodeFromRow(r gen.FeederNode) domain.FeederNode {
	return domain.FeederNode{
		ID: r.ID, FeederID: r.FeederID, Name: r.Name, ParentNodeID: r.ParentNodeID,
		GroundROhm: r.GroundROhm, GroundXOhm: r.GroundXOhm,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func feederLineFromRow(r gen.FeederLine) domain.FeederLine {
	return domain.FeederLine{
		ID: r.ID, FeederID: r.FeederID, Name: r.Name,
		FromNodeID: r.FromNodeID, ToNodeID: r.ToNodeID,
		Linecode: r.Linecode, LengthM: r.LengthM, IsSwitch: r.IsSwitch,
		ROhm: r.ROhm, XOhm: r.XOhm, BS: r.BS,
		AmpacityA: r.AmpacityA, AmpacitySource: (*domain.AmpacitySource)(r.AmpacitySource),
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func siteFromRow(r gen.Site) domain.Site {
	return domain.Site{
		ID: r.ID, NMI: r.Nmi, FeederID: r.FeederID, NodeID: r.NodeID, Name: r.Name, Phase: r.Phase,
		PVKW: r.PvKw, InverterKVA: r.InverterKva, ExportCapW: r.ExportCapW, ImportCapW: r.ImportCapW,
		HasBattery: r.HasBattery, BatteryKWh: r.BatteryKwh, HasEV: r.HasEv,
		ProfileCustomer: r.ProfileCustomer,
		CreatedAt:       r.CreatedAt, UpdatedAt: r.UpdatedAt, DeletedAt: r.DeletedAt,
		LatitudeDeg: r.LatitudeDeg, LongitudeDeg: r.LongitudeDeg,
	}
}

func deviceFromRow(r gen.Device) domain.Device {
	return domain.Device{
		ID: r.ID, SiteID: r.SiteID, DERType: domain.DERType(r.DerType), RatedW: r.RatedW,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, DeletedAt: r.DeletedAt,
	}
}

func siteProfileFromRow(r gen.SiteProfile) domain.SiteProfile {
	return domain.SiteProfile{
		SiteID: r.SiteID, TS: r.Ts, LoadW: r.LoadW, PVW: r.PvW, ControlledLoadW: r.ControlledLoadW,
	}
}

func envelopeConfigFromRow(r gen.EnvelopeConfig) domain.EnvelopeConfig {
	return domain.EnvelopeConfig{
		ID: r.ID, FeederID: r.FeederID, Version: r.Version, Policy: domain.EnvelopePolicy(r.Policy),
		VMinPU: r.VMinPu, VMaxPU: r.VMaxPu,
		TransformerLimitPct: r.TransformerLimitPct, LineLimitPct: r.LineLimitPct,
		PVScale: r.PvScale, StaticLimitW: r.StaticLimitW,
		IntervalMinutes: r.IntervalMinutes, HorizonIntervals: r.HorizonIntervals,
		BreachGraceSeconds: r.BreachGraceSeconds, OfflineAfterSeconds: r.OfflineAfterSeconds,
		Note: r.Note, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt,
	}
}

func feederNodeStateFromRow(r gen.FeederNodeState) domain.FeederNodeState {
	return domain.FeederNodeState{
		FeederID: r.FeederID, NodeID: r.NodeID, ValidFrom: r.ValidFrom, ValidTo: r.ValidTo,
		EnvelopeRunID: r.EnvelopeRunID,
		ForecastVPU:   r.ForecastVPu, EnvelopeVPU: r.EnvelopeVPu, StaticVPU: r.StaticVPu,
	}
}

func feederLineStateFromRow(r gen.FeederLineState) domain.FeederLineState {
	return domain.FeederLineState{
		FeederID: r.FeederID, LineID: r.LineID, ValidFrom: r.ValidFrom, ValidTo: r.ValidTo,
		EnvelopeRunID:    r.EnvelopeRunID,
		ForecastCurrentA: r.ForecastCurrentA, EnvelopeCurrentA: r.EnvelopeCurrentA, StaticCurrentA: r.StaticCurrentA,
		ForecastPowerW: r.ForecastPowerW, EnvelopePowerW: r.EnvelopePowerW, StaticPowerW: r.StaticPowerW,
	}
}
