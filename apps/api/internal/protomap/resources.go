package protomap

import (
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/internal/domain"
)

// optionalID renders a nullable uuid column.
func optionalID(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

// Feeder converts a feeder to its message.
func Feeder(f domain.Feeder) *doelabv1.Feeder {
	return &doelabv1.Feeder{
		Id: f.ID.String(), Code: f.Code, Name: f.Name,
		NominalVoltageV: f.NominalVoltageV, TransformerKva: f.TransformerKVA,
		SourceVoltageV: f.SourceVoltageV, SourceAngleDeg: f.SourceAngleDeg,
		SourceROhm: f.SourceROhm, SourceXOhm: f.SourceXOhm, TapPu: f.TapPU,
		Timezone: f.Timezone, Attribution: f.Attribution,
		CreatedAt: timestamppb.New(f.CreatedAt), UpdatedAt: timestamppb.New(f.UpdatedAt),
	}
}

// FeederNode converts a feeder node to its message.
func FeederNode(n domain.FeederNode) *doelabv1.FeederNode {
	return &doelabv1.FeederNode{
		Id: n.ID.String(), FeederId: n.FeederID.String(), Name: n.Name,
		ParentNodeId: optionalID(n.ParentNodeID),
		GroundROhm:   n.GroundROhm, GroundXOhm: n.GroundXOhm,
		CreatedAt: timestamppb.New(n.CreatedAt), UpdatedAt: timestamppb.New(n.UpdatedAt),
	}
}

// FeederLine converts a feeder line to its message.
func FeederLine(l domain.FeederLine) *doelabv1.FeederLine {
	out := &doelabv1.FeederLine{
		Id: l.ID.String(), FeederId: l.FeederID.String(), Name: l.Name,
		FromNodeId: l.FromNodeID.String(), ToNodeId: l.ToNodeID.String(),
		Linecode: l.Linecode, LengthM: l.LengthM, IsSwitch: l.IsSwitch,
		ROhm: l.ROhm, XOhm: l.XOhm, BS: l.BS,
		AmpacityA: l.AmpacityA,
		CreatedAt: timestamppb.New(l.CreatedAt), UpdatedAt: timestamppb.New(l.UpdatedAt),
	}
	if l.AmpacitySource != nil {
		source := ampacitySources.toProto[*l.AmpacitySource]
		out.AmpacitySource = &source
	}
	return out
}

// Site converts a site to its message. deleted_at is not on the wire.
func Site(s domain.Site) *doelabv1.Site {
	return &doelabv1.Site{
		Id: s.ID.String(), Nmi: s.NMI, FeederId: s.FeederID.String(), NodeId: s.NodeID.String(),
		Name: s.Name, Phase: int32(s.Phase),
		PvKw: s.PVKW, InverterKva: s.InverterKVA, ExportCapW: s.ExportCapW, ImportCapW: s.ImportCapW,
		HasBattery: s.HasBattery, BatteryKwh: s.BatteryKWh, HasEv: s.HasEV,
		ProfileCustomer: s.ProfileCustomer,
		CreatedAt:       timestamppb.New(s.CreatedAt), UpdatedAt: timestamppb.New(s.UpdatedAt),
	}
}

// SiteFromProto converts the client-set fields of a site message. The ids
// have passed validation as UUIDs; one that still fails to parse becomes the
// nil UUID, which names no row.
func SiteFromProto(p *doelabv1.Site) domain.Site {
	return domain.Site{
		NMI: p.GetNmi(), FeederID: parseID(p.GetFeederId()), NodeID: parseID(p.GetNodeId()),
		Name: p.GetName(), Phase: int16(p.GetPhase()), //nolint:gosec // G115: validated as 1 to 3
		PVKW: p.GetPvKw(), InverterKVA: p.GetInverterKva(),
		ExportCapW: p.GetExportCapW(), ImportCapW: p.GetImportCapW(),
		HasBattery: p.GetHasBattery(), BatteryKWh: p.BatteryKwh, HasEV: p.GetHasEv(),
		ProfileCustomer: p.ProfileCustomer,
	}
}

// SiteProfile converts a profile row to its message.
func SiteProfile(p domain.SiteProfile) *doelabv1.SiteProfile {
	return &doelabv1.SiteProfile{
		SiteId: p.SiteID.String(), Ts: timestamppb.New(p.TS),
		LoadW: p.LoadW, PvW: p.PVW, ControlledLoadW: p.ControlledLoadW,
	}
}

// Device converts a device to its message. deleted_at is not on the wire.
func Device(d domain.Device) *doelabv1.Device {
	return &doelabv1.Device{
		Id: d.ID.String(), SiteId: d.SiteID.String(),
		DerType: DERTypeToProto(d.DERType), RatedW: d.RatedW,
		CreatedAt: timestamppb.New(d.CreatedAt), UpdatedAt: timestamppb.New(d.UpdatedAt),
	}
}

// DeviceFromProto converts the client-set fields of a device message.
func DeviceFromProto(p *doelabv1.Device) domain.Device {
	return domain.Device{
		SiteID: parseID(p.GetSiteId()), DERType: DERTypeFromProto(p.GetDerType()), RatedW: p.GetRatedW(),
	}
}

// EnvelopeConfig converts a config version to its message.
func EnvelopeConfig(c domain.EnvelopeConfig) *doelabv1.EnvelopeConfig {
	return &doelabv1.EnvelopeConfig{
		Id: c.ID.String(), FeederId: c.FeederID.String(), Version: c.Version,
		Policy: EnvelopePolicyToProto(c.Policy),
		VMinPu: c.VMinPU, VMaxPu: c.VMaxPU,
		TransformerLimitPct: c.TransformerLimitPct, LineLimitPct: c.LineLimitPct,
		PvScale: c.PVScale, StaticLimitW: c.StaticLimitW,
		IntervalMinutes: c.IntervalMinutes, HorizonIntervals: c.HorizonIntervals,
		BreachGraceSeconds: c.BreachGraceSeconds, OfflineAfterSeconds: c.OfflineAfterSeconds,
		Note: c.Note, CreatedBy: c.CreatedBy, CreatedAt: timestamppb.New(c.CreatedAt),
	}
}

// EnvelopeConfigFromProto converts the client-set fields of a config message.
// The version, the author and the time are the server's to set.
func EnvelopeConfigFromProto(p *doelabv1.EnvelopeConfig) domain.EnvelopeConfig {
	return domain.EnvelopeConfig{
		FeederID: parseID(p.GetFeederId()), Policy: EnvelopePolicyFromProto(p.GetPolicy()),
		VMinPU: p.GetVMinPu(), VMaxPU: p.GetVMaxPu(),
		TransformerLimitPct: p.GetTransformerLimitPct(), LineLimitPct: p.GetLineLimitPct(),
		PVScale: p.GetPvScale(), StaticLimitW: p.GetStaticLimitW(),
		IntervalMinutes: p.GetIntervalMinutes(), HorizonIntervals: p.GetHorizonIntervals(),
		BreachGraceSeconds: p.GetBreachGraceSeconds(), OfflineAfterSeconds: p.GetOfflineAfterSeconds(),
		Note: p.GetNote(),
	}
}

// parseID parses a UUID that validation has already accepted. Anything else
// becomes the nil UUID, which names no row.
func parseID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}

// Slice converts a slice of domain values with one of the functions above.
func Slice[D, P any](in []D, convert func(D) P) []P {
	out := make([]P, len(in))
	for i, v := range in {
		out[i] = convert(v)
	}
	return out
}
