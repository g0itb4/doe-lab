package server_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/gen/doelab/v1/doelabv1connect"
	"doelab/api/internal/domain"
	"doelab/api/internal/profile"
	"doelab/api/internal/repo/repotest"
)

func (a *api) telemetry(token string) doelabv1connect.TelemetryServiceClient {
	return doelabv1connect.NewTelemetryServiceClient(a.HTTP, a.URL, as(token))
}

func (a *api) alerts(token string) doelabv1connect.AlertServiceClient {
	return doelabv1connect.NewAlertServiceClient(a.HTTP, a.URL, as(token))
}

func (a *api) backstops(token string) doelabv1connect.BackstopServiceClient {
	return doelabv1connect.NewBackstopServiceClient(a.HTTP, a.URL, as(token))
}

// device gives a site a device.
func (a *api) device(t *testing.T, site domain.Site, kind domain.DERType) string {
	t.Helper()
	d, err := a.Store.CreateDevice(repotest.Ctx(), domain.Device{SiteID: site.ID, DERType: kind, RatedW: 5000})
	noErr(t, "device", err)
	return d.ID.String()
}

// reading is the message for a reading of a device, seconds into the profile
// day.
func reading(deviceID string, seconds int, netExportW float64) *doelabv1.Reading {
	volts := 241.5
	return &doelabv1.Reading{
		DeviceId: deviceID, Ts: timestamppb.New(repotest.Day.Add(time.Duration(seconds) * time.Second)),
		PowerW: netExportW + 500, NetExportW: netExportW, VoltageV: &volts,
	}
}

// ingest opens a stream for a site, sends the batches and closes it.
func (a *api) ingest(token, nmi string, batches ...[]*doelabv1.Reading) (*doelabv1.IngestReadingsResponse, error) {
	stream := a.telemetry(token).IngestReadings(ctx)
	for _, batch := range batches {
		if err := stream.Send(&doelabv1.IngestReadingsRequest{Nmi: nmi, Readings: batch}); err != nil {
			break // the server has answered already; the close says what
		}
	}
	res, err := stream.CloseAndReceive()
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func day(seconds int) *timestamppb.Timestamp {
	return timestamppb.New(repotest.Day.Add(time.Duration(seconds) * time.Second))
}

func TestIngestReadings(t *testing.T) {
	t.Parallel()
	p := prepare(t)
	a, b := p.fixture.SiteA, p.fixture.SiteB
	solar, ev := p.device(t, a, domain.DERSolar), p.device(t, b, domain.DEREV)
	token := p.Tokens.DeviceToken(a.NMI)

	// Two batches on one stream; the second repeats a reading of the first.
	res, err := p.ingest(token, a.NMI,
		[]*doelabv1.Reading{reading(solar, 0, 100), reading(solar, 5, 200), reading(solar, 10, 300)},
		[]*doelabv1.Reading{reading(solar, 10, 300), reading(solar, 15, 400)})
	noErr(t, "ingest", err)
	if res.GetAccepted() != 4 || res.GetDuplicates() != 1 {
		t.Errorf("ingest = %v, want 4 accepted and 1 duplicate", res)
	}

	list := func(deviceID string, size int32, token string) (*doelabv1.ListReadingsResponse, error) {
		res, err := p.telemetry("").ListReadings(ctx, req(&doelabv1.ListReadingsRequest{
			DeviceId: deviceID, From: day(0), To: day(3600), PageSize: size, PageToken: token,
		}))
		if err != nil {
			return nil, err
		}
		return res.Msg, nil
	}
	all, err := list(solar, 0, "")
	noErr(t, "list", err)
	if len(all.GetReadings()) != 4 || all.GetNextPageToken() != "" {
		t.Fatalf("%d readings, token %q", len(all.GetReadings()), all.GetNextPageToken())
	}
	first := all.GetReadings()[0]
	if first.GetSiteId() != a.ID.String() || first.GetDeviceId() != solar || first.GetPowerW() != 600 || first.GetNetExportW() != 100 ||
		first.GetVoltageV() != 241.5 || first.SocPct != nil || first.GetReceivedAt() == nil || !first.GetTs().AsTime().Equal(repotest.Day) {
		t.Errorf("the first reading = %v", first)
	}
	page, err := list(solar, 3, "")
	noErr(t, "first page", err)
	rest, err := list(solar, 3, page.GetNextPageToken())
	noErr(t, "second page", err)
	if len(page.GetReadings()) != 3 || len(rest.GetReadings()) != 1 || rest.GetReadings()[0].GetNetExportW() != 400 {
		t.Errorf("pages of %d and %d", len(page.GetReadings()), len(rest.GetReadings()))
	}

	// A stream is for one site, and needs that site's token.
	stream := p.telemetry(token).IngestReadings(ctx)
	noErr(t, "send", stream.Send(&doelabv1.IngestReadingsRequest{Nmi: a.NMI, Readings: []*doelabv1.Reading{reading(solar, 20, 1)}}))
	_ = stream.Send(&doelabv1.IngestReadingsRequest{Nmi: b.NMI, Readings: []*doelabv1.Reading{reading(ev, 20, 1)}})
	_, err = stream.CloseAndReceive()
	wantCode(t, "a stream that changes site", err, connect.CodeInvalidArgument)

	_, err = p.ingest(operatorToken, a.NMI, []*doelabv1.Reading{reading(solar, 30, 1)})
	wantCode(t, "the operator's token", err, connect.CodePermissionDenied)
	_, err = p.ingest("", a.NMI, []*doelabv1.Reading{reading(solar, 30, 1)})
	wantCode(t, "no token", err, connect.CodeUnauthenticated)
	_, err = p.ingest(p.Tokens.DeviceToken(b.NMI), a.NMI, []*doelabv1.Reading{reading(solar, 30, 1)})
	wantCode(t, "another site's token", err, connect.CodePermissionDenied)
	_, err = p.ingest(token, a.NMI, []*doelabv1.Reading{reading(ev, 30, 1)})
	wantCode(t, "a reading of another site's device", err, connect.CodePermissionDenied)
	_, err = p.ingest(token, a.NMI, nil)
	wantViolation(t, "an empty batch", err, "readings")
	_, err = p.ingest(token, "nmi", []*doelabv1.Reading{reading(solar, 30, 1)})
	wantViolation(t, "a malformed NMI", err, "nmi")
	soc := 101.0
	full := reading(solar, 30, 1)
	full.SocPct = &soc
	_, err = p.ingest(token, a.NMI, []*doelabv1.Reading{full})
	wantViolation(t, "a state of charge above 100", err, "soc_pct")
	// A stream with nothing on it stores nothing.
	res, err = p.ingest(token, a.NMI)
	if err != nil || res.GetAccepted() != 0 {
		t.Errorf("an empty stream: %v, %v", res, err)
	}

	_, err = list(unknownID, 0, "")
	wantCode(t, "readings of an unknown device", err, connect.CodeNotFound)
	_, err = p.telemetry("").ListReadings(ctx, req(&doelabv1.ListReadingsRequest{DeviceId: solar, From: day(60), To: day(0)}))
	wantViolation(t, "an inverted range", err, "to must be after from")
}

func TestFleetSummaryAndWatch(t *testing.T) {
	t.Parallel()
	p := prepare(t)
	a := p.fixture.SiteA
	feederID := p.fixture.Feeder.ID.String()
	solar := p.device(t, a, domain.DERSolar)
	p.device(t, p.fixture.SiteB, domain.DEREV)

	res, err := p.telemetry("").GetFleetSummary(ctx, req(&doelabv1.GetFleetSummaryRequest{FeederId: feederID}))
	noErr(t, "summary", err)
	s := res.Msg.GetSummary()
	if s.GetFeederId() != feederID || !s.GetAt().AsTime().Equal(repotest.Day) || s.GetEnrolledSites() != 1 || s.GetDevices() != 2 ||
		s.GetDevicesOnline() != 0 || s.BackstopEventId != nil || s.GetLatestRunId() != p.runID ||
		s.GetLatestRunStatus() != doelabv1.RunStatus_RUN_STATUS_RUNNING || s.GetLatestRunAt() == nil {
		t.Errorf("the summary = %v", s)
	}

	// A watcher sees the fleet change without asking again.
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := p.telemetry("").WatchFleet(watchCtx, req(&doelabv1.WatchFleetRequest{FeederId: feederID}))
	noErr(t, "watch", err)
	if !stream.Receive() || stream.Msg().GetSummary().GetReportingSites() != 0 {
		t.Fatalf("the first summary = %v, %v", stream.Msg(), stream.Err())
	}
	_, err = p.publish(engineToken, p.runID, "batch-0001", envelope(a.ID.String(), 0, 1000))
	noErr(t, "publish", err)
	_, err = p.ingest(p.Tokens.DeviceToken(a.NMI), a.NMI, []*doelabv1.Reading{reading(solar, 0, 2500)})
	noErr(t, "ingest", err)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if !stream.Receive() {
			t.Fatalf("the watch ended: %v", stream.Err())
		}
		got := stream.Msg().GetSummary()
		if got.GetReportingSites() == 1 {
			if got.GetExportW() != 2500 || got.GetExportLimitW() != 1000 || got.GetSitesOverLimit() != 1 || got.GetDevicesOnline() != 1 {
				t.Errorf("the summary after a reading = %v", got)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the watch never showed the reading")
		}
	}
	cancel()
	for stream.Receive() {
	}
	if err := stream.Err(); connect.CodeOf(err) != connect.CodeCanceled {
		t.Errorf("the watch ended with %v", err)
	}

	// Under a backstop the summary names it.
	created, err := p.backstops(operatorToken).CreateBackstopEvent(ctx, req(&doelabv1.CreateBackstopEventRequest{
		BackstopEvent: &doelabv1.BackstopEvent{FeederId: feederID, Reason: "test"},
	}))
	noErr(t, "backstop", err)
	res, err = p.telemetry("").GetFleetSummary(ctx, req(&doelabv1.GetFleetSummaryRequest{FeederId: feederID}))
	noErr(t, "summary under a backstop", err)
	if res.Msg.GetSummary().GetBackstopEventId() != created.Msg.GetBackstopEvent().GetId() || res.Msg.GetSummary().GetExportLimitW() != 0 {
		t.Errorf("the summary under a backstop = %v", res.Msg.GetSummary())
	}

	_, err = p.telemetry("").GetFleetSummary(ctx, req(&doelabv1.GetFleetSummaryRequest{FeederId: unknownID}))
	wantCode(t, "an unknown feeder", err, connect.CodeNotFound)
	unknown, err := p.telemetry("").WatchFleet(ctx, req(&doelabv1.WatchFleetRequest{FeederId: unknownID}))
	noErr(t, "watch an unknown feeder", err)
	if unknown.Receive() {
		t.Errorf("a summary of an unknown feeder: %v", unknown.Msg())
	}
	wantCode(t, "watching an unknown feeder", unknown.Err(), connect.CodeNotFound)
}

func TestFleetState(t *testing.T) {
	t.Parallel()
	p := prepare(t)
	// A site with a place on the map, a device, an envelope, a reading over
	// the limit and an alert; and one with a place and nothing else.
	place := func(serial int, name string) domain.Site {
		site, err := p.Store.CreateSite(repotest.Ctx(), domain.Site{
			NMI: repotest.NMI(t, serial), FeederID: p.fixture.Feeder.ID, NodeID: p.fixture.HouseA.ID, Name: name, Phase: 1,
			ExportCapW: 5000, ImportCapW: 14000, LatitudeDeg: repotest.Ptr(-33.85), LongitudeDeg: repotest.Ptr(151.06),
		})
		noErr(t, "site", err)
		return site
	}
	busy, quiet := place(30, "Ld30"), place(31, "Ld31")
	solar := p.device(t, busy, domain.DERSolar)
	_, err := p.publish(engineToken, p.runID, "batch-0001", envelope(busy.ID.String(), 0, 1000))
	noErr(t, "publish", err)
	_, err = p.ingest(p.Tokens.DeviceToken(busy.NMI), busy.NMI, []*doelabv1.Reading{reading(solar, 0, 2500)})
	noErr(t, "ingest", err)
	_, _, err = p.Store.OpenAlert(repotest.Ctx(), domain.Alert{
		SiteID: busy.ID, FeederID: p.fixture.Feeder.ID, Kind: domain.AlertConstraintBreach, Severity: domain.SeverityWarning,
		OpenedAt: repotest.Day, LimitW: repotest.Ptr(1000.0), PeakW: repotest.Ptr(2500.0),
	})
	noErr(t, "alert", err)

	res, err := p.telemetry("").GetFleetState(ctx, req(&doelabv1.GetFleetStateRequest{}))
	noErr(t, "fleet state", err)
	state := res.Msg
	if !state.GetAt().AsTime().Equal(repotest.Day) || len(state.GetFeeders()) != 1 ||
		state.GetFeeders()[0].GetFeederId() != p.fixture.Feeder.ID.String() || state.GetFeeders()[0].GetSitesOverLimit() != 1 ||
		state.GetFeeders()[0].GetLatestRunId() != p.runID {
		t.Errorf("the feeders = %v", state.GetFeeders())
	}
	if len(state.GetSites()) != 2 {
		t.Fatalf("%d sites, want the two with a place", len(state.GetSites()))
	}
	b, q := state.GetSites()[0], state.GetSites()[1]
	if b.GetSiteId() != busy.ID.String() || !b.GetReporting() || b.GetNetExportW() != 2500 || !b.GetOverLimit() ||
		b.GetEnvelope().GetExportLimitW() != 1000 || b.GetOpenAlert().GetKind() != doelabv1.AlertKind_ALERT_KIND_CONSTRAINT_BREACH ||
		b.GetOpenAlert().GetPeakW() != 2500 {
		t.Errorf("the busy site = %v", b)
	}
	if q.GetSiteId() != quiet.ID.String() || q.GetReporting() || q.NetExportW != nil || q.GetOverLimit() || q.Envelope != nil || q.OpenAlert != nil {
		t.Errorf("the quiet site = %v", q)
	}
}

func TestFeederStatesOverTheWire(t *testing.T) {
	t.Parallel()
	p := prepare(t)
	feederID := p.fixture.Feeder.ID.String()
	node := func(id string, slot int, vPU float64) *doelabv1.FeederNodeState {
		from := repotest.Day.Add(time.Duration(slot) * 30 * time.Minute)
		return &doelabv1.FeederNodeState{
			NodeId: id, ValidFrom: timestamppb.New(from), ValidTo: timestamppb.New(from.Add(30 * time.Minute)),
			ForecastVPu: []float64{vPU, 1.04, 1.04}, EnvelopeVPu: []float64{vPU + 0.03, 1.04, 1.04}, StaticVPu: []float64{vPU + 0.06, 1.04, 1.04},
		}
	}
	line := func(id string, slot int, powerW float64) *doelabv1.FeederLineState {
		from := repotest.Day.Add(time.Duration(slot) * 30 * time.Minute)
		return &doelabv1.FeederLineState{
			LineId: id, ValidFrom: timestamppb.New(from), ValidTo: timestamppb.New(from.Add(30 * time.Minute)),
			ForecastCurrentA: []float64{10, 0, 0, 10}, EnvelopeCurrentA: []float64{20, 0, 0, 20}, StaticCurrentA: []float64{30, 0, 0, 30},
			ForecastPowerW: powerW, EnvelopePowerW: -powerW, StaticPowerW: -2 * powerW,
		}
	}
	record := func(token, runID string, nodes []*doelabv1.FeederNodeState, lines []*doelabv1.FeederLineState) (*doelabv1.RecordFeederStatesResponse, error) {
		res, err := p.runs(token).RecordFeederStates(ctx, req(&doelabv1.RecordFeederStatesRequest{EnvelopeRunId: runID, Nodes: nodes, Lines: lines}))
		if err != nil {
			return nil, err
		}
		return res.Msg, nil
	}
	root, house, main := p.fixture.Root.ID.String(), p.fixture.HouseA.ID.String(), p.fixture.Lines[0].ID.String()

	got, err := record(engineToken, p.runID,
		[]*doelabv1.FeederNodeState{node(root, 0, 1.05), node(house, 0, 1.08), node(house, 1, 1.09)},
		[]*doelabv1.FeederLineState{line(main, 0, 1500)})
	noErr(t, "record", err)
	if got.GetNodes() != 3 || got.GetLines() != 1 {
		t.Errorf("recorded %d node states and %d line states", got.GetNodes(), got.GetLines())
	}

	// Now is the first half hour of the profile day.
	state, err := p.telemetry("").GetFeederState(ctx, req(&doelabv1.GetFeederStateRequest{FeederId: feederID}))
	noErr(t, "state now", err)
	s := state.Msg
	if !s.GetAt().AsTime().Equal(repotest.Day) || len(s.GetNodes()) != 2 || len(s.GetLines()) != 1 ||
		s.GetVMinPu() != 0.94 || s.GetVMaxPu() != 1.10 || s.GetLineLimitPct() != 100 || s.GetTransformerLimitPct() != 100 {
		t.Fatalf("the state now = %v", s)
	}
	l := s.GetLines()[0]
	if l.GetLineId() != main || l.GetFeederId() != feederID || l.GetEnvelopeRunId() != p.runID || l.GetForecastPowerW() != 1500 ||
		l.GetStaticPowerW() != -3000 || len(l.GetEnvelopeCurrentA()) != 4 || l.GetEnvelopeCurrentA()[3] != 20 {
		t.Errorf("the line state = %v", l)
	}
	for _, n := range s.GetNodes() {
		if n.GetFeederId() != feederID || n.GetEnvelopeRunId() != p.runID || len(n.GetForecastVPu()) != 3 || n.GetValidFrom() == nil {
			t.Errorf("a node state = %v", n)
		}
	}
	// At an instant that is named: the second half hour.
	later, err := p.telemetry("").GetFeederState(ctx, req(&doelabv1.GetFeederStateRequest{FeederId: feederID, At: day(40 * 60)}))
	noErr(t, "state later", err)
	if len(later.Msg.GetNodes()) != 1 || later.Msg.GetNodes()[0].GetForecastVPu()[0] != 1.09 || len(later.Msg.GetLines()) != 0 ||
		!later.Msg.GetAt().AsTime().Equal(repotest.Day.Add(40*time.Minute)) {
		t.Errorf("the state forty minutes on = %v", later.Msg)
	}
	_, err = p.telemetry("").GetFeederState(ctx, req(&doelabv1.GetFeederStateRequest{FeederId: unknownID}))
	wantCode(t, "an unknown feeder", err, connect.CodeNotFound)
	_, err = p.telemetry("").GetFeederState(ctx, req(&doelabv1.GetFeederStateRequest{FeederId: "not-a-uuid"}))
	wantViolation(t, "a malformed feeder id", err, "feeder_id")

	// Shape, refused by the validation interceptor.
	twoPhases := node(root, 2, 1)
	twoPhases.StaticVPu = []float64{1, 1}
	_, err = record(engineToken, p.runID, []*doelabv1.FeederNodeState{twoPhases}, nil)
	wantViolation(t, "two phases", err, "static_v_pu")
	threeConductors := line(main, 2, 1)
	threeConductors.ForecastCurrentA = []float64{1, 1, 1}
	_, err = record(engineToken, p.runID, nil, []*doelabv1.FeederLineState{threeConductors})
	wantViolation(t, "three conductors", err, "forecast_current_a")
	backwards := node(root, 2, 1)
	backwards.ValidTo = backwards.GetValidFrom()
	_, err = record(engineToken, p.runID, []*doelabv1.FeederNodeState{backwards}, nil)
	wantViolation(t, "an interval that ends where it starts", err, "valid_to must be after valid_from")
	backwardsLine := line(main, 2, 1)
	backwardsLine.ValidTo = backwardsLine.GetValidFrom()
	_, err = record(engineToken, p.runID, nil, []*doelabv1.FeederLineState{backwardsLine})
	wantViolation(t, "a line interval that ends where it starts", err, "valid_to must be after valid_from")
	_, err = record(engineToken, "not-a-uuid", nil, nil)
	wantViolation(t, "a malformed run id", err, "envelope_run_id")

	// Meaning, refused by the service.
	_, err = record(engineToken, unknownID, []*doelabv1.FeederNodeState{node(root, 2, 1)}, nil)
	wantCode(t, "an unknown run", err, connect.CodeFailedPrecondition)
	_, err = record(engineToken, p.runID, []*doelabv1.FeederNodeState{node(root, 48, 1)}, nil)
	wantCode(t, "an interval outside the horizon", err, connect.CodeInvalidArgument)
	_, err = record(engineToken, p.runID, []*doelabv1.FeederNodeState{node(unknownID, 2, 1)}, nil)
	wantCode(t, "a node that is not the feeder's", err, connect.CodeFailedPrecondition)

	// Only the engine records.
	_, err = record("", p.runID, []*doelabv1.FeederNodeState{node(root, 2, 1)}, nil)
	wantCode(t, "anonymous", err, connect.CodeUnauthenticated)
	_, err = record(operatorToken, p.runID, []*doelabv1.FeederNodeState{node(root, 2, 1)}, nil)
	wantCode(t, "an operator", err, connect.CodePermissionDenied)
}

// interval is the message for the half hour that starts slot half hours into
// the profile day.
func interval(slot int, netLoadW float64) *doelabv1.EnvelopeRunInterval {
	from := repotest.Day.Add(time.Duration(slot) * 30 * time.Minute)
	return &doelabv1.EnvelopeRunInterval{
		ValidFrom: timestamppb.New(from), ValidTo: timestamppb.New(from.Add(30 * time.Minute)),
		ForecastNetLoadW: netLoadW, ForecastLoadingPct: 12.5, ForecastVMinPu: 1.02, ForecastVMaxPu: 1.06,
		ExportLimitTotalW: 2000, ImportLimitTotalW: 7000, StaticLimitTotalW: 5000, StaticVMaxPu: 1.13,
		StaticBinding: doelabv1.BindingConstraint_BINDING_CONSTRAINT_VOLTAGE_HIGH, StaticBindingElement: "XDLAB000014",
		EnvelopeVMaxPu: new(1.09),
	}
}

// seedDay gives both sites a profile for the local day that holds the
// profile day's first instant: a flat load, and at site A a flat PV output.
func (p prepared) seedDay(t *testing.T) {
	t.Helper()
	zone, err := time.LoadLocation("Australia/Sydney")
	noErr(t, "zone", err)
	year := profile.Year{Start: profile.DefaultYearStart, Location: zone}
	for _, site := range []struct {
		id  uuid.UUID
		pvW float64
	}{{p.fixture.SiteA.ID, 3000}, {p.fixture.SiteB.ID, 0}} {
		rows := make([]domain.SiteProfile, 48)
		for i := range rows {
			at := repotest.Day.Add(-10*time.Hour + time.Duration(i)*30*time.Minute)
			rows[i] = domain.SiteProfile{TS: year.At(at).UTC(), LoadW: 500, PVW: site.pvW}
		}
		noErr(t, "profiles", p.Store.ReplaceSiteProfiles(repotest.Ctx(), site.id, rows))
	}
}

func TestRunIntervalsAndSeries(t *testing.T) {
	t.Parallel()
	p := prepare(t)
	a := p.fixture.SiteA
	feederID, siteA := p.fixture.Feeder.ID.String(), a.ID.String()
	solar := p.device(t, a, domain.DERSolar)
	p.seedDay(t)

	// The engine records what it forecast for two half hours.
	create := func(token, runID string, intervals ...*doelabv1.EnvelopeRunInterval) (*doelabv1.CreateEnvelopeRunIntervalsResponse, error) {
		res, err := p.runs(token).CreateEnvelopeRunIntervals(ctx, req(&doelabv1.CreateEnvelopeRunIntervalsRequest{EnvelopeRunId: runID, Intervals: intervals}))
		if err != nil {
			return nil, err
		}
		return res.Msg, nil
	}
	created, err := create(engineToken, p.runID, interval(0, -10000), interval(1, -11000))
	noErr(t, "create intervals", err)
	if created.GetCreated() != 2 {
		t.Errorf("created = %v", created)
	}
	listed, err := p.runs("").ListEnvelopeRunIntervals(ctx, req(&doelabv1.ListEnvelopeRunIntervalsRequest{EnvelopeRunId: p.runID}))
	noErr(t, "list intervals", err)
	rows := listed.Msg.GetIntervals()
	if len(rows) != 2 || rows[0].GetEnvelopeRunId() != p.runID || rows[0].GetFeederId() != feederID || rows[0].GetForecastNetLoadW() != -10000 ||
		rows[0].GetForecastLoadingPct() != 12.5 || rows[0].GetForecastVMinPu() != 1.02 || rows[0].GetForecastVMaxPu() != 1.06 ||
		rows[0].GetExportLimitTotalW() != 2000 || rows[0].GetImportLimitTotalW() != 7000 || rows[0].GetStaticLimitTotalW() != 5000 ||
		rows[0].GetStaticVMaxPu() != 1.13 || rows[0].GetEnvelopeVMaxPu() != 1.09 || rows[0].GetStaticBinding() != doelabv1.BindingConstraint_BINDING_CONSTRAINT_VOLTAGE_HIGH ||
		rows[0].GetStaticBindingElement() != "XDLAB000014" || rows[0].GetCreatedAt() == nil || !rows[1].GetValidFrom().AsTime().Equal(repotest.Day.Add(30*time.Minute)) {
		t.Errorf("intervals = %v", rows)
	}

	_, err = create(engineToken, p.runID, interval(0, 1))
	wantCode(t, "an interval twice", err, connect.CodeAlreadyExists)
	_, err = create(operatorToken, p.runID, interval(5, 1))
	wantCode(t, "the operator's token", err, connect.CodePermissionDenied)
	_, err = create(engineToken, unknownID, interval(5, 1))
	wantCode(t, "an unknown run", err, connect.CodeFailedPrecondition)
	_, err = create(engineToken, p.runID, interval(48, 1))
	wantCode(t, "an interval outside the horizon", err, connect.CodeInvalidArgument)
	noVolts := interval(5, 1)
	noVolts.ForecastVMinPu = 0
	_, err = create(engineToken, p.runID, noVolts)
	wantViolation(t, "a voltage of zero", err, "forecast_v_min_pu")
	_, err = create(engineToken, p.runID)
	wantViolation(t, "no intervals", err, "intervals")
	_, err = p.runs("").ListEnvelopeRunIntervals(ctx, req(&doelabv1.ListEnvelopeRunIntervalsRequest{EnvelopeRunId: unknownID}))
	wantCode(t, "intervals of an unknown run", err, connect.CodeNotFound)

	// Envelopes for three half hours, and a minute of telemetry in the first.
	_, err = p.publish(engineToken, p.runID, "batch-0001", envelope(siteA, 0, 2000), envelope(siteA, 1, 2000), envelope(siteA, 2, 2000))
	noErr(t, "publish", err)
	_, err = p.ingest(p.Tokens.DeviceToken(a.NMI), a.NMI, []*doelabv1.Reading{reading(solar, 0, 1000), reading(solar, 30, 2000)})
	noErr(t, "ingest", err)

	series, err := p.telemetry("").GetFeederSeries(ctx, req(&doelabv1.GetFeederSeriesRequest{FeederId: feederID, From: day(0), To: day(24 * 3600)}))
	noErr(t, "feeder series", err)
	fs := series.Msg
	if fs.GetVMinPu() != 0.94 || fs.GetVMaxPu() != 1.10 || fs.GetTransformerKva() != 100 || fs.GetStaticLimitW() != 5000 || len(fs.GetPoints()) != 2 {
		t.Fatalf("the feeder series = %v", fs)
	}
	first, second := fs.GetPoints()[0], fs.GetPoints()[1]
	if first.GetForecastNetLoadW() != -10000 || first.GetForecastLoadingPct() != 12.5 || first.GetForecastVMinPu() != 1.02 ||
		first.GetForecastVMaxPu() != 1.06 || first.GetExportLimitTotalW() != 2000 || first.GetImportLimitTotalW() != 7000 ||
		first.GetStaticLimitTotalW() != 5000 || first.GetStaticVMaxPu() != 1.13 || first.GetEnvelopeVMaxPu() != 1.09 ||
		first.GetStaticBinding() != doelabv1.BindingConstraint_BINDING_CONSTRAINT_VOLTAGE_HIGH || first.GetStaticBindingElement() != "XDLAB000014" ||
		!first.GetValidFrom().AsTime().Equal(repotest.Day) || !first.GetValidTo().AsTime().Equal(repotest.Day.Add(30*time.Minute)) ||
		first.MeasuredExportW == nil || first.GetMeasuredExportW() != 1500 || first.GetMeasuredImportW() != 0 {
		t.Errorf("the first point = %v", first)
	}
	if second.MeasuredExportW != nil || second.MeasuredImportW != nil || second.GetForecastNetLoadW() != -11000 {
		t.Errorf("the second point = %v", second)
	}

	site, err := p.telemetry("").GetSiteSeries(ctx, req(&doelabv1.GetSiteSeriesRequest{SiteId: siteA, From: day(0), To: day(3600)}))
	noErr(t, "site series", err)
	power, forecast := site.Msg.GetPower(), site.Msg.GetForecast()
	if len(power) != 1 || !power[0].GetBucket().AsTime().Equal(repotest.Day) || power[0].GetAvgNetExportW() != 1500 ||
		power[0].GetMaxNetExportW() != 2000 || power[0].GetAvgVoltageV() != 241.5 || power[0].AvgSocPct != nil {
		t.Errorf("the site's power = %v", power)
	}
	if len(forecast) != 2 || !forecast[1].GetTs().AsTime().Equal(repotest.Day.Add(30*time.Minute)) || forecast[0].GetLoadW() != 500 || forecast[0].GetPvW() != 3000 {
		t.Errorf("the site's forecast = %v", forecast)
	}

	// The day in numbers. Site A could export 3000 − 500 = 2500 W in each of
	// three half hours, and its envelope allows 2000 W: 3.75 kWh possible,
	// 3.0 kWh let out. The fixed limit of 5000 W holds nothing back, and
	// breaks a network limit in both half hours the engine reported on.
	report, err := p.telemetry("").GetDailyReport(ctx, req(&doelabv1.GetDailyReportRequest{FeederId: feederID, Day: day(0)}))
	noErr(t, "report", err)
	r := report.Msg
	if !r.GetFrom().AsTime().Equal(repotest.Day.Add(-10*time.Hour)) || !r.GetTo().AsTime().Equal(repotest.Day.Add(14*time.Hour)) ||
		r.GetEnrolledSites() != 1 || r.GetIntervals() != 3 || r.GetPotentialExportKwh() != 3.75 || r.GetEnvelopeExportKwh() != 3 ||
		r.GetEnvelopeCurtailedKwh() != 0.75 || r.GetStaticExportKwh() != 3.75 || r.GetStaticCurtailedKwh() != 0 ||
		r.GetStaticViolationIntervals() != 2 || r.GetStaticLimitW() != 5000 || r.GetConstraintBreaches() != 0 ||
		r.GetDeviceOfflineAlerts() != 0 || r.GetBackstopEvents() != 0 {
		t.Errorf("the report = %v", r)
	}

	week := day(8 * 24 * 3600)
	_, err = p.telemetry("").GetFeederSeries(ctx, req(&doelabv1.GetFeederSeriesRequest{FeederId: feederID, From: day(0), To: week}))
	wantViolation(t, "eight days of a feeder", err, "at most 7 days")
	_, err = p.telemetry("").GetSiteSeries(ctx, req(&doelabv1.GetSiteSeriesRequest{SiteId: siteA, From: day(0), To: week}))
	wantViolation(t, "eight days of a site", err, "at most 7 days")
	_, err = p.telemetry("").GetFeederSeries(ctx, req(&doelabv1.GetFeederSeriesRequest{FeederId: unknownID, From: day(0), To: day(60)}))
	wantCode(t, "the series of an unknown feeder", err, connect.CodeNotFound)
	_, err = p.telemetry("").GetSiteSeries(ctx, req(&doelabv1.GetSiteSeriesRequest{SiteId: unknownID, From: day(0), To: day(60)}))
	wantCode(t, "the series of an unknown site", err, connect.CodeNotFound)
	_, err = p.telemetry("").GetDailyReport(ctx, req(&doelabv1.GetDailyReportRequest{FeederId: unknownID, Day: day(0)}))
	wantCode(t, "the report of an unknown feeder", err, connect.CodeNotFound)
	_, err = p.telemetry("").GetDailyReport(ctx, req(&doelabv1.GetDailyReportRequest{FeederId: feederID}))
	wantViolation(t, "a report with no day", err, "day")
}

func TestAlerts(t *testing.T) {
	t.Parallel()
	p := prepare(t)
	a := p.fixture.SiteA
	feederID, siteA := p.fixture.Feeder.ID.String(), a.ID.String()
	solar := p.device(t, a, domain.DERSolar)
	token := p.Tokens.DeviceToken(a.NMI)

	list := func(r *doelabv1.ListAlertsRequest) []*doelabv1.Alert {
		t.Helper()
		r.FeederId = feederID
		res, err := p.alerts("").ListAlerts(ctx, req(r))
		noErr(t, "list alerts", err)
		return res.Msg.GetAlerts()
	}

	// The site exports five times its limit for a minute.
	_, err := p.publish(engineToken, p.runID, "batch-0001", envelope(siteA, 0, 1000))
	noErr(t, "publish", err)
	_, err = p.ingest(token, a.NMI, []*doelabv1.Reading{reading(solar, 0, 5000)}, []*doelabv1.Reading{reading(solar, 61, 5200)})
	noErr(t, "ingest", err)
	alerts := list(&doelabv1.ListAlertsRequest{})
	if len(alerts) != 1 {
		t.Fatalf("alerts = %v", alerts)
	}
	breach := alerts[0]
	if breach.GetKind() != doelabv1.AlertKind_ALERT_KIND_CONSTRAINT_BREACH || breach.GetSeverity() != doelabv1.AlertSeverity_ALERT_SEVERITY_WARNING ||
		breach.GetSiteId() != siteA || breach.DeviceId != nil || !breach.GetOpenedAt().AsTime().Equal(repotest.Day) || breach.ResolvedAt != nil ||
		breach.GetLimitW() != 1000 || breach.GetPeakW() != 5200 || breach.GetDetail() == "" || breach.AcknowledgedAt != nil ||
		breach.GetCreatedAt() == nil || breach.GetUpdatedAt() == nil {
		t.Errorf("the breach = %v", breach)
	}

	// Ten minutes of silence: the sweep finds the device gone.
	p.Clock.Advance(10 * time.Minute)
	opened, err := p.Compliance.Sweep(ctx)
	if err != nil || opened != 1 {
		t.Fatalf("the sweep opened %d, %v", opened, err)
	}
	offlineKind := doelabv1.AlertKind_ALERT_KIND_DEVICE_OFFLINE
	offline := list(&doelabv1.ListAlertsRequest{Kind: &offlineKind})
	if len(offline) != 1 || offline[0].GetDeviceId() != solar || offline[0].GetSeverity() != doelabv1.AlertSeverity_ALERT_SEVERITY_INFO || offline[0].LimitW != nil {
		t.Errorf("offline alerts = %v", offline)
	}
	siteB := p.fixture.SiteB.ID.String()
	if got := list(&doelabv1.ListAlertsRequest{SiteId: &siteB}); len(got) != 0 {
		t.Errorf("site B's alerts = %v", got)
	}
	if got := list(&doelabv1.ListAlertsRequest{SiteId: &siteA, OpenOnly: true}); len(got) != 2 {
		t.Errorf("site A's open alerts = %v", got)
	}
	// Newest first, a page at a time.
	firstPage, err := p.alerts("").ListAlerts(ctx, req(&doelabv1.ListAlertsRequest{FeederId: feederID, PageSize: 1}))
	noErr(t, "first page", err)
	if len(firstPage.Msg.GetAlerts()) != 1 || firstPage.Msg.GetAlerts()[0].GetId() != offline[0].GetId() || firstPage.Msg.GetNextPageToken() == "" {
		t.Errorf("the first page = %v", firstPage.Msg)
	}

	// The device comes back, inside its limit: both alerts resolve.
	_, err = p.ingest(token, a.NMI, []*doelabv1.Reading{reading(solar, 700, 900)})
	noErr(t, "ingest", err)
	if got := list(&doelabv1.ListAlertsRequest{OpenOnly: true}); len(got) != 0 {
		t.Errorf("open alerts after the device came back = %v", got)
	}
	got, err := p.alerts("").GetAlert(ctx, req(&doelabv1.GetAlertRequest{Id: breach.GetId()}))
	noErr(t, "get", err)
	if !got.Msg.GetAlert().GetResolvedAt().AsTime().Equal(repotest.Day.Add(700 * time.Second)) {
		t.Errorf("the breach after it resolved = %v", got.Msg.GetAlert())
	}

	// An operator acknowledges it; nobody else may.
	seen, err := p.alerts(operatorToken).AcknowledgeAlert(ctx, req(&doelabv1.AcknowledgeAlertRequest{Id: breach.GetId()}))
	noErr(t, "acknowledge", err)
	if seen.Msg.GetAlert().GetAcknowledgedBy() != "operator" || seen.Msg.GetAlert().GetAcknowledgedAt() == nil {
		t.Errorf("acknowledged = %v", seen.Msg.GetAlert())
	}
	_, err = p.alerts("").AcknowledgeAlert(ctx, req(&doelabv1.AcknowledgeAlertRequest{Id: breach.GetId()}))
	wantCode(t, "acknowledging with no token", err, connect.CodeUnauthenticated)
	_, err = p.alerts(engineToken).AcknowledgeAlert(ctx, req(&doelabv1.AcknowledgeAlertRequest{Id: breach.GetId()}))
	wantCode(t, "acknowledging as the engine", err, connect.CodePermissionDenied)
	_, err = p.alerts(operatorToken).AcknowledgeAlert(ctx, req(&doelabv1.AcknowledgeAlertRequest{Id: unknownID}))
	wantCode(t, "acknowledging an unknown alert", err, connect.CodeNotFound)
	_, err = p.alerts("").GetAlert(ctx, req(&doelabv1.GetAlertRequest{Id: unknownID}))
	wantCode(t, "an unknown alert", err, connect.CodeNotFound)
	_, err = p.alerts("").ListAlerts(ctx, req(&doelabv1.ListAlertsRequest{FeederId: unknownID}))
	wantCode(t, "alerts of an unknown feeder", err, connect.CodeNotFound)
	unspecified := doelabv1.AlertKind_ALERT_KIND_UNSPECIFIED
	_, err = p.alerts("").ListAlerts(ctx, req(&doelabv1.ListAlertsRequest{FeederId: feederID, Kind: &unspecified}))
	wantViolation(t, "a kind that is no kind", err, "kind")
}

func TestBackstop(t *testing.T) {
	t.Parallel()
	p := prepare(t)
	a := p.fixture.SiteA
	feederID, siteA := p.fixture.Feeder.ID.String(), a.ID.String()

	_, err := p.publish(engineToken, p.runID, "batch-0001", envelope(siteA, 0, 1000), envelope(siteA, 1, 1100))
	noErr(t, "publish", err)
	sub := p.subscribe(t, p.Tokens.DeviceToken(a.NMI), a.NMI)
	if got := sub.next(t); got.GetEnvelope().GetExportLimitW() != 1000 {
		t.Fatalf("before the backstop: %v", got)
	}

	trigger := func(token string, event *doelabv1.BackstopEvent, sites ...string) (*doelabv1.CreateBackstopEventResponse, error) {
		res, err := p.backstops(token).CreateBackstopEvent(ctx, req(&doelabv1.CreateBackstopEventRequest{BackstopEvent: event, SiteIds: sites}))
		if err != nil {
			return nil, err
		}
		return res.Msg, nil
	}
	fault := func() *doelabv1.BackstopEvent {
		return &doelabv1.BackstopEvent{FeederId: feederID, Reason: "transformer fault", ExportLimitW: 0}
	}

	// Only an operator may pull it, and must say why.
	_, err = trigger("", fault())
	wantCode(t, "no token", err, connect.CodeUnauthenticated)
	_, err = trigger(engineToken, fault())
	wantCode(t, "the engine's token", err, connect.CodePermissionDenied)
	_, err = trigger(operatorToken, &doelabv1.BackstopEvent{FeederId: feederID})
	wantViolation(t, "no reason", err, "feeder_id and reason are required")
	withID := fault()
	withID.Id = unknownID
	_, err = trigger(operatorToken, withID)
	wantViolation(t, "an id from the client", err, "set by the server")
	_, err = trigger(operatorToken, fault(), unknownID)
	wantCode(t, "a site that is not the feeder's", err, connect.CodeFailedPrecondition)
	_, err = trigger(operatorToken, fault(), "not-a-uuid")
	wantViolation(t, "a malformed site id", err, "site_ids")

	// The operator pulls it: the device hears at once.
	created, err := trigger(operatorToken, fault())
	noErr(t, "trigger", err)
	event := created.GetBackstopEvent()
	if event.GetId() == "" || event.GetFeederId() != feederID || event.GetReason() != "transformer fault" || event.GetExportLimitW() != 0 ||
		event.GetTriggeredBy() != "operator" || !event.GetTriggeredAt().AsTime().Equal(repotest.Day) || event.ClearedAt != nil || event.ClearedBy != nil ||
		event.GetCreatedAt() == nil || event.GetUpdatedAt() == nil || len(created.GetSiteIds()) != 1 || created.GetSiteIds()[0] != siteA {
		t.Errorf("the backstop = %v", created)
	}
	held := sub.next(t).GetEnvelope()
	if held.GetSource() != doelabv1.EnvelopeSource_ENVELOPE_SOURCE_BACKSTOP || held.GetExportLimitW() != 0 || held.GetBackstopEventId() != event.GetId() ||
		held.GetImportLimitW() != 7000 {
		t.Errorf("the device's envelope under the backstop = %v", held)
	}

	// The engine is refused while it lasts, and a second backstop too.
	_, err = p.publish(engineToken, p.runID, "batch-0002", envelope(siteA, 0, 4000))
	wantCode(t, "publishing over the backstop", err, connect.CodeFailedPrecondition)
	_, err = trigger(operatorToken, fault())
	wantCode(t, "a second backstop", err, connect.CodeAlreadyExists)

	got, err := p.backstops("").GetBackstopEvent(ctx, req(&doelabv1.GetBackstopEventRequest{Id: event.GetId()}))
	noErr(t, "get", err)
	if got.Msg.GetBackstopEvent().GetId() != event.GetId() || len(got.Msg.GetSiteIds()) != 1 {
		t.Errorf("get = %v", got.Msg)
	}
	listed, err := p.backstops("").ListBackstopEvents(ctx, req(&doelabv1.ListBackstopEventsRequest{FeederId: feederID}))
	noErr(t, "list", err)
	if len(listed.Msg.GetBackstopEvents()) != 1 || listed.Msg.GetNextPageToken() != "" {
		t.Errorf("list = %v", listed.Msg)
	}

	// Five minutes on the operator clears it: the engine's envelope is back.
	p.Clock.Advance(5 * time.Minute)
	_, err = p.backstops("").ClearBackstop(ctx, req(&doelabv1.ClearBackstopRequest{Id: event.GetId()}))
	wantCode(t, "clearing with no token", err, connect.CodeUnauthenticated)
	cleared, err := p.backstops(operatorToken).ClearBackstop(ctx, req(&doelabv1.ClearBackstopRequest{Id: event.GetId()}))
	noErr(t, "clear", err)
	if c := cleared.Msg.GetBackstopEvent(); c.GetClearedBy() != "operator" || !c.GetClearedAt().AsTime().Equal(repotest.Day.Add(5*time.Minute)) {
		t.Errorf("cleared = %v", c)
	}
	restored := sub.next(t).GetEnvelope()
	if restored.GetSource() != doelabv1.EnvelopeSource_ENVELOPE_SOURCE_ENGINE || restored.GetExportLimitW() != 1000 || restored.GetEnvelopeRunId() != p.runID {
		t.Errorf("the device's envelope after the backstop = %v", restored)
	}
	again, err := p.backstops(operatorToken).ClearBackstop(ctx, req(&doelabv1.ClearBackstopRequest{Id: event.GetId()}))
	noErr(t, "clear again", err)
	if !again.Msg.GetBackstopEvent().GetClearedAt().AsTime().Equal(cleared.Msg.GetBackstopEvent().GetClearedAt().AsTime()) {
		t.Errorf("cleared again = %v", again.Msg.GetBackstopEvent())
	}
	_, err = p.publish(engineToken, p.runID, "batch-0003", envelope(siteA, 0, 4000))
	noErr(t, "publishing after the backstop", err)

	_, err = p.backstops("").GetBackstopEvent(ctx, req(&doelabv1.GetBackstopEventRequest{Id: unknownID}))
	wantCode(t, "an unknown backstop", err, connect.CodeNotFound)
	_, err = p.backstops(operatorToken).ClearBackstop(ctx, req(&doelabv1.ClearBackstopRequest{Id: unknownID}))
	wantCode(t, "clearing an unknown backstop", err, connect.CodeNotFound)
	_, err = p.backstops("").ListBackstopEvents(ctx, req(&doelabv1.ListBackstopEventsRequest{FeederId: unknownID}))
	wantCode(t, "backstops of an unknown feeder", err, connect.CodeNotFound)
}
