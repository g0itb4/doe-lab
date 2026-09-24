package protomap

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/internal/domain"
)

func ptr[T any](v T) *T { return &v }

var (
	created = time.Date(2026, 10, 1, 1, 2, 3, 0, time.UTC)
	updated = created.Add(time.Hour)
)

// A resource that goes to its message and back is the same resource. Each
// pair of functions is one field after another, and this is what catches a
// field left out of one direction.
func TestRoundTrips(t *testing.T) {
	t.Parallel()

	feeder := domain.Feeder{
		ID: uuid.New(), Code: "LV10", Name: "Elm Street", NominalVoltageV: 230, TransformerKVA: 500,
		SourceVoltageV: 250, SourceAngleDeg: -30, SourceROhm: 0.003, SourceXOhm: 0.0001, TapPU: 0.975,
		Timezone: "Australia/Sydney", Attribution: "CSIRO", CreatedAt: created, UpdatedAt: updated,
	}
	if got := FeederFromMessage(Feeder(feeder)); got != feeder {
		t.Errorf("feeder:\n got %+v\nwant %+v", got, feeder)
	}

	root := domain.FeederNode{ID: uuid.New(), FeederID: feeder.ID, Name: "root", CreatedAt: created, UpdatedAt: updated}
	node := domain.FeederNode{
		ID: uuid.New(), FeederID: feeder.ID, Name: "B12", ParentNodeID: &root.ID,
		GroundROhm: ptr(10.0), GroundXOhm: ptr(0.0), CreatedAt: created, UpdatedAt: updated,
	}
	for _, n := range []domain.FeederNode{root, node} {
		if got := FeederNodeFromMessage(FeederNode(n)); !reflect.DeepEqual(got, n) {
			t.Errorf("node:\n got %+v\nwant %+v", got, n)
		}
	}

	matrix := make([]float64, 16)
	matrix[0], matrix[5] = 0.1, 0.2
	rated := domain.FeederLine{
		ID: uuid.New(), FeederID: feeder.ID, Name: "L_2", FromNodeID: root.ID, ToNodeID: node.ID,
		Linecode: "ugsc_16cu", LengthM: 17.6, ROhm: matrix, XOhm: matrix, BS: matrix,
		AmpacityA: ptr(90.0), AmpacitySource: ptr(domain.AmpacityOperator), CreatedAt: created, UpdatedAt: updated,
	}
	unrated := rated
	unrated.IsSwitch, unrated.AmpacityA, unrated.AmpacitySource = true, nil, nil
	for _, l := range []domain.FeederLine{rated, unrated} {
		if got := FeederLineFromMessage(FeederLine(l)); !reflect.DeepEqual(got, l) {
			t.Errorf("line:\n got %+v\nwant %+v", got, l)
		}
	}

	site := domain.Site{
		ID: uuid.New(), NMI: "XDLAB000014", FeederID: feeder.ID, NodeID: node.ID, Name: "Ld7_LOAD_A", Phase: 2,
		PVKW: 6.6, InverterKVA: 5, ExportCapW: 5000, ImportCapW: 14000,
		HasBattery: true, BatteryKWh: ptr(13.5), HasEV: true, ProfileCustomer: ptr(int32(42)),
		CreatedAt: created, UpdatedAt: updated,
	}
	if got := SiteFromMessage(Site(site)); !reflect.DeepEqual(got, site) {
		t.Errorf("site:\n got %+v\nwant %+v", got, site)
	}
	// deleted_at does not cross the wire.
	deleted := site
	deleted.DeletedAt = &updated
	if got := SiteFromMessage(Site(deleted)); got.DeletedAt != nil {
		t.Errorf("deleted_at crossed the wire: %v", got.DeletedAt)
	}

	device := domain.Device{ID: uuid.New(), SiteID: site.ID, DERType: domain.DERBattery, RatedW: 5000, CreatedAt: created, UpdatedAt: updated}
	if got := DeviceFromMessage(Device(device)); got != device {
		t.Errorf("device:\n got %+v\nwant %+v", got, device)
	}

	config := domain.EnvelopeConfig{
		ID: uuid.New(), FeederID: feeder.ID, Version: 3, Policy: domain.PolicyProportional,
		VMinPU: 0.94, VMaxPU: 1.1, TransformerLimitPct: 100, LineLimitPct: 90, PVScale: 3, StaticLimitW: 5000,
		IntervalMinutes: 30, HorizonIntervals: 48, BreachGraceSeconds: 60, OfflineAfterSeconds: 300,
		Note: "tighter", CreatedBy: "operator", CreatedAt: created,
	}
	if got := EnvelopeConfigFromMessage(EnvelopeConfig(config)); got != config {
		t.Errorf("config:\n got %+v\nwant %+v", got, config)
	}

	runID, eventID := uuid.New(), uuid.New()
	engine := domain.Envelope{
		ID: uuid.New(), SiteID: site.ID, ValidFrom: created, ValidTo: created.Add(30 * time.Minute),
		ExportLimitW: 1500, ImportLimitW: 7000, Source: domain.SourceEngine, EnvelopeRunID: &runID,
		ExportBinding: domain.BindingVoltageHigh, ExportBindingElement: "XDLAB000014",
		ImportBinding: domain.BindingLine, ImportBindingElement: "L_2", CreatedAt: created,
	}
	backstop := engine
	backstop.Source, backstop.EnvelopeRunID, backstop.BackstopEventID, backstop.SupersededAt = domain.SourceBackstop, nil, &eventID, &updated
	backstop.ExportBinding, backstop.ExportBindingElement = domain.BindingNone, ""
	for _, e := range []domain.Envelope{engine, backstop} {
		if got := EnvelopeFromMessage(Envelope(e)); !reflect.DeepEqual(got, e) {
			t.Errorf("envelope:\n got %+v\nwant %+v", got, e)
		}
	}
	if OptionalEnvelope(nil) != nil || OptionalEnvelope(&engine).GetExportLimitW() != 1500 {
		t.Error("OptionalEnvelope")
	}

	failed := domain.EnvelopeRun{
		ID: runID, FeederID: feeder.ID, EnvelopeConfigID: config.ID, Status: domain.RunFailed, IdempotencyKey: "run-0001",
		HorizonFrom: created, HorizonTo: created.Add(24 * time.Hour), StartedAt: created, CompletedAt: &updated,
		DurationMS: ptr(int32(241)), SiteCount: 56, IntervalCount: 48, EnvelopeCount: 2688,
		EngineVersion: "1.0", Error: ptr("no convergence"), CreatedAt: created, UpdatedAt: updated,
	}
	msg := EnvelopeRun(failed)
	if msg.GetStatus() != doelabv1.RunStatus_RUN_STATUS_FAILED || msg.GetError() != "no convergence" || msg.GetDurationMs() != 241 ||
		!msg.GetCompletedAt().AsTime().Equal(updated) || msg.GetEnvelopeCount() != 2688 {
		t.Errorf("run message = %v", msg)
	}
	running := failed
	running.Status, running.CompletedAt, running.DurationMS, running.Error = domain.RunRunning, nil, nil, nil
	if msg := EnvelopeRun(running); msg.CompletedAt != nil || msg.DurationMs != nil || msg.Error != nil {
		t.Errorf("a running run has finish fields: %v", msg)
	}
	// Only the client's fields are read back from a run message.
	in := EnvelopeRunFromProto(msg)
	if in.FeederID != feeder.ID || in.EnvelopeConfigID != config.ID || in.IdempotencyKey != "run-0001" || !in.HorizonFrom.Equal(created) ||
		in.EngineVersion != "1.0" || in.Status != "" || in.ID != uuid.Nil {
		t.Errorf("run from proto = %+v", in)
	}
}

// What a client may set is narrower than the resource: the server's fields
// are dropped on the way in.
func TestClientSetFieldsOnly(t *testing.T) {
	t.Parallel()

	e := EnvelopeFromProto(&doelabv1.Envelope{
		Id: uuid.NewString(), SiteId: uuid.NewString(), ExportLimitW: 1, ImportLimitW: 2,
		Source: doelabv1.EnvelopeSource_ENVELOPE_SOURCE_BACKSTOP, EnvelopeRunId: ptr(uuid.NewString()),
		// Bindings left unspecified are "none".
	})
	if e.ID != uuid.Nil || e.Source != "" || e.EnvelopeRunID != nil || e.ExportBinding != domain.BindingNone || e.ImportBinding != domain.BindingNone {
		t.Errorf("envelope from proto = %+v", e)
	}

	c := EnvelopeConfigFromProto(&doelabv1.EnvelopeConfig{Id: uuid.NewString(), Version: 9, CreatedBy: "me", VMinPu: 0.94})
	if c.ID != uuid.Nil || c.Version != 0 || c.CreatedBy != "" || c.VMinPU != 0.94 {
		t.Errorf("config from proto = %+v", c)
	}
	s := SiteFromProto(&doelabv1.Site{Id: uuid.NewString(), Nmi: "XDLAB000014"})
	if s.ID != uuid.Nil || s.NMI != "XDLAB000014" {
		t.Errorf("site from proto = %+v", s)
	}
}

func TestSlice(t *testing.T) {
	t.Parallel()
	got := Slice([]domain.SiteProfile{{LoadW: 1}, {LoadW: 2}}, SiteProfile)
	if len(got) != 2 || got[1].GetLoadW() != 2 {
		t.Errorf("Slice = %v", got)
	}
	if got := Slice([]domain.SiteProfile(nil), SiteProfile); len(got) != 0 {
		t.Errorf("Slice of nil = %v", got)
	}
}

// The resources that only the server writes go one way, a field at a time.
func TestOperationsResources(t *testing.T) {
	t.Parallel()
	site, device, run, feeder := uuid.New(), uuid.New(), uuid.New(), uuid.New()

	reading := domain.Reading{
		DeviceID: device, SiteID: site, TS: created, PowerW: 3000, NetExportW: 2500,
		SOCPct: ptr(55.0), VoltageV: ptr(241.5), ReceivedAt: updated,
	}
	r := Reading(reading)
	if r.GetDeviceId() != device.String() || r.GetSiteId() != site.String() || !r.GetTs().AsTime().Equal(created) || r.GetPowerW() != 3000 ||
		r.GetNetExportW() != 2500 || r.GetSocPct() != 55 || r.GetVoltageV() != 241.5 || !r.GetReceivedAt().AsTime().Equal(updated) {
		t.Errorf("reading = %v", r)
	}
	// On the way in, the site and the time of receipt are the server's.
	in := ReadingFromProto(r)
	want := reading
	want.SiteID, want.ReceivedAt = uuid.Nil, time.Time{}
	if !reflect.DeepEqual(in, want) {
		t.Errorf("reading from proto:\n got %+v\nwant %+v", in, want)
	}

	open := domain.Alert{
		ID: uuid.New(), SiteID: site, FeederID: feeder, Kind: domain.AlertConstraintBreach, Severity: domain.SeverityCritical,
		OpenedAt: created, LimitW: ptr(1000.0), PeakW: ptr(2500.0), Detail: "over", CreatedAt: created, UpdatedAt: updated,
	}
	a := Alert(open)
	if a.GetId() != open.ID.String() || a.GetSiteId() != site.String() || a.GetFeederId() != feeder.String() || a.DeviceId != nil ||
		a.GetKind() != doelabv1.AlertKind_ALERT_KIND_CONSTRAINT_BREACH || a.GetSeverity() != doelabv1.AlertSeverity_ALERT_SEVERITY_CRITICAL ||
		!a.GetOpenedAt().AsTime().Equal(created) || a.ResolvedAt != nil || a.AcknowledgedAt != nil || a.AcknowledgedBy != nil ||
		a.GetLimitW() != 1000 || a.GetPeakW() != 2500 || a.GetDetail() != "over" ||
		!a.GetCreatedAt().AsTime().Equal(created) || !a.GetUpdatedAt().AsTime().Equal(updated) {
		t.Errorf("open alert = %v", a)
	}
	closed := domain.Alert{
		ID: uuid.New(), SiteID: site, DeviceID: &device, Kind: domain.AlertDeviceOffline, Severity: domain.SeverityInfo,
		OpenedAt: created, ResolvedAt: &updated, AcknowledgedAt: &updated, AcknowledgedBy: ptr("operator"),
	}
	a = Alert(closed)
	if a.GetDeviceId() != device.String() || !a.GetResolvedAt().AsTime().Equal(updated) || !a.GetAcknowledgedAt().AsTime().Equal(updated) ||
		a.GetAcknowledgedBy() != "operator" || a.LimitW != nil || a.PeakW != nil {
		t.Errorf("closed alert = %v", a)
	}

	event := domain.BackstopEvent{
		ID: uuid.New(), FeederID: feeder, Reason: "fault", ExportLimitW: 500, TriggeredBy: "operator", TriggeredAt: created,
		ClearedBy: ptr("operator"), ClearedAt: &updated, CreatedAt: created, UpdatedAt: updated,
	}
	e := BackstopEvent(event)
	if e.GetId() != event.ID.String() || e.GetFeederId() != feeder.String() || e.GetReason() != "fault" || e.GetExportLimitW() != 500 ||
		e.GetTriggeredBy() != "operator" || !e.GetTriggeredAt().AsTime().Equal(created) || e.GetClearedBy() != "operator" ||
		!e.GetClearedAt().AsTime().Equal(updated) || !e.GetCreatedAt().AsTime().Equal(created) || !e.GetUpdatedAt().AsTime().Equal(updated) {
		t.Errorf("backstop = %v", e)
	}
	// On the way in, a client sets the feeder, the reason and the limit.
	if got := BackstopEventFromProto(e); got != (domain.BackstopEvent{FeederID: feeder, Reason: "fault", ExportLimitW: 500}) {
		t.Errorf("backstop from proto = %+v", got)
	}

	interval := domain.EnvelopeRunInterval{
		EnvelopeRunID: run, FeederID: feeder, ValidFrom: created, ValidTo: updated,
		ForecastNetLoadW: -12000, ForecastLoadingPct: 12.5, ForecastVMinPU: 1.02, ForecastVMaxPU: 1.06,
		ExportLimitTotalW: 150000, ImportLimitTotalW: 300000, StaticLimitTotalW: 280000, StaticVMaxPU: 1.13,
		StaticBinding: domain.BindingVoltageHigh, StaticBindingElement: "XDLAB000014", CreatedAt: created,
	}
	i := EnvelopeRunInterval(interval)
	if i.GetEnvelopeRunId() != run.String() || i.GetFeederId() != feeder.String() || !i.GetCreatedAt().AsTime().Equal(created) {
		t.Errorf("interval = %v", i)
	}
	// On the way in, the run, the feeder and the time of creation are the
	// server's; the rest survives the round trip.
	wantInterval := interval
	wantInterval.EnvelopeRunID, wantInterval.FeederID, wantInterval.CreatedAt = uuid.Nil, uuid.Nil, time.Time{}
	if got := EnvelopeRunIntervalFromProto(i); got != wantInterval {
		t.Errorf("interval from proto:\n got %+v\nwant %+v", got, wantInterval)
	}

	ids := []uuid.UUID{site, device}
	if got := ParseIDs(IDs(ids)); !reflect.DeepEqual(got, ids) {
		t.Errorf("ids = %v, want %v", got, ids)
	}
	if got := IDs(nil); len(got) != 0 {
		t.Errorf("no ids = %v", got)
	}
}
