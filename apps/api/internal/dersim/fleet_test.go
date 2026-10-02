package dersim

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/gen/doelab/v1/doelabv1connect"
	"doelab/api/internal/apitest"
	"doelab/api/internal/domain"
	"doelab/api/internal/profile"
	"doelab/api/internal/repo/repotest"
	"doelab/api/internal/service"
)

// world is the test API with the fixture feeder in it, and a fleet for it.
// Site A has a 5 kW solar inverter; site B has no device until a test gives
// it one. Both have a profile for the day: 500 W of load, and at site A
// 3000 W of sun. The test clock stands at 10:00 in Sydney.
type world struct {
	*apitest.API
	fixture repotest.Fixture
	solar   domain.Device
	fleet   *Fleet
	log     *logBuffer
	runID   string
}

// logBuffer is a log that a test reads while the fleet writes it.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) has(text string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Contains(b.buf.String(), text)
}

var ctx = repotest.Ctx()

func noErr(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// at is the instant so many minutes into the profile day.
func at(minutes int) time.Time { return repotest.Day.Add(time.Duration(minutes) * time.Minute) }

func newWorld(t *testing.T, withConfig bool) *world {
	t.Helper()
	a := apitest.New(t)
	w := &world{API: a, fixture: repotest.Seed(t, a.Store, "LV10", 1), log: &logBuffer{}}
	if withConfig {
		_, err := a.Store.CreateEnvelopeConfig(ctx, repotest.Config(w.fixture.Feeder.ID))
		noErr(t, "config", err)
	}
	w.profiles(t, w.fixture.SiteA, 3000)
	w.profiles(t, w.fixture.SiteB, 0)
	w.solar = w.device(t, w.fixture.SiteA, domain.DERSolar, 5000)

	w.fleet = NewFleet(NewAPI(a.HTTP, a.URL), "LV10", a.Tokens.DeviceToken,
		slog.New(slog.NewTextHandler(w.log, &slog.HandlerOptions{Level: slog.LevelDebug})))
	w.fleet.Retry = 10 * time.Millisecond
	t.Cleanup(w.fleet.Close)
	return w
}

// profiles gives a site a flat profile for the local day that holds the test
// clock.
func (w *world) profiles(t *testing.T, site domain.Site, pvW float64) {
	t.Helper()
	zone, err := time.LoadLocation("Australia/Sydney")
	noErr(t, "zone", err)
	year := profile.Year{Start: profile.DefaultYearStart, Location: zone}
	rows := make([]domain.SiteProfile, 48)
	for i := range rows {
		rows[i] = domain.SiteProfile{TS: year.At(at(-600 + 30*i)).UTC(), LoadW: 500, PVW: pvW}
	}
	noErr(t, "profiles", w.Store.ReplaceSiteProfiles(ctx, site.ID, rows))
}

func (w *world) device(t *testing.T, site domain.Site, kind domain.DERType, ratedW float64) domain.Device {
	t.Helper()
	d, err := w.Store.CreateDevice(ctx, domain.Device{SiteID: site.ID, DERType: kind, RatedW: ratedW})
	noErr(t, "device", err)
	return d
}

// publish gives site A an envelope for the half hour that starts slot half
// hours into the day, as the engine would.
func (w *world) publish(t *testing.T, slot int, exportW float64) {
	t.Helper()
	engine := apitest.As(apitest.EngineToken)
	if w.runID == "" {
		config, err := w.Store.GetActiveEnvelopeConfig(ctx, w.fixture.Feeder.ID)
		noErr(t, "active config", err)
		run, err := doelabv1connect.NewEnvelopeRunServiceClient(w.HTTP, w.URL, engine).CreateEnvelopeRun(ctx,
			connect.NewRequest(&doelabv1.CreateEnvelopeRunRequest{EnvelopeRun: &doelabv1.EnvelopeRun{
				FeederId: w.fixture.Feeder.ID.String(), EnvelopeConfigId: config.ID.String(), IdempotencyKey: "run-0001",
				HorizonFrom: timestamppb.New(at(0)), HorizonTo: timestamppb.New(at(24 * 60)), EngineVersion: "test",
			}}))
		noErr(t, "run", err)
		w.runID = run.Msg.GetEnvelopeRun().GetId()
	}
	from := at(30 * slot)
	_, err := doelabv1connect.NewEnvelopeServiceClient(w.HTTP, w.URL, engine).PublishEnvelopes(ctx,
		connect.NewRequest(&doelabv1.PublishEnvelopesRequest{
			EnvelopeRunId: w.runID, IdempotencyKey: "batch-" + from.Format("150405") + "-" + time.Now().Format("150405.000000"),
			Envelopes: []*doelabv1.Envelope{{
				SiteId: w.fixture.SiteA.ID.String(), ValidFrom: timestamppb.New(from), ValidTo: timestamppb.New(from.Add(30 * time.Minute)),
				ExportLimitW: exportW, ImportLimitW: 7000,
				ExportBinding: doelabv1.BindingConstraint_BINDING_CONSTRAINT_VOLTAGE_HIGH, ExportBindingElement: "Ld1_LOAD_A",
				ImportBinding: doelabv1.BindingConstraint_BINDING_CONSTRAINT_SITE_CAP,
			}},
		}))
	noErr(t, "publish", err)
}

// hears waits until site A's subscription has delivered an envelope with the
// given export limit.
func (w *world) hears(t *testing.T, exportW float64) {
	t.Helper()
	waitFor(t, "the envelope to reach the device", func() bool {
		e, _ := w.fleet.Holding(w.fixture.SiteA.NMI)
		return e != nil && e.GetExportLimitW() == exportW
	})
}

func (w *world) tick(t *testing.T, minute int) Stats {
	t.Helper()
	stats, err := w.fleet.Tick(ctx, at(minute))
	noErr(t, "tick", err)
	return stats
}

func (w *world) flush(t *testing.T) int {
	t.Helper()
	n, err := w.fleet.Flush()
	noErr(t, "flush", err)
	return n
}

// readings returns what a device has reported for the day.
func (w *world) readings(t *testing.T, device domain.Device) []domain.Reading {
	t.Helper()
	rows, _, err := w.Store.ListReadings(ctx, device.ID, at(0), at(24*60), domain.Page{Size: 2000})
	noErr(t, "readings", err)
	return rows
}

func (w *world) openAlerts(t *testing.T, kind domain.AlertKind) []domain.Alert {
	t.Helper()
	alerts, _, err := w.Store.ListAlerts(ctx, w.fixture.Feeder.ID, service.AlertFilter{Kind: &kind, OpenOnly: true}, domain.Page{Size: 100})
	noErr(t, "alerts", err)
	return alerts
}

func TestDevicesObeyTheirEnvelope(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	w.publish(t, 0, 600)
	w.fleet.BatchesPerStream = 2
	noErr(t, "connect", w.fleet.Connect(ctx))
	if w.fleet.Units() != 1 {
		t.Fatalf("%d units, want 1: site B has no device", w.fleet.Units())
	}
	w.hears(t, 600)
	if e, heard := w.fleet.Holding(w.fixture.SiteA.NMI); heard != 1 || e.GetSiteId() != w.fixture.SiteA.ID.String() {
		t.Errorf("holding %v after %d messages", e, heard)
	}
	if e, heard := w.fleet.Holding(w.fixture.SiteB.NMI); e != nil || heard != 0 {
		t.Errorf("a site with no device holds %v", e)
	}

	for minute := range 5 {
		if stats := w.tick(t, minute); stats != (Stats{Reported: 1}) {
			t.Errorf("minute %d: %+v", minute, stats)
		}
	}
	// A stream carries two batches: the fifth is on a stream of its own.
	if n := w.flush(t); n != 1 {
		t.Errorf("flush stored %d readings, want 1", n)
	}
	if n := w.flush(t); n != 0 {
		t.Errorf("a second flush stored %d readings", n)
	}

	// The sun gives at least 1500 W and the home takes at most 550 W, so
	// every step has more to export than the limit lets out.
	rows := w.readings(t, w.solar)
	if len(rows) != 5 {
		t.Fatalf("%d readings, want 5", len(rows))
	}
	for i, r := range rows {
		if r.NetExportW != 600 || r.PowerW < 600+450 || r.PowerW > 600+550 || r.SOCPct != nil || !r.TS.Equal(at(i)) {
			t.Errorf("reading %d = %+v, want 600 W of export from 1050 to 1150 W of PV", i, r)
		}
	}
	if alerts := w.openAlerts(t, domain.AlertConstraintBreach); len(alerts) != 0 {
		t.Errorf("a compliant device raised %v", alerts)
	}
}

func TestDeviceWithNoEnvelopeUsesItsDefault(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	w.fleet.DefaultExportW = 800
	noErr(t, "connect", w.fleet.Connect(ctx))

	// It asks once, and takes "none" as the answer for the interval.
	if stats := w.tick(t, 0); stats != (Stats{Reported: 1, Asked: 1}) {
		t.Errorf("the first step: %+v", stats)
	}
	if stats := w.tick(t, 1); stats != (Stats{Reported: 1}) {
		t.Errorf("the second step: %+v", stats)
	}

	// An envelope that arrives in the interval is heard, not asked for.
	w.publish(t, 0, 300)
	w.hears(t, 300)
	if stats := w.tick(t, 2); stats != (Stats{Reported: 1}) {
		t.Errorf("after the publish: %+v", stats)
	}

	// The test clock stands still, so the subscription never rolls over: at
	// the next interval the device holds an envelope that has ended, and asks.
	if stats := w.tick(t, 30); stats != (Stats{Reported: 1, Asked: 1}) {
		t.Errorf("the next interval: %+v", stats)
	}
	w.publish(t, 2, 400)
	if stats := w.tick(t, 60); stats != (Stats{Reported: 1, Asked: 1}) {
		t.Errorf("an interval with an envelope: %+v", stats)
	}
	// The answer is kept for the interval.
	if stats := w.tick(t, 61); stats != (Stats{Reported: 1}) {
		t.Errorf("the step after asking: %+v", stats)
	}
	w.flush(t)

	want := []float64{800, 800, 300, 800, 400, 400}
	rows := w.readings(t, w.solar)
	if len(rows) != len(want) {
		t.Fatalf("%d readings, want %d", len(rows), len(want))
	}
	for i, r := range rows {
		if r.NetExportW != want[i] {
			t.Errorf("reading %d exports %v W, want %v", i, r.NetExportW, want[i])
		}
	}
}

func TestFeederWithNoConfig(t *testing.T) {
	t.Parallel()
	w := newWorld(t, false)
	noErr(t, "connect", w.fleet.Connect(ctx))
	// Without a config the PV is not scaled, and "no envelope" is asked
	// again every five minutes.
	for minute, asked := range []int{1, 0, 0, 0, 0, 1} {
		if stats := w.tick(t, minute); stats != (Stats{Reported: 1, Asked: asked}) {
			t.Errorf("minute %d: %+v", minute, stats)
		}
	}
	if n := w.flush(t); n != 6 {
		t.Errorf("stored %d readings, want 6", n)
	}
	for _, r := range w.readings(t, w.solar) {
		if r.NetExportW != DefaultExportW {
			t.Errorf("exports %v W, want the default %v", r.NetExportW, DefaultExportW)
		}
	}
}

func TestRogueDeviceBreaches(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	w.publish(t, 0, 600)
	w.fleet.RogueFraction = 0.01 // of one device: one
	noErr(t, "connect", w.fleet.Connect(ctx))
	if !w.fleet.units[0].rogue || w.fleet.units[0].flaky {
		t.Fatal("the device is not rogue")
	}
	// A caller's own pick replaces the fractions': none, then more than the
	// fleet has, then the one rogue again.
	w.fleet.Misbehave(0, 0)
	if rogues, flaky := w.fleet.Misbehaving(); rogues != 0 || flaky != 0 {
		t.Errorf("%d rogue and %d flaky devices after a pick of none", rogues, flaky)
	}
	w.fleet.Misbehave(0, 5)
	if rogues, flaky := w.fleet.Misbehaving(); rogues != 0 || flaky != 1 {
		t.Errorf("%d rogue and %d flaky devices after a pick of five flaky from one", rogues, flaky)
	}
	w.fleet.Misbehave(1, 1)
	if rogues, flaky := w.fleet.Misbehaving(); rogues != 1 || flaky != 0 {
		t.Errorf("%d rogue and %d flaky devices after a pick of one of each from one", rogues, flaky)
	}

	// The grace period is a minute: the second reading over the limit opens
	// the alert.
	for minute := range 3 {
		if stats := w.tick(t, minute); stats != (Stats{Reported: 1}) {
			t.Errorf("minute %d: %+v", minute, stats)
		}
	}
	w.flush(t)
	for _, r := range w.readings(t, w.solar) {
		if r.NetExportW < 900 {
			t.Errorf("a rogue device exports %v W under a 600 W limit", r.NetExportW)
		}
	}
	alerts := w.openAlerts(t, domain.AlertConstraintBreach)
	if len(alerts) != 1 || alerts[0].SiteID != w.fixture.SiteA.ID || !alerts[0].OpenedAt.Equal(at(0)) || *alerts[0].LimitW != 600 {
		t.Errorf("alerts = %+v, want one breach of the 600 W limit since the first reading", alerts)
	}
}

func TestFlakyDeviceGoesSilent(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	w.fleet.FlakyFraction, w.fleet.RogueFraction = 1, -1
	noErr(t, "connect", w.fleet.Connect(ctx))
	u := w.fleet.units[0]
	if !u.flaky || u.rogue || u.silentFrom < 0 || u.silentFrom >= flakyPeriod {
		t.Fatalf("the device: flaky %v, rogue %v, silent from %v", u.flaky, u.rogue, u.silentFrom)
	}
	// Silent from the third minute of the day, for twenty minutes.
	u.silentFrom = (time.Duration(at(2).Unix()) * time.Second) % flakyPeriod

	for minute := range 23 {
		want := Stats{Reported: 1}
		if minute >= 2 && minute < 22 {
			want = Stats{Silent: 1}
		}
		if minute == 0 {
			want.Asked = 1
		}
		if stats := w.tick(t, minute); stats != want {
			t.Errorf("minute %d: %+v, want %+v", minute, stats, want)
		}
		if minute == 9 {
			// Heard last at minute 1; the offline period is five minutes.
			w.flush(t)
			w.Clock.Advance(9 * time.Minute)
			opened, err := w.Compliance.Sweep(ctx)
			if err != nil || opened != 1 {
				t.Fatalf("sweep opened %d alerts, %v", opened, err)
			}
			if alerts := w.openAlerts(t, domain.AlertDeviceOffline); len(alerts) != 1 || !alerts[0].OpenedAt.Equal(at(6)) {
				t.Fatalf("offline alerts = %+v", alerts)
			}
		}
	}
	// Its first reading after the silence resolves the alert.
	w.flush(t)
	if alerts := w.openAlerts(t, domain.AlertDeviceOffline); len(alerts) != 0 {
		t.Errorf("the alert stays open: %+v", alerts)
	}
	if rows := w.readings(t, w.solar); len(rows) != 3 {
		t.Errorf("%d readings, want 3", len(rows))
	}
}

func TestBackstopReachesTheDevice(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	w.publish(t, 0, 600)
	noErr(t, "connect", w.fleet.Connect(ctx))
	w.hears(t, 600)
	w.tick(t, 0)

	backstops := doelabv1connect.NewBackstopServiceClient(w.HTTP, w.URL, apitest.As(apitest.OperatorToken))
	event, err := backstops.CreateBackstopEvent(ctx, connect.NewRequest(&doelabv1.CreateBackstopEventRequest{
		BackstopEvent: &doelabv1.BackstopEvent{FeederId: w.fixture.Feeder.ID.String(), Reason: "transformer alarm"},
	}))
	noErr(t, "backstop", err)
	w.hears(t, 0)
	if e, _ := w.fleet.Holding(w.fixture.SiteA.NMI); e.GetSource() != doelabv1.EnvelopeSource_ENVELOPE_SOURCE_BACKSTOP {
		t.Errorf("the device holds %v, want the backstop's envelope", e)
	}
	w.tick(t, 1)
	w.tick(t, 2)

	_, err = backstops.ClearBackstop(ctx, connect.NewRequest(&doelabv1.ClearBackstopRequest{Id: event.Msg.GetBackstopEvent().GetId()}))
	noErr(t, "clear", err)
	w.hears(t, 600)
	w.tick(t, 3)
	w.flush(t)

	want := []float64{600, 0, 0, 600}
	rows := w.readings(t, w.solar)
	if len(rows) != len(want) {
		t.Fatalf("%d readings, want %d", len(rows), len(want))
	}
	for i, r := range rows {
		if r.NetExportW != want[i] {
			t.Errorf("reading %d exports %v W, want %v", i, r.NetExportW, want[i])
		}
	}
	if alerts := w.openAlerts(t, domain.AlertConstraintBreach); len(alerts) != 0 {
		t.Errorf("a compliant device raised %v", alerts)
	}
}

func TestBatteryAndCar(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	battery := w.device(t, w.fixture.SiteA, domain.DERBattery, 5000)
	// Site B gives no battery size.
	other := w.device(t, w.fixture.SiteB, domain.DERBattery, 3000)
	car := w.device(t, w.fixture.SiteB, domain.DEREV, 7000)
	noErr(t, "connect", w.fleet.Connect(ctx))
	if w.fleet.Units() != 2 {
		t.Fatalf("%d units, want 2", w.fleet.Units())
	}
	var a, b *unit
	for _, u := range w.fleet.units {
		if u.site.ID == w.fixture.SiteA.ID {
			a = u
		} else {
			b = u
		}
	}
	if a.plant.BatteryKWh != 10 || a.plant.BatteryW != 5000 || a.plant.SolarW != 5000 || a.plant.EVW != 0 {
		t.Errorf("site A's plant = %+v", a.plant)
	}
	if b.plant.BatteryKWh != defaultBatteryKWh || b.plant.BatteryW != 3000 || b.plant.EVW != 7000 || b.plant.SolarW != 0 {
		t.Errorf("site B's plant = %+v", b.plant)
	}

	// 19:00 in Sydney: the car is home, and there is no sun in this profile
	// either way for site B.
	const evening = 9 * 60
	if stats := w.tick(t, evening); stats != (Stats{Reported: 2, Asked: 2}) {
		t.Errorf("the step: %+v", stats)
	}
	if n := w.flush(t); n != 4 {
		t.Errorf("stored %d readings, want 4: one for each device", n)
	}

	// At site A the battery takes the surplus before any of it is exported.
	sun, charge := w.readings(t, w.solar)[0], w.readings(t, battery)[0]
	if charge.PowerW >= 0 || charge.SOCPct == nil || *charge.SOCPct <= 50 || charge.NetExportW != sun.NetExportW || sun.SOCPct != nil {
		t.Errorf("site A: solar %+v, battery %+v", sun, charge)
	}
	// At site B the battery serves the home, and the car charges from the grid.
	serve, drive := w.readings(t, other)[0], w.readings(t, car)[0]
	if serve.PowerW < 450 || *serve.SOCPct >= 50 || drive.PowerW != -7000 || *drive.SOCPct <= evArrivalSOC*100 ||
		drive.NetExportW != -7000 || serve.NetExportW != -7000 {
		t.Errorf("site B: battery %+v, car %+v", serve, drive)
	}
}

func TestWrongTokenIsRefused(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	w.fleet.Token = func(string) string { return w.Tokens.DeviceToken(w.fixture.SiteB.NMI) }
	noErr(t, "connect", w.fleet.Connect(ctx))

	// The subscription is refused, and tried again.
	waitFor(t, "the subscription to be refused twice", func() bool {
		return strings.Count(w.logText(), "subscription lost") >= 2
	})
	if !w.log.has("permission_denied") {
		t.Errorf("the log does not say why: %s", w.logText())
	}
	// The device carries on with what it can ask without a token, and learns
	// that its readings were refused when its stream closes.
	if stats := w.tick(t, 0); stats != (Stats{Reported: 1, Asked: 1}) {
		t.Errorf("the step: %+v", stats)
	}
	_, err := w.fleet.Flush()
	if connect.CodeOf(err) != connect.CodePermissionDenied || !strings.Contains(err.Error(), w.fixture.SiteA.NMI) {
		t.Errorf("flush = %v, want permission denied for the site", err)
	}
	if rows := w.readings(t, w.solar); len(rows) != 0 {
		t.Errorf("%d readings were stored", len(rows))
	}
}

func (w *world) logText() string {
	w.log.mu.Lock()
	defer w.log.mu.Unlock()
	return w.log.buf.String()
}

func TestBrokenStreamIsReplaced(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	noErr(t, "connect", w.fleet.Connect(ctx))

	// The first step opens the stream under a context that then ends.
	brief, cancel := context.WithCancel(ctx)
	if _, err := w.fleet.Tick(brief, at(0)); err != nil {
		t.Fatal(err)
	}
	cancel()
	if stats := w.tick(t, 1); stats != (Stats{Failed: 1}) {
		t.Errorf("the step on a dead stream: %+v", stats)
	}
	if !w.log.has("readings not sent") {
		t.Errorf("the log: %s", w.logText())
	}
	// The next step opens another.
	if stats := w.tick(t, 2); stats != (Stats{Reported: 1}) {
		t.Errorf("the step after: %+v", stats)
	}
	if n := w.flush(t); n != 1 {
		t.Errorf("stored %d readings from the new stream, want 1", n)
	}
}

// The clients below fail the calls a test names.

var errDown = connect.NewError(connect.CodeUnavailable, errors.New("the API is down"))

type noSites struct {
	doelabv1connect.SiteServiceClient
}

func (noSites) ListSites(context.Context, *connect.Request[doelabv1.ListSitesRequest]) (*connect.Response[doelabv1.ListSitesResponse], error) {
	return nil, errDown
}

type noDevices struct {
	doelabv1connect.DeviceServiceClient
}

func (noDevices) ListDevices(context.Context, *connect.Request[doelabv1.ListDevicesRequest]) (*connect.Response[doelabv1.ListDevicesResponse], error) {
	return nil, errDown
}

type noConfigs struct {
	doelabv1connect.EnvelopeConfigServiceClient
}

func (noConfigs) GetActiveEnvelopeConfig(context.Context, *connect.Request[doelabv1.GetActiveEnvelopeConfigRequest]) (*connect.Response[doelabv1.GetActiveEnvelopeConfigResponse], error) {
	return nil, errDown
}

type noEnvelopes struct {
	doelabv1connect.EnvelopeServiceClient
}

func (noEnvelopes) GetCurrentEnvelope(context.Context, *connect.Request[doelabv1.GetCurrentEnvelopeRequest]) (*connect.Response[doelabv1.GetCurrentEnvelopeResponse], error) {
	return nil, errDown
}

func (noEnvelopes) SubscribeEnvelopes(context.Context, *connect.Request[doelabv1.SubscribeEnvelopesRequest]) (*connect.ServerStreamForClient[doelabv1.SubscribeEnvelopesResponse], error) {
	return nil, errDown
}

func TestConnectFailures(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	// A second feeder, in a time zone that does not exist, and a third with
	// no devices.
	odd := repotest.NewFeeder("LV20")
	odd.Timezone = "Mars/Olympus_Mons"
	_, err := w.Store.CreateFeeder(ctx, odd)
	noErr(t, "odd feeder", err)
	_, err = w.Store.CreateFeeder(ctx, repotest.NewFeeder("LV30"))
	noErr(t, "empty feeder", err)

	fleet := func(code string) *Fleet {
		f := NewFleet(NewAPI(w.HTTP, w.URL), code, w.Tokens.DeviceToken, slog.New(slog.DiscardHandler))
		t.Cleanup(f.Close)
		return f
	}
	for name, tt := range map[string]struct {
		fleet *Fleet
		want  string
	}{
		"an unknown feeder":     {fleet("LV99"), "feeder LV99: not_found"},
		"an unknown time zone":  {fleet("LV20"), `time zone "Mars/Olympus_Mons"`},
		"a feeder of no device": {fleet("LV30"), "feeder LV30 has no devices"},
		"sites that fail": {func() *Fleet {
			f := fleet("LV10")
			f.API.Sites = noSites{}
			return f
		}(), "sites: unavailable"},
		"devices that fail": {func() *Fleet {
			f := fleet("LV10")
			f.API.Devices = noDevices{}
			return f
		}(), "devices: unavailable"},
	} {
		if err := tt.fleet.Connect(ctx); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v, want %q", name, err, tt.want)
		}
	}
}

func TestForecastFailures(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	noErr(t, "connect", w.fleet.Connect(ctx))

	// Two days on, the sites have no profile.
	if _, err := w.fleet.Tick(ctx, at(2*24*60)); connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "forecast: ") {
		t.Errorf("a step with no profile: %v", err)
	}
	w.fleet.API.Configs = noConfigs{}
	if _, err := w.fleet.Tick(ctx, at(0)); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Errorf("a step with no answer about the config: %v", err)
	}
}

func TestDeviceThatCannotAskUsesItsDefault(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	w.publish(t, 0, 600)
	w.fleet.API.Envelopes = noEnvelopes{}
	noErr(t, "connect", w.fleet.Connect(ctx))
	waitFor(t, "the subscription to fail", func() bool { return w.log.has("subscription lost") })

	// Every step asks again: no answer is not "none".
	for minute := range 2 {
		if stats := w.tick(t, minute); stats != (Stats{Reported: 1, Asked: 1}) {
			t.Errorf("minute %d: %+v", minute, stats)
		}
	}
	w.flush(t)
	if !w.log.has("no answer about the envelope") {
		t.Errorf("the log: %s", w.logText())
	}
	for _, r := range w.readings(t, w.solar) {
		if r.NetExportW != DefaultExportW {
			t.Errorf("exports %v W, want the default", r.NetExportW)
		}
	}
}

func TestRun(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	// A stream for every step, so a step's reading is stored when it ends.
	w.fleet.BatchesPerStream = 1
	noErr(t, "connect", w.fleet.Connect(ctx))

	// The fleet's timer: it tells the test how long the fleet would wait, and
	// fires when the test says so.
	waits, fire := make(chan time.Duration), make(chan time.Time)
	w.fleet.after = func(d time.Duration) <-chan time.Time {
		waits <- d
		return fire
	}
	running, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.fleet.Run(running, w.SimClock)
	}()

	// The next minute of feeder time is a second away at sixty times speed.
	if d := <-waits; d != time.Second {
		t.Errorf("waits %v for the first step, want 1s", d)
	}
	fire <- time.Time{}
	<-waits // the step is done, and the fleet waits for the next
	if !w.log.has("msg=step at=2012-10-01T00:01:00Z reported=1") {
		t.Errorf("the log: %s", w.logText())
	}

	// A step that fails is logged, and the fleet carries on.
	w.fleet.API.Configs, w.fleet.to = noConfigs{}, time.Time{}
	fire <- time.Time{}
	<-waits
	if !w.log.has(`msg="step failed" at=2012-10-01T00:01:00Z`) {
		t.Errorf("the log: %s", w.logText())
	}

	stop()
	<-done
	if rows := w.readings(t, w.solar); len(rows) != 1 || !rows[0].TS.Equal(at(1)) {
		t.Errorf("readings = %+v, want one for the first minute", rows)
	}
}

func TestClockSettings(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	anchor, speed, err := w.fleet.API.ClockSettings(ctx)
	if err != nil || !anchor.Equal(apitest.Anchor) || speed != apitest.Speed {
		t.Errorf("clock = %v, %v, %v", anchor, speed, err)
	}
	w.Close()
	if _, _, err := w.fleet.API.ClockSettings(ctx); err == nil {
		t.Error("the clock of an API that is gone")
	}
}

func TestFeederCodes(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	codes, err := w.fleet.API.FeederCodes(ctx)
	if err != nil || len(codes) != 1 || codes[0] != w.fleet.FeederCode {
		t.Errorf("feeder codes = %v, %v", codes, err)
	}
	w.Close()
	if _, err := w.fleet.API.FeederCodes(ctx); err == nil {
		t.Error("the feeders of an API that is gone")
	}
}

// The shares of several fleets are of all their devices, spread over the
// fleets in turn.
func TestMisbehaveAcrossFleets(t *testing.T) {
	t.Parallel()
	fleet := func(units int) *Fleet {
		f := &Fleet{Seed: 1}
		for range units {
			f.units = append(f.units, &unit{})
		}
		return f
	}
	counts := func(fleets []*Fleet) (out [][2]int) {
		for _, f := range fleets {
			rogues, flaky := f.Misbehaving()
			out = append(out, [2]int{rogues, flaky})
		}
		return out
	}
	same := func(got, want [][2]int) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	// 4 % of 76 devices is four of each: not one of each on every feeder.
	var demo []*Fleet
	for _, units := range []int{11, 4, 4, 3, 4, 4, 3, 6, 5, 4, 4, 3, 6, 5, 5, 5} {
		demo = append(demo, fleet(units))
	}
	Misbehave(demo, 0.04, 0.04)
	rogues, flaky := 0, 0
	for i, c := range counts(demo) {
		rogues, flaky = rogues+c[0], flaky+c[1]
		if c[0]+c[1] > 1 {
			t.Errorf("fleet %d has %d rogue and %d flaky devices: they are not spread", i, c[0], c[1])
		}
	}
	if rogues != 4 || flaky != 4 {
		t.Errorf("%d rogue and %d flaky devices of 76, want 4 and 4", rogues, flaky)
	}

	// A fleet with no room is passed over, and the pick stops when none has.
	small := []*Fleet{fleet(1), fleet(0), fleet(3)}
	Misbehave(small, 0.5, 1)
	if got := counts(small); !same(got, [][2]int{{1, 0}, {0, 0}, {1, 2}}) {
		t.Errorf("half rogue and all flaky of four devices = %v", got)
	}
	// None asked for: the pick before is undone.
	Misbehave(small, 0, 0)
	if got := counts(small); !same(got, [][2]int{{0, 0}, {0, 0}, {0, 0}}) {
		t.Errorf("no share = %v", got)
	}
	Misbehave(nil, 1, 1)
}
