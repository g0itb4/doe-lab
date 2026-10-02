package protomap

import (
	"time"

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

// Substation converts a substation to its message.
func Substation(s domain.Substation) *doelabv1.Substation {
	return &doelabv1.Substation{
		Id: s.ID.String(), Code: s.Code, Name: s.Name, Dnsp: s.DNSP, State: s.State,
		LatitudeDeg: s.LatitudeDeg, LongitudeDeg: s.LongitudeDeg,
		CreatedAt: timestamppb.New(s.CreatedAt), UpdatedAt: timestamppb.New(s.UpdatedAt),
	}
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
		SubstationId: optionalID(f.SubstationID),
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
		LatitudeDeg: s.LatitudeDeg, LongitudeDeg: s.LongitudeDeg,
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
		LatitudeDeg:     p.LatitudeDeg, LongitudeDeg: p.LongitudeDeg,
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

// optionalTime renders a nullable timestamptz column.
func optionalTime(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

// EnvelopeRun converts a run to its message.
func EnvelopeRun(r domain.EnvelopeRun) *doelabv1.EnvelopeRun {
	return &doelabv1.EnvelopeRun{
		Id: r.ID.String(), FeederId: r.FeederID.String(), EnvelopeConfigId: r.EnvelopeConfigID.String(),
		Status: RunStatusToProto(r.Status), IdempotencyKey: r.IdempotencyKey,
		HorizonFrom: timestamppb.New(r.HorizonFrom), HorizonTo: timestamppb.New(r.HorizonTo),
		StartedAt: timestamppb.New(r.StartedAt), CompletedAt: optionalTime(r.CompletedAt),
		DurationMs: r.DurationMS,
		SiteCount:  r.SiteCount, IntervalCount: r.IntervalCount, EnvelopeCount: r.EnvelopeCount,
		EngineVersion: r.EngineVersion, Error: r.Error,
		CreatedAt: timestamppb.New(r.CreatedAt), UpdatedAt: timestamppb.New(r.UpdatedAt),
	}
}

// EnvelopeRunFromProto converts the client-set fields of a run message.
func EnvelopeRunFromProto(p *doelabv1.EnvelopeRun) domain.EnvelopeRun {
	return domain.EnvelopeRun{
		FeederID: parseID(p.GetFeederId()), EnvelopeConfigID: parseID(p.GetEnvelopeConfigId()),
		IdempotencyKey: p.GetIdempotencyKey(),
		HorizonFrom:    p.GetHorizonFrom().AsTime(), HorizonTo: p.GetHorizonTo().AsTime(),
		EngineVersion: p.GetEngineVersion(),
	}
}

// Envelope converts an envelope to its message.
func Envelope(e domain.Envelope) *doelabv1.Envelope {
	return &doelabv1.Envelope{
		Id: e.ID.String(), SiteId: e.SiteID.String(),
		ValidFrom: timestamppb.New(e.ValidFrom), ValidTo: timestamppb.New(e.ValidTo),
		ExportLimitW: e.ExportLimitW, ImportLimitW: e.ImportLimitW,
		Source:        EnvelopeSourceToProto(e.Source),
		EnvelopeRunId: optionalID(e.EnvelopeRunID), BackstopEventId: optionalID(e.BackstopEventID),
		ExportBinding: BindingConstraintToProto(e.ExportBinding), ExportBindingElement: e.ExportBindingElement,
		ImportBinding: BindingConstraintToProto(e.ImportBinding), ImportBindingElement: e.ImportBindingElement,
		SupersededAt: optionalTime(e.SupersededAt), CreatedAt: timestamppb.New(e.CreatedAt),
	}
}

// OptionalEnvelope converts an envelope that may be absent.
func OptionalEnvelope(e *domain.Envelope) *doelabv1.Envelope {
	if e == nil {
		return nil
	}
	return Envelope(*e)
}

// EnvelopeFromProto converts the client-set fields of an envelope message:
// the site, the interval, the limits and the bindings. An unspecified binding
// is "none". The source and the run are the server's to set.
func EnvelopeFromProto(p *doelabv1.Envelope) domain.Envelope {
	binding := func(b doelabv1.BindingConstraint) domain.BindingConstraint {
		if d := BindingConstraintFromProto(b); d != "" {
			return d
		}
		return domain.BindingNone
	}
	return domain.Envelope{
		SiteID:    parseID(p.GetSiteId()),
		ValidFrom: p.GetValidFrom().AsTime(), ValidTo: p.GetValidTo().AsTime(),
		ExportLimitW: p.GetExportLimitW(), ImportLimitW: p.GetImportLimitW(),
		ExportBinding: binding(p.GetExportBinding()), ExportBindingElement: p.GetExportBindingElement(),
		ImportBinding: binding(p.GetImportBinding()), ImportBindingElement: p.GetImportBindingElement(),
	}
}

// The functions below read whole resources back from their messages. The
// engine and the simulator use them: they are clients of the API, and rebuild
// domain values from what it returns.

// optionalParsedID reads a nullable uuid field.
func optionalParsedID(s *string) *uuid.UUID {
	if s == nil {
		return nil
	}
	id := parseID(*s)
	return &id
}

// FeederFromMessage reads a whole feeder.
func FeederFromMessage(p *doelabv1.Feeder) domain.Feeder {
	return domain.Feeder{
		ID: parseID(p.GetId()), Code: p.GetCode(), Name: p.GetName(),
		NominalVoltageV: p.GetNominalVoltageV(), TransformerKVA: p.GetTransformerKva(),
		SourceVoltageV: p.GetSourceVoltageV(), SourceAngleDeg: p.GetSourceAngleDeg(),
		SourceROhm: p.GetSourceROhm(), SourceXOhm: p.GetSourceXOhm(), TapPU: p.GetTapPu(),
		Timezone: p.GetTimezone(), Attribution: p.GetAttribution(),
		CreatedAt: p.GetCreatedAt().AsTime(), UpdatedAt: p.GetUpdatedAt().AsTime(),
		SubstationID: optionalParsedID(p.SubstationId),
	}
}

// FeederNodeFromMessage reads a whole feeder node.
func FeederNodeFromMessage(p *doelabv1.FeederNode) domain.FeederNode {
	return domain.FeederNode{
		ID: parseID(p.GetId()), FeederID: parseID(p.GetFeederId()), Name: p.GetName(),
		ParentNodeID: optionalParsedID(p.ParentNodeId),
		GroundROhm:   p.GroundROhm, GroundXOhm: p.GroundXOhm,
		CreatedAt: p.GetCreatedAt().AsTime(), UpdatedAt: p.GetUpdatedAt().AsTime(),
	}
}

// FeederLineFromMessage reads a whole feeder line.
func FeederLineFromMessage(p *doelabv1.FeederLine) domain.FeederLine {
	out := domain.FeederLine{
		ID: parseID(p.GetId()), FeederID: parseID(p.GetFeederId()), Name: p.GetName(),
		FromNodeID: parseID(p.GetFromNodeId()), ToNodeID: parseID(p.GetToNodeId()),
		Linecode: p.GetLinecode(), LengthM: p.GetLengthM(), IsSwitch: p.GetIsSwitch(),
		ROhm: p.GetROhm(), XOhm: p.GetXOhm(), BS: p.GetBS(),
		AmpacityA: p.AmpacityA,
		CreatedAt: p.GetCreatedAt().AsTime(), UpdatedAt: p.GetUpdatedAt().AsTime(),
	}
	if p.AmpacitySource != nil {
		source := ampacitySources.fromProto[p.GetAmpacitySource()]
		out.AmpacitySource = &source
	}
	return out
}

// SiteFromMessage reads a whole site.
func SiteFromMessage(p *doelabv1.Site) domain.Site {
	site := SiteFromProto(p)
	site.ID = parseID(p.GetId())
	site.CreatedAt, site.UpdatedAt = p.GetCreatedAt().AsTime(), p.GetUpdatedAt().AsTime()
	return site
}

// DeviceFromMessage reads a whole device.
func DeviceFromMessage(p *doelabv1.Device) domain.Device {
	device := DeviceFromProto(p)
	device.ID = parseID(p.GetId())
	device.CreatedAt, device.UpdatedAt = p.GetCreatedAt().AsTime(), p.GetUpdatedAt().AsTime()
	return device
}

// EnvelopeConfigFromMessage reads a whole config version.
func EnvelopeConfigFromMessage(p *doelabv1.EnvelopeConfig) domain.EnvelopeConfig {
	config := EnvelopeConfigFromProto(p)
	config.ID, config.Version = parseID(p.GetId()), p.GetVersion()
	config.CreatedBy, config.CreatedAt = p.GetCreatedBy(), p.GetCreatedAt().AsTime()
	return config
}

// EnvelopeFromMessage reads a whole envelope.
func EnvelopeFromMessage(p *doelabv1.Envelope) domain.Envelope {
	e := EnvelopeFromProto(p)
	e.ID = parseID(p.GetId())
	e.Source = envelopeSources.fromProto[p.GetSource()]
	e.EnvelopeRunID, e.BackstopEventID = optionalParsedID(p.EnvelopeRunId), optionalParsedID(p.BackstopEventId)
	e.CreatedAt = p.GetCreatedAt().AsTime()
	if p.GetSupersededAt() != nil {
		at := p.GetSupersededAt().AsTime()
		e.SupersededAt = &at
	}
	return e
}

// EnvelopeRunInterval converts a run interval to its message.
func EnvelopeRunInterval(i domain.EnvelopeRunInterval) *doelabv1.EnvelopeRunInterval {
	return &doelabv1.EnvelopeRunInterval{
		EnvelopeRunId: i.EnvelopeRunID.String(), FeederId: i.FeederID.String(),
		ValidFrom: timestamppb.New(i.ValidFrom), ValidTo: timestamppb.New(i.ValidTo),
		ForecastNetLoadW: i.ForecastNetLoadW, ForecastLoadingPct: i.ForecastLoadingPct,
		ForecastVMinPu: i.ForecastVMinPU, ForecastVMaxPu: i.ForecastVMaxPU,
		ExportLimitTotalW: i.ExportLimitTotalW, ImportLimitTotalW: i.ImportLimitTotalW,
		StaticLimitTotalW: i.StaticLimitTotalW, StaticVMaxPu: i.StaticVMaxPU,
		StaticBinding: BindingConstraintToProto(i.StaticBinding), StaticBindingElement: i.StaticBindingElement,
		CreatedAt: timestamppb.New(i.CreatedAt), EnvelopeVMaxPu: i.EnvelopeVMaxPU,
	}
}

// EnvelopeRunIntervalFromProto converts the client-set fields of a run
// interval message. The run and the feeder are the server's to set.
func EnvelopeRunIntervalFromProto(p *doelabv1.EnvelopeRunInterval) domain.EnvelopeRunInterval {
	return domain.EnvelopeRunInterval{
		ValidFrom: p.GetValidFrom().AsTime(), ValidTo: p.GetValidTo().AsTime(),
		ForecastNetLoadW: p.GetForecastNetLoadW(), ForecastLoadingPct: p.GetForecastLoadingPct(),
		ForecastVMinPU: p.GetForecastVMinPu(), ForecastVMaxPU: p.GetForecastVMaxPu(),
		ExportLimitTotalW: p.GetExportLimitTotalW(), ImportLimitTotalW: p.GetImportLimitTotalW(),
		StaticLimitTotalW: p.GetStaticLimitTotalW(), StaticVMaxPU: p.GetStaticVMaxPu(),
		StaticBinding: BindingConstraintFromProto(p.GetStaticBinding()), StaticBindingElement: p.GetStaticBindingElement(),
		EnvelopeVMaxPU: p.EnvelopeVMaxPu,
	}
}

// FeederNodeState converts a node state to its message.
func FeederNodeState(s domain.FeederNodeState) *doelabv1.FeederNodeState {
	return &doelabv1.FeederNodeState{
		FeederId: s.FeederID.String(), NodeId: s.NodeID.String(),
		ValidFrom: timestamppb.New(s.ValidFrom), ValidTo: timestamppb.New(s.ValidTo),
		EnvelopeRunId: s.EnvelopeRunID.String(),
		ForecastVPu:   s.ForecastVPU, EnvelopeVPu: s.EnvelopeVPU, StaticVPu: s.StaticVPU,
	}
}

// FeederNodeStateFromProto converts the client-set fields of a node state
// message. The feeder and the run are the server's to set.
func FeederNodeStateFromProto(p *doelabv1.FeederNodeState) domain.FeederNodeState {
	return domain.FeederNodeState{
		NodeID: parseID(p.GetNodeId()), ValidFrom: p.GetValidFrom().AsTime(), ValidTo: p.GetValidTo().AsTime(),
		ForecastVPU: p.GetForecastVPu(), EnvelopeVPU: p.GetEnvelopeVPu(), StaticVPU: p.GetStaticVPu(),
	}
}

// FeederLineState converts a line state to its message.
func FeederLineState(s domain.FeederLineState) *doelabv1.FeederLineState {
	return &doelabv1.FeederLineState{
		FeederId: s.FeederID.String(), LineId: s.LineID.String(),
		ValidFrom: timestamppb.New(s.ValidFrom), ValidTo: timestamppb.New(s.ValidTo),
		EnvelopeRunId:    s.EnvelopeRunID.String(),
		ForecastCurrentA: s.ForecastCurrentA, EnvelopeCurrentA: s.EnvelopeCurrentA, StaticCurrentA: s.StaticCurrentA,
		ForecastPowerW: s.ForecastPowerW, EnvelopePowerW: s.EnvelopePowerW, StaticPowerW: s.StaticPowerW,
	}
}

// FeederLineStateFromProto converts the client-set fields of a line state
// message. The feeder and the run are the server's to set.
func FeederLineStateFromProto(p *doelabv1.FeederLineState) domain.FeederLineState {
	return domain.FeederLineState{
		LineID: parseID(p.GetLineId()), ValidFrom: p.GetValidFrom().AsTime(), ValidTo: p.GetValidTo().AsTime(),
		ForecastCurrentA: p.GetForecastCurrentA(), EnvelopeCurrentA: p.GetEnvelopeCurrentA(), StaticCurrentA: p.GetStaticCurrentA(),
		ForecastPowerW: p.GetForecastPowerW(), EnvelopePowerW: p.GetEnvelopePowerW(), StaticPowerW: p.GetStaticPowerW(),
	}
}

// Reading converts a reading to its message.
func Reading(r domain.Reading) *doelabv1.Reading {
	return &doelabv1.Reading{
		DeviceId: r.DeviceID.String(), SiteId: r.SiteID.String(), Ts: timestamppb.New(r.TS),
		PowerW: r.PowerW, NetExportW: r.NetExportW, SocPct: r.SOCPct, VoltageV: r.VoltageV,
		ReceivedAt: timestamppb.New(r.ReceivedAt),
	}
}

// ReadingFromProto converts the client-set fields of a reading message. The
// site and the time it was received are the server's to set.
func ReadingFromProto(p *doelabv1.Reading) domain.Reading {
	return domain.Reading{
		DeviceID: parseID(p.GetDeviceId()), TS: p.GetTs().AsTime(),
		PowerW: p.GetPowerW(), NetExportW: p.GetNetExportW(), SOCPct: p.SocPct, VoltageV: p.VoltageV,
	}
}

// Alert converts an alert to its message.
func Alert(a domain.Alert) *doelabv1.Alert {
	return &doelabv1.Alert{
		Id: a.ID.String(), SiteId: a.SiteID.String(), FeederId: a.FeederID.String(), DeviceId: optionalID(a.DeviceID),
		Kind: AlertKindToProto(a.Kind), Severity: AlertSeverityToProto(a.Severity),
		OpenedAt: timestamppb.New(a.OpenedAt), ResolvedAt: optionalTime(a.ResolvedAt),
		AcknowledgedAt: optionalTime(a.AcknowledgedAt), AcknowledgedBy: a.AcknowledgedBy,
		LimitW: a.LimitW, PeakW: a.PeakW, Detail: a.Detail,
		CreatedAt: timestamppb.New(a.CreatedAt), UpdatedAt: timestamppb.New(a.UpdatedAt),
	}
}

// BackstopEvent converts a backstop to its message.
func BackstopEvent(e domain.BackstopEvent) *doelabv1.BackstopEvent {
	return &doelabv1.BackstopEvent{
		Id: e.ID.String(), FeederId: e.FeederID.String(), Reason: e.Reason, ExportLimitW: e.ExportLimitW,
		TriggeredBy: e.TriggeredBy, TriggeredAt: timestamppb.New(e.TriggeredAt),
		ClearedBy: e.ClearedBy, ClearedAt: optionalTime(e.ClearedAt),
		CreatedAt: timestamppb.New(e.CreatedAt), UpdatedAt: timestamppb.New(e.UpdatedAt),
	}
}

// BackstopEventFromProto converts the client-set fields of a backstop
// message: the feeder, the reason and the limit.
func BackstopEventFromProto(p *doelabv1.BackstopEvent) domain.BackstopEvent {
	return domain.BackstopEvent{FeederID: parseID(p.GetFeederId()), Reason: p.GetReason(), ExportLimitW: p.GetExportLimitW()}
}

// IDs renders a list of ids.
func IDs(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

// ParseIDs reads a list of ids that validation has accepted.
func ParseIDs(ids []string) []uuid.UUID {
	out := make([]uuid.UUID, len(ids))
	for i, id := range ids {
		out[i] = parseID(id)
	}
	return out
}
