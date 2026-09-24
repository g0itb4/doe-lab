package service_test

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/auth"
	"doelab/api/internal/domain"
	"doelab/api/internal/profile"
	"doelab/api/internal/repo/repotest"
	"doelab/api/internal/service"
)

// ops is a scene with the services that watch the fleet, on a clock the test
// moves, and three devices: solar and a battery at site A, an EV charger at
// site B.
type ops struct {
	*scene
	clock      *movedClock
	compliance *service.Compliance
	telemetry  *service.Telemetry
	backstops  *service.Backstops
	alerts     *service.Alerts
	envelopes  *service.Envelopes

	solar, battery, ev domain.Device
}

func newOps(t *testing.T) *ops {
	t.Helper()
	s := newScene(t)
	ctx := repotest.Ctx()
	o := &ops{scene: s, clock: &movedClock{now: repotest.Day}}
	o.compliance = service.NewCompliance(s.store, o.clock)
	o.telemetry = service.NewTelemetry(s.store, o.clock, o.compliance)
	o.backstops = service.NewBackstops(s.store, s.bus, o.clock)
	o.alerts = service.NewAlerts(s.store)
	o.envelopes = service.NewEnvelopes(s.store, s.bus, o.clock)

	device := func(site domain.Site, kind domain.DERType) domain.Device {
		d, err := s.store.CreateDevice(ctx, domain.Device{SiteID: site.ID, DERType: kind, RatedW: 5000})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	o.solar, o.battery, o.ev = device(s.f.SiteA, domain.DERSolar), device(s.f.SiteA, domain.DERBattery), device(s.f.SiteB, domain.DEREV)
	return o
}

// asDevice is a context that holds the device token of a site.
func asDevice(site domain.Site) context.Context {
	return auth.NewContext(context.Background(), auth.Actor{Scope: auth.ScopeDevice, NMI: site.NMI})
}

// at is an instant, seconds into the profile day.
func at(seconds int) time.Time {
	return repotest.Day.Add(time.Duration(seconds) * time.Second)
}

// limit gives site A an engine envelope for a half hour of the profile day.
func (o *ops) limit(t *testing.T, slot int, exportW float64) {
	t.Helper()
	if _, _, err := o.store.ReplaceEnvelopes(repotest.Ctx(), []domain.Envelope{repotest.Envelope(o.f.SiteA.ID, o.run.ID, slot, exportW)}); err != nil {
		t.Fatal(err)
	}
}

// report sends one reading of the solar device through the service.
func (o *ops) report(t *testing.T, seconds int, netExportW float64) {
	t.Helper()
	stored, err := o.telemetry.Ingest(asDevice(o.f.SiteA), o.f.SiteA.NMI, []domain.Reading{repotest.Reading(o.solar.ID, seconds, netExportW)})
	if err != nil || stored != 1 {
		t.Fatalf("reading at %d s: stored %d, %v", seconds, stored, err)
	}
}

// listAlerts returns the alerts of the feeder, newest first.
func (o *ops) listAlerts(t *testing.T, feederID uuid.UUID) []domain.Alert {
	t.Helper()
	alerts, _, err := o.alerts.List(repotest.Ctx(), feederID, service.AlertFilter{}, domain.Page{})
	if err != nil {
		t.Fatal(err)
	}
	return alerts
}

func TestBreachOpensAfterTheGraceAndResolves(t *testing.T) {
	t.Parallel()
	o := newOps(t)
	feeder := o.f.Feeder.ID
	o.limit(t, 0, 1000)

	// Inside the tolerance is inside the limit.
	o.report(t, 0, 1000+service.ExportToleranceW)
	// Above it, but not yet for the grace period of 60 s.
	o.report(t, 10, 2000)
	o.report(t, 40, 2500)
	o.report(t, 69, 2200)
	if alerts := o.listAlerts(t, feeder); len(alerts) != 0 {
		t.Fatalf("alerts before the grace has run: %+v", alerts)
	}

	// 60 s after it began: the alert opens, dated from the beginning, with
	// the worst reading so far.
	o.report(t, 70, 2200)
	alerts := o.listAlerts(t, feeder)
	if len(alerts) != 1 {
		t.Fatalf("alerts after the grace: %+v", alerts)
	}
	a := alerts[0]
	if a.Kind != domain.AlertConstraintBreach || a.Severity != domain.SeverityWarning || a.SiteID != o.f.SiteA.ID ||
		!a.OpenedAt.Equal(at(10)) || a.LimitW == nil || *a.LimitW != 1000 || a.PeakW == nil || *a.PeakW != 2500 ||
		a.ResolvedAt != nil || !strings.Contains(a.Detail, "1000 W") {
		t.Errorf("the alert = %+v", a)
	}

	// It gets worse: the same alert, a higher peak.
	o.report(t, 80, 3000)
	alerts = o.listAlerts(t, feeder)
	if len(alerts) != 1 || alerts[0].ID != a.ID || *alerts[0].PeakW != 3000 {
		t.Errorf("after a worse reading: %+v", alerts)
	}

	// Back inside the limit: resolved, then.
	o.report(t, 90, 900)
	alerts = o.listAlerts(t, feeder)
	if len(alerts) != 1 || alerts[0].ResolvedAt == nil || !alerts[0].ResolvedAt.Equal(at(90)) {
		t.Errorf("after a reading inside the limit: %+v", alerts)
	}

	// A new excess starts its own grace period.
	o.report(t, 100, 2000)
	o.report(t, 159, 2000)
	if alerts = o.listAlerts(t, feeder); len(alerts) != 1 {
		t.Errorf("a second excess opened an alert before its own grace: %+v", alerts)
	}
	o.report(t, 160, 2000)
	if alerts = o.listAlerts(t, feeder); len(alerts) != 2 || !alerts[0].OpenedAt.Equal(at(100)) {
		t.Errorf("the second alert: %+v", alerts)
	}

	// With no envelope in force there is no limit to break, however long.
	o.report(t, 9000, 9999)
	o.report(t, 9100, 9999)
	alerts = o.listAlerts(t, feeder)
	if len(alerts) != 2 || alerts[0].ResolvedAt == nil {
		t.Errorf("with no envelope: %+v", alerts)
	}
}

func TestBreachOfABackstopIsCritical(t *testing.T) {
	t.Parallel()
	o := newOps(t)
	if _, _, err := o.backstops.Trigger(repotest.Ctx(), domain.BackstopEvent{FeederID: o.f.Feeder.ID, Reason: "transformer fault"}, nil); err != nil {
		t.Fatal(err)
	}
	o.report(t, 0, 2000)
	o.report(t, 60, 2400)
	alerts := o.listAlerts(t, o.f.Feeder.ID)
	if len(alerts) != 1 || alerts[0].Severity != domain.SeverityCritical || *alerts[0].LimitW != 0 || *alerts[0].PeakW != 2400 {
		t.Errorf("alerts = %+v", alerts)
	}
}

func TestBreachThatMeetsABackstopTurnsCritical(t *testing.T) {
	t.Parallel()
	o := newOps(t)
	o.limit(t, 0, 1000)
	o.report(t, 0, 2000)
	o.report(t, 60, 2000)
	if alerts := o.listAlerts(t, o.f.Feeder.ID); len(alerts) != 1 || alerts[0].Severity != domain.SeverityWarning {
		t.Fatalf("alerts = %+v", alerts)
	}
	if _, _, err := o.backstops.Trigger(repotest.Ctx(), domain.BackstopEvent{FeederID: o.f.Feeder.ID, Reason: "transformer fault"}, nil); err != nil {
		t.Fatal(err)
	}
	// The same alert, now of a device that exports through a backstop.
	o.report(t, 120, 2000)
	alerts := o.listAlerts(t, o.f.Feeder.ID)
	if len(alerts) != 1 || alerts[0].Severity != domain.SeverityCritical || *alerts[0].LimitW != 1000 || !alerts[0].OpenedAt.Equal(at(0)) {
		t.Errorf("alerts = %+v", alerts)
	}
}

func TestSweepFindsSilentDevices(t *testing.T) {
	t.Parallel()
	o := newOps(t)
	ctx := repotest.Ctx()
	// A second feeder with no config: the default thresholds apply to it.
	other := repotest.Seed(t, o.store, "LV20", 11)
	otherDevice, err := o.store.CreateDevice(ctx, domain.Device{SiteID: other.SiteA.ID, DERType: domain.DERSolar, RatedW: 5000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.store.InsertReadings(ctx, []domain.Reading{repotest.Reading(otherDevice.ID, 0, 0)}); err != nil {
		t.Fatal(err)
	}
	// The solar device reports once. The battery and the charger never do.
	o.report(t, 0, 500)

	// 300 s of silence is the limit, not past it.
	o.clock.set(at(300))
	if opened, err := o.compliance.Sweep(ctx); err != nil || opened != 0 {
		t.Fatalf("at the limit: opened %d, %v", opened, err)
	}
	o.clock.set(at(301))
	if opened, err := o.compliance.Sweep(ctx); err != nil || opened != 2 {
		t.Fatalf("past the limit: opened %d, %v, want one on each feeder", opened, err)
	}
	alerts := o.listAlerts(t, o.f.Feeder.ID)
	if len(alerts) != 1 {
		t.Fatalf("alerts = %+v", alerts)
	}
	a := alerts[0]
	if a.Kind != domain.AlertDeviceOffline || a.Severity != domain.SeverityInfo || a.DeviceID == nil || *a.DeviceID != o.solar.ID ||
		!a.OpenedAt.Equal(at(300)) || !strings.Contains(a.Detail, "solar") {
		t.Errorf("the alert = %+v", a)
	}
	if got := o.listAlerts(t, other.Feeder.ID); len(got) != 1 || *got[0].DeviceID != otherDevice.ID {
		t.Errorf("the other feeder's alerts = %+v", got)
	}

	// The next sweep finds the same silence, and opens nothing new.
	o.clock.set(at(360))
	if opened, err := o.compliance.Sweep(ctx); err != nil || opened != 0 {
		t.Errorf("a second sweep: opened %d, %v", opened, err)
	}

	// The device speaks: the alert is resolved.
	o.report(t, 400, 500)
	alerts = o.listAlerts(t, o.f.Feeder.ID)
	if len(alerts) != 1 || alerts[0].ResolvedAt == nil || !alerts[0].ResolvedAt.Equal(at(400)) {
		t.Errorf("after the device reported: %+v", alerts)
	}

	o.clock.set(at(1000))
	for _, step := range []string{"ListFeeders", "GetActiveEnvelopeConfig", "ListDeviceStates", "OpenAlert"} {
		o.store.failAt(step)
		if _, err := o.compliance.Sweep(ctx); !errors.Is(err, errDown) {
			t.Errorf("with %s failing: %v", step, err)
		}
	}
	o.store.failAt("")
	if got := o.listAlerts(t, o.f.Feeder.ID); len(got) != 1 {
		t.Errorf("a failed sweep left an alert behind: %+v", got)
	}
}

func TestIngest(t *testing.T) {
	t.Parallel()
	o := newOps(t)
	device := asDevice(o.f.SiteA)
	nmi := o.f.SiteA.NMI

	// The token is for one site.
	if _, err := o.telemetry.Ingest(repotest.Ctx(), nmi, nil); !errors.Is(err, domain.ErrPermissionDenied) {
		t.Errorf("the operator's token: %v", err)
	}
	if _, err := o.telemetry.Ingest(asDevice(o.f.SiteB), nmi, nil); !errors.Is(err, domain.ErrPermissionDenied) {
		t.Errorf("another site's token: %v", err)
	}
	if _, err := o.telemetry.Ingest(context.Background(), nmi, nil); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("no token: %v", err)
	}
	if stored, err := o.telemetry.Ingest(device, nmi, nil); err != nil || stored != 0 {
		t.Errorf("an empty batch: %d, %v", stored, err)
	}
	ghost := domain.Site{NMI: repotest.NMI(t, 99)}
	if _, err := o.telemetry.Ingest(asDevice(ghost), ghost.NMI, []domain.Reading{repotest.Reading(o.solar.ID, 0, 0)}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a site that does not exist: %v", err)
	}

	// A device of another site, in a batch that is otherwise good: nothing
	// of the batch is stored.
	mixed := []domain.Reading{repotest.Reading(o.solar.ID, 0, 100), repotest.Reading(o.ev.ID, 0, 100)}
	if _, err := o.telemetry.Ingest(device, nmi, mixed); !errors.Is(err, domain.ErrPermissionDenied) {
		t.Errorf("a reading from another site's device: %v", err)
	}
	if rows, _, err := o.telemetry.ListReadings(device, o.solar.ID, at(0), at(3600), domain.Page{}); err != nil || len(rows) != 0 {
		t.Errorf("after the refused batch: %d readings, %v", len(rows), err)
	}

	// Both devices of the site in one batch; then the same batch again.
	batch := []domain.Reading{repotest.Reading(o.solar.ID, 0, 100), repotest.Reading(o.battery.ID, 0, 100), repotest.Reading(o.solar.ID, 5, 120)}
	if stored, err := o.telemetry.Ingest(device, nmi, batch); err != nil || stored != 3 {
		t.Errorf("a batch of three: %d, %v", stored, err)
	}
	if stored, err := o.telemetry.Ingest(device, nmi, batch); err != nil || stored != 0 {
		t.Errorf("the same batch again: %d, %v", stored, err)
	}
	rows, next, err := o.telemetry.ListReadings(device, o.solar.ID, at(0), at(3600), domain.Page{})
	if err != nil || len(rows) != 2 || next != "" || rows[1].NetExportW != 120 || rows[0].SiteID != o.f.SiteA.ID {
		t.Errorf("the solar device's readings = %+v, %q, %v", rows, next, err)
	}
	if _, _, err := o.telemetry.ListReadings(device, uuid.New(), at(0), at(3600), domain.Page{}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("readings of an unknown device: %v", err)
	}

	// The site is judged by the newest reading of a batch, wherever it is
	// in the batch.
	o.limit(t, 0, 1000)
	outOfOrder := []domain.Reading{repotest.Reading(o.solar.ID, 30, 5000), repotest.Reading(o.solar.ID, 20, 0)}
	if _, err := o.telemetry.Ingest(device, nmi, outOfOrder); err != nil {
		t.Fatal(err)
	}
	o.report(t, 91, 5000)
	if alerts := o.listAlerts(t, o.f.Feeder.ID); len(alerts) != 1 || !alerts[0].OpenedAt.Equal(at(30)) {
		t.Errorf("alerts = %+v, want one from the reading at 30 s", alerts)
	}
}

func TestIngestFailuresStoreNothing(t *testing.T) {
	t.Parallel()
	// The second ResolveAlert is the one for a site inside its limit; an
	// OpenAlert needs a site that has been over for the grace period.
	for _, step := range []string{"ListDevices", "InsertReadings", "GetActiveEnvelopeConfig", "ResolveAlert", "ResolveAlert#2", "GetCurrentEnvelope", "OpenAlert"} {
		t.Run(step, func(t *testing.T) {
			t.Parallel()
			o := newOps(t)
			o.limit(t, 0, 1000)
			o.report(t, 0, 5000)

			netW := 0.0
			if step == "OpenAlert" {
				netW = 5000
			}
			o.store.failAt(step)
			_, err := o.telemetry.Ingest(asDevice(o.f.SiteA), o.f.SiteA.NMI, []domain.Reading{repotest.Reading(o.solar.ID, 60, netW)})
			if !errors.Is(err, errDown) {
				t.Fatalf("error = %v, want the store's", err)
			}
			o.store.failAt("")
			rows, _, err := o.telemetry.ListReadings(context.Background(), o.solar.ID, at(0), at(3600), domain.Page{})
			if err != nil || len(rows) != 1 {
				t.Errorf("%d readings stored, %v; want the first one only", len(rows), err)
			}
			if alerts := o.listAlerts(t, o.f.Feeder.ID); len(alerts) != 0 {
				t.Errorf("alerts = %+v", alerts)
			}
		})
	}
}

func TestFleetSummary(t *testing.T) {
	t.Parallel()
	o := newOps(t)
	ctx := repotest.Ctx()
	feeder := o.f.Feeder.ID
	// A second device at site B that never reports.
	if _, err := o.store.CreateDevice(ctx, domain.Device{SiteID: o.f.SiteB.ID, DERType: domain.DERBattery, RatedW: 5000}); err != nil {
		t.Fatal(err)
	}

	// Nothing has reported, and the engine has published nothing.
	empty, err := o.telemetry.Summary(ctx, feeder)
	if err != nil || empty.EnrolledSites != 1 || empty.Devices != 4 || empty.DevicesOnline != 0 || empty.ReportingSites != 0 ||
		empty.ExportLimitW != 0 || empty.BackstopEventID != nil || empty.LatestRun == nil || empty.LatestRun.ID != o.run.ID ||
		empty.FeederID != feeder || !empty.At.Equal(at(0)) {
		t.Errorf("the quiet fleet = %+v, %v", empty, err)
	}

	// Site A's devices share a meter: the site's flow is what the device
	// heard last says. Site B imports.
	if _, err := o.store.InsertReadings(ctx, []domain.Reading{
		repotest.Reading(o.battery.ID, 0, 3000), repotest.Reading(o.solar.ID, 30, 3500), repotest.Reading(o.ev.ID, 10, -2000),
	}); err != nil {
		t.Fatal(err)
	}
	// The envelope in force, and the next one, which is not in force yet.
	o.limit(t, 0, 3000)
	o.limit(t, 1, 4000)
	if _, _, err := o.store.OpenAlert(ctx, domain.Alert{
		SiteID: o.f.SiteA.ID, FeederID: o.f.Feeder.ID, Kind: domain.AlertConstraintBreach, Severity: domain.SeverityWarning,
		OpenedAt: at(0), LimitW: repotest.Ptr(3000.0), PeakW: repotest.Ptr(3500.0),
	}); err != nil {
		t.Fatal(err)
	}

	o.clock.set(at(60))
	got, err := o.telemetry.Summary(ctx, feeder)
	if err != nil {
		t.Fatal(err)
	}
	if got.EnrolledSites != 1 || got.Devices != 4 || got.DevicesOnline != 3 || got.ReportingSites != 2 ||
		got.ExportW != 3500 || got.ImportW != 2000 || got.ExportLimitW != 3000 || got.SitesOverLimit != 1 || got.OpenAlerts != 1 {
		t.Errorf("the summary = %+v", got)
	}

	// A site just inside the tolerance is not over.
	if _, err := o.store.InsertReadings(ctx, []domain.Reading{repotest.Reading(o.solar.ID, 45, 3050)}); err != nil {
		t.Fatal(err)
	}
	if got, err = o.telemetry.Summary(ctx, feeder); err != nil || got.SitesOverLimit != 0 || got.ExportW != 3050 {
		t.Errorf("inside the tolerance: %+v, %v", got, err)
	}

	event, _, err := o.backstops.Trigger(ctx, domain.BackstopEvent{FeederID: feeder, Reason: "test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err = o.telemetry.Summary(ctx, feeder); err != nil || got.BackstopEventID == nil || *got.BackstopEventID != event.ID ||
		got.ExportLimitW != 0 || got.SitesOverLimit != 1 {
		t.Errorf("under a backstop: %+v, %v", got, err)
	}

	// Ten minutes on, with no reading since: nothing is online.
	o.clock.set(at(600))
	if got, err = o.telemetry.Summary(ctx, feeder); err != nil || got.DevicesOnline != 0 || got.ReportingSites != 0 || got.ExportW != 0 || got.SitesOverLimit != 0 {
		t.Errorf("after ten minutes of silence: %+v, %v", got, err)
	}

	// A feeder with no config and no run.
	bare := repotest.Seed(t, o.store, "LV20", 11)
	if got, err = o.telemetry.Summary(ctx, bare.Feeder.ID); err != nil || got.LatestRun != nil || got.EnrolledSites != 1 {
		t.Errorf("a bare feeder: %+v, %v", got, err)
	}
	if _, err := o.telemetry.Summary(ctx, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("an unknown feeder: %v", err)
	}
	for _, step := range []string{"GetActiveEnvelopeConfig", "ListAllSites", "ListDeviceStates", "ListActiveEnvelopes", "CountOpenAlerts", "GetActiveBackstopEvent", "ListEnvelopeRuns"} {
		o.store.failAt(step)
		if _, err := o.telemetry.Summary(ctx, feeder); !errors.Is(err, errDown) {
			t.Errorf("with %s failing: %v", step, err)
		}
	}
}

func TestWatchFleet(t *testing.T) {
	t.Parallel()
	o := newOps(t)
	o.telemetry.WatchEvery = time.Millisecond

	// It sends at once and then again and again, until the client leaves.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sent := 0
	err := o.telemetry.Watch(ctx, o.f.Feeder.ID, func(s service.FleetSummary) error {
		if s.FeederID != o.f.Feeder.ID {
			t.Errorf("a summary of feeder %s", s.FeederID)
		}
		if sent++; sent == 3 {
			cancel()
		}
		return nil
	})
	if err != nil || sent != 3 {
		t.Errorf("sent %d summaries, %v", sent, err)
	}

	gone := errors.New("broken pipe")
	if err := o.telemetry.Watch(context.Background(), o.f.Feeder.ID, func(service.FleetSummary) error { return gone }); !errors.Is(err, gone) {
		t.Errorf("with a failing send: %v", err)
	}
	err = o.telemetry.Watch(context.Background(), uuid.New(), func(service.FleetSummary) error { return nil })
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("an unknown feeder: %v", err)
	}
}

func TestFeederSeries(t *testing.T) {
	t.Parallel()
	o := newOps(t)
	ctx := repotest.Ctx()
	feeder := o.f.Feeder.ID

	// The engine covered the half hours 0, 2 and 3 of the day; not 1.
	if err := o.store.CreateEnvelopeRunIntervals(ctx, []domain.EnvelopeRunInterval{
		repotest.Interval(o.run.ID, feeder, 0, 10000), repotest.Interval(o.run.ID, feeder, 2, 12000), repotest.Interval(o.run.ID, feeder, 3, 13000),
	}); err != nil {
		t.Fatal(err)
	}
	// The fleet reported in half hours 0, 1 and 2.
	if _, err := o.store.InsertReadings(ctx, []domain.Reading{
		repotest.Reading(o.solar.ID, 0, 3000), repotest.Reading(o.ev.ID, 0, -1000), // minute 0: 3000 out, 1000 in
		repotest.Reading(o.solar.ID, 60, 1000),      // minute 1: 1000 out
		repotest.Reading(o.solar.ID, 1800, 9000),    // half hour 1, which has no interval
		repotest.Reading(o.solar.ID, 3600, 500),     // half hour 2
		repotest.Reading(o.ev.ID, 3600+120, -700),   // half hour 2, two minutes on
		repotest.Reading(o.solar.ID, 3600+120, 300), // the same minute
	}); err != nil {
		t.Fatal(err)
	}

	series, err := o.telemetry.Series(ctx, feeder, at(0), at(4*3600))
	if err != nil {
		t.Fatal(err)
	}
	if series.VMinPU != 0.94 || series.VMaxPU != 1.10 || series.TransformerKVA != 100 || series.StaticLimitW != 5000 {
		t.Errorf("the limits = %+v", series)
	}
	if len(series.Points) != 3 {
		t.Fatalf("%d points, want 3", len(series.Points))
	}
	first, third, fourth := series.Points[0], series.Points[1], series.Points[2]
	if first.ForecastNetLoadW != 10000 || first.MeasuredExportW == nil || *first.MeasuredExportW != 2000 || *first.MeasuredImportW != 500 {
		t.Errorf("half hour 0 = %+v, want 2000 W out and 500 W in over its two minutes", first)
	}
	if third.ForecastNetLoadW != 12000 || third.MeasuredExportW == nil || *third.MeasuredExportW != 400 || *third.MeasuredImportW != 350 {
		t.Errorf("half hour 2 = %+v, want 400 W out and 350 W in over its two minutes", third)
	}
	if fourth.MeasuredExportW != nil || fourth.MeasuredImportW != nil {
		t.Errorf("half hour 3 = %+v, want nothing measured", fourth)
	}

	// A feeder with no config has no limits to draw; that is not an error.
	bare := repotest.Seed(t, o.store, "LV20", 11)
	if series, err = o.telemetry.Series(ctx, bare.Feeder.ID, at(0), at(3600)); err != nil || len(series.Points) != 0 || series.VMaxPU != 0 || series.TransformerKVA != 100 {
		t.Errorf("a bare feeder: %+v, %v", series, err)
	}

	if _, err := o.telemetry.Series(ctx, feeder, at(0), at(0)); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("an empty range: %v", err)
	}
	if _, err := o.telemetry.Series(ctx, feeder, at(0), at(8*24*3600)); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("eight days: %v", err)
	}
	if _, err := o.telemetry.Series(ctx, uuid.New(), at(0), at(3600)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("an unknown feeder: %v", err)
	}
	for _, step := range []string{"GetActiveEnvelopeConfig", "ListFeederIntervals", "ListFleetSeries"} {
		o.store.failAt(step)
		if _, err := o.telemetry.Series(ctx, feeder, at(0), at(3600)); !errors.Is(err, errDown) {
			t.Errorf("with %s failing: %v", step, err)
		}
	}
}

// sydney is the zone of the fixture feeder.
func sydney(t *testing.T) *time.Location {
	t.Helper()
	zone, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		t.Fatal(err)
	}
	return zone
}

// localDay is the local calendar day of the fixture feeder that holds the
// profile day's first instant: 10:00 in Sydney on 1 October.
func localDay() (from, to time.Time) {
	from = repotest.Day.Add(-10 * time.Hour)
	return from, from.Add(24 * time.Hour)
}

// seedProfiles gives a site a profile for the local day: a flat load, and for
// the half hours given, counted from the profile day's first instant, a load
// and a PV output.
func (o *ops) seedProfiles(t *testing.T, site domain.Site, flatLoadW float64, special map[int][2]float64) {
	t.Helper()
	year := profile.Year{Start: profile.DefaultYearStart, Location: sydney(t)}
	from, _ := localDay()
	rows := make([]domain.SiteProfile, 48)
	for i := range rows {
		rows[i] = domain.SiteProfile{TS: year.At(from.Add(time.Duration(i) * 30 * time.Minute)).UTC(), LoadW: flatLoadW}
		if v, ok := special[i-20]; ok {
			// Part of the load is controlled load: a forecast counts both.
			rows[i].LoadW, rows[i].ControlledLoadW, rows[i].PVW = v[0]*0.6, v[0]*0.4, v[1]
		}
	}
	if err := o.store.ReplaceSiteProfiles(repotest.Ctx(), site.ID, rows); err != nil {
		t.Fatal(err)
	}
}

// useConfig makes a new version of the feeder's config the active one.
func (o *ops) useConfig(t *testing.T, change func(*domain.EnvelopeConfig)) {
	t.Helper()
	c := repotest.Config(o.f.Feeder.ID)
	change(&c)
	if _, err := service.NewEnvelopeConfigs(o.store).Create(repotest.Ctx(), c); err != nil {
		t.Fatal(err)
	}
}

func TestSiteSeries(t *testing.T) {
	t.Parallel()
	o := newOps(t)
	ctx := repotest.Ctx()
	site := o.f.SiteA
	o.seedProfiles(t, site, 500, map[int][2]float64{0: {1000, 2000}, 2: {0, 3000}})
	if _, err := o.store.InsertReadings(ctx, []domain.Reading{
		repotest.Reading(o.solar.ID, 0, 1000), repotest.Reading(o.solar.ID, 30, 2000), repotest.Reading(o.solar.ID, 60, 400),
	}); err != nil {
		t.Fatal(err)
	}

	// With the first config: PV as recorded.
	series, err := o.telemetry.SiteSeries(ctx, site.ID, at(0), at(90*60))
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Power) != 2 || series.Power[0].AvgNetExportW != 1500 || series.Power[0].MaxNetExportW != 2000 || series.Power[1].AvgNetExportW != 400 {
		t.Errorf("power = %+v", series.Power)
	}
	if len(series.Forecast) != 3 {
		t.Fatalf("forecast = %+v, want three half hours", series.Forecast)
	}
	f := series.Forecast
	if !f[0].TS.Equal(at(0)) || f[0].LoadW != 1000 || f[0].PVW != 2000 || f[1].LoadW != 500 || f[1].PVW != 0 || f[2].LoadW != 0 || f[2].PVW != 3000 {
		t.Errorf("forecast = %+v", f)
	}

	// The active config scales PV.
	o.useConfig(t, func(c *domain.EnvelopeConfig) { c.PVScale = 2 })
	// A range that begins inside a half hour takes that half hour.
	series, err = o.telemetry.SiteSeries(ctx, site.ID, at(10*60), at(20*60))
	if err != nil || len(series.Forecast) != 1 || series.Forecast[0].PVW != 4000 || len(series.Power) != 0 {
		t.Errorf("with PV scaled by 2: %+v, %v", series, err)
	}

	// A site with no profile has telemetry and no forecast; a feeder with no
	// config scales nothing.
	bare := repotest.Seed(t, o.store, "LV20", 11)
	if series, err = o.telemetry.SiteSeries(ctx, bare.SiteA.ID, at(0), at(3600)); err != nil || len(series.Forecast) != 0 {
		t.Errorf("a site with no profile: %+v, %v", series, err)
	}

	if _, err := o.telemetry.SiteSeries(ctx, site.ID, at(60), at(0)); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("a backwards range: %v", err)
	}
	if _, err := o.telemetry.SiteSeries(ctx, uuid.New(), at(0), at(3600)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("an unknown site: %v", err)
	}
	for _, step := range []string{"ListSitePower", "GetFeeder", "GetActiveEnvelopeConfig", "ListSiteProfiles"} {
		o.store.failAt(step)
		if _, err := o.telemetry.SiteSeries(ctx, site.ID, at(0), at(3600)); !errors.Is(err, errDown) {
			t.Errorf("with %s failing: %v", step, err)
		}
	}
	o.store.failAt("")

	odd := oddZone(t, o)
	if _, err := o.telemetry.SiteSeries(ctx, odd.ID, at(0), at(3600)); err == nil || !strings.Contains(err.Error(), "Mars/Olympus_Mons") {
		t.Errorf("an unknown zone: %v", err)
	}
}

// oddZone makes a feeder whose zone is not a zone, with one site, and returns
// the site.
func oddZone(t *testing.T, o *ops) domain.Site {
	t.Helper()
	ctx := repotest.Ctx()
	feeder := repotest.NewFeeder("ODD")
	feeder.Timezone = "Mars/Olympus_Mons"
	feeder, err := o.store.CreateFeeder(ctx, feeder)
	if err != nil {
		t.Fatal(err)
	}
	node, err := o.store.CreateFeederNode(ctx, domain.FeederNode{FeederID: feeder.ID, Name: "root"})
	if err != nil {
		t.Fatal(err)
	}
	site, err := o.store.CreateSite(ctx, domain.Site{NMI: repotest.NMI(t, 21), FeederID: feeder.ID, NodeID: node.ID, Name: "Ld1_LOAD_A", Phase: 1})
	if err != nil {
		t.Fatal(err)
	}
	return site
}

func near(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestDailyReport(t *testing.T) {
	t.Parallel()
	o := newOps(t)
	ctx := repotest.Ctx()
	feeder := o.f.Feeder.ID
	a, b := o.f.SiteA, o.f.SiteB
	from, to := localDay()

	// PV counts double, and the fixed limit is 1500 W.
	o.useConfig(t, func(c *domain.EnvelopeConfig) { c.PVScale, c.StaticLimitW = 2, 1500 })
	// Site A, by the half hour from 10:00 local:
	//
	//	         load    PV ×2   potential  envelope  exports  fixed 1500 W
	//	10:00    1000     4000        3000      2000     2000          1500
	//	10:30    1500     1000           0      5000        0             0
	//	11:00       0     6000        6000      4000     4000          1500
	//	                              ────                ────          ────
	//	                              9000                6000          3000   W for half an hour
	//	                               4.5                 3.0           1.5   kWh
	o.seedProfiles(t, a, 500, map[int][2]float64{0: {1000, 2000}, 1: {1500, 500}, 2: {0, 3000}})
	o.seedProfiles(t, b, 300, nil)
	o.limit(t, 0, 2000)
	o.limit(t, 1, 5000)
	o.limit(t, 2, 4000)
	// Site B is not enrolled: an envelope for it counts for nothing.
	if _, _, err := o.store.ReplaceEnvelopes(ctx, []domain.Envelope{repotest.Envelope(b.ID, o.run.ID, 0, 100)}); err != nil {
		t.Fatal(err)
	}

	// The fixed limit would have broken a network limit in two of three
	// half hours.
	quiet := repotest.Interval(o.run.ID, feeder, 1, 11000)
	quiet.StaticBinding, quiet.StaticBindingElement = domain.BindingNone, ""
	if err := o.store.CreateEnvelopeRunIntervals(ctx, []domain.EnvelopeRunInterval{
		repotest.Interval(o.run.ID, feeder, 0, 10000), quiet, repotest.Interval(o.run.ID, feeder, 2, 12000),
	}); err != nil {
		t.Fatal(err)
	}

	// One breach and one offline alert in the day; a breach the day before
	// and one the day after.
	breach := func(opened time.Time, resolve bool) {
		t.Helper()
		if _, _, err := o.store.OpenAlert(ctx, domain.Alert{
			SiteID: a.ID, FeederID: feeder, Kind: domain.AlertConstraintBreach, Severity: domain.SeverityWarning,
			OpenedAt: opened, LimitW: repotest.Ptr(1000.0), PeakW: repotest.Ptr(2000.0),
		}); err != nil {
			t.Fatal(err)
		}
		if resolve {
			if _, err := o.store.ResolveAlert(ctx, a.ID, domain.AlertConstraintBreach, opened.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
		}
	}
	breach(from.Add(-time.Hour), true)
	breach(from.Add(11*time.Hour), true)
	breach(to, false)
	if _, _, err := o.store.OpenAlert(ctx, domain.Alert{
		SiteID: a.ID, FeederID: feeder, DeviceID: &o.solar.ID, Kind: domain.AlertDeviceOffline, Severity: domain.SeverityInfo, OpenedAt: from.Add(12 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	// One backstop in the day, one the day before, one the day after.
	for _, triggered := range []time.Time{from.Add(-time.Hour), from.Add(13 * time.Hour), to.Add(time.Hour)} {
		event, err := o.store.CreateBackstopEvent(ctx, domain.BackstopEvent{
			FeederID: feeder, Reason: "test", TriggeredBy: "operator", TriggeredAt: triggered,
		}, []uuid.UUID{a.ID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := o.store.ClearBackstopEvent(ctx, event.ID, "operator", triggered.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}

	// Any instant of the day names the day.
	for _, instant := range []time.Time{from, repotest.Day, to.Add(-time.Second)} {
		r, err := o.telemetry.Report(ctx, feeder, instant)
		if err != nil {
			t.Fatal(err)
		}
		if !r.From.Equal(from) || !r.To.Equal(to) || r.EnrolledSites != 1 || r.Intervals != 3 || r.StaticLimitW != 1500 {
			t.Errorf("the day = %+v", r)
		}
		if !near(r.PotentialExportKWh, 4.5) || !near(r.EnvelopeExportKWh, 3.0) || !near(r.EnvelopeCurtailedKWh, 1.5) ||
			!near(r.StaticExportKWh, 1.5) || !near(r.StaticCurtailedKWh, 3.0) {
			t.Errorf("the energy = %+v", r)
		}
		if r.StaticViolationIntervals != 2 || r.ConstraintBreaches != 1 || r.DeviceOfflineAlerts != 1 || r.BackstopEvents != 1 {
			t.Errorf("the counts = %+v", r)
		}
	}

	// The day after has an alert and a backstop, and no envelope.
	next, err := o.telemetry.Report(ctx, feeder, to)
	if err != nil || next.Intervals != 0 || next.PotentialExportKWh != 0 || next.ConstraintBreaches != 1 || next.BackstopEvents != 1 ||
		next.DeviceOfflineAlerts != 0 || next.StaticViolationIntervals != 0 {
		t.Errorf("the day after = %+v, %v", next, err)
	}

	for _, step := range []string{"GetActiveEnvelopeConfig", "ListAllSites", "ListFeederEnvelopes", "ListFeederProfiles", "ListFeederIntervals", "ListAlerts", "ListBackstopEvents"} {
		o.store.failAt(step)
		if _, err := o.telemetry.Report(ctx, feeder, repotest.Day); !errors.Is(err, errDown) {
			t.Errorf("with %s failing: %v", step, err)
		}
	}
	o.store.failAt("")

	bare := repotest.Seed(t, o.store, "LV20", 11)
	if _, err := o.telemetry.Report(ctx, bare.Feeder.ID, repotest.Day); !errors.Is(err, domain.ErrFailedPrecondition) {
		t.Errorf("a feeder with no config: %v", err)
	}
	if _, err := o.telemetry.Report(ctx, uuid.New(), repotest.Day); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("an unknown feeder: %v", err)
	}
	odd := oddZone(t, o)
	if _, err := o.telemetry.Report(ctx, odd.FeederID, repotest.Day); err == nil || !strings.Contains(err.Error(), "Mars/Olympus_Mons") {
		t.Errorf("an unknown zone: %v", err)
	}
}

func TestAlerts(t *testing.T) {
	t.Parallel()
	o := newOps(t)
	ctx := repotest.Ctx()
	opened, _, err := o.store.OpenAlert(ctx, domain.Alert{
		SiteID: o.f.SiteA.ID, FeederID: o.f.Feeder.ID, Kind: domain.AlertConstraintBreach, Severity: domain.SeverityWarning,
		OpenedAt: at(0), LimitW: repotest.Ptr(1000.0), PeakW: repotest.Ptr(2000.0),
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := o.alerts.Get(ctx, opened.ID)
	if err != nil || got.ID != opened.ID || got.AcknowledgedAt != nil {
		t.Errorf("Get = %+v, %v", got, err)
	}
	seen, err := o.alerts.Acknowledge(ctx, opened.ID)
	if err != nil || seen.AcknowledgedAt == nil || seen.AcknowledgedBy == nil || *seen.AcknowledgedBy != "operator" {
		t.Errorf("Acknowledge = %+v, %v", seen, err)
	}
	again, err := o.alerts.Acknowledge(ctx, opened.ID)
	if err != nil || !again.AcknowledgedAt.Equal(*seen.AcknowledgedAt) {
		t.Errorf("Acknowledge again = %+v, %v", again, err)
	}
	offline := domain.AlertDeviceOffline
	if rows, _, err := o.alerts.List(ctx, o.f.Feeder.ID, service.AlertFilter{Kind: &offline}, domain.Page{}); err != nil || len(rows) != 0 {
		t.Errorf("offline alerts = %+v, %v", rows, err)
	}

	if _, err := o.alerts.Get(ctx, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("an unknown alert: %v", err)
	}
	if _, err := o.alerts.Acknowledge(ctx, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("acknowledging an unknown alert: %v", err)
	}
	if _, _, err := o.alerts.List(ctx, uuid.New(), service.AlertFilter{}, domain.Page{}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("alerts of an unknown feeder: %v", err)
	}
}

// backstopEnvelopes returns the active backstop envelopes of a site that end
// after an instant, in time order.
func (o *ops) backstopEnvelopes(t *testing.T, siteID uuid.UUID, after time.Time) []domain.Envelope {
	t.Helper()
	active, err := o.store.ListActiveEnvelopes(repotest.Ctx(), []uuid.UUID{siteID}, after)
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.Envelope
	for _, e := range active {
		if e.Source == domain.SourceBackstop {
			out = append(out, e)
		}
	}
	return out
}

func TestBackstopTakesOverAndGivesBack(t *testing.T) {
	t.Parallel()
	o := newOps(t)
	ctx := repotest.Ctx()
	feeder, a, b := o.f.Feeder.ID, o.f.SiteA, o.f.SiteB

	// The engine has published six half hours for site A, with an import
	// limit that is not the site's cap.
	var engine []domain.Envelope
	for slot := range 6 {
		e := repotest.Envelope(a.ID, o.run.ID, slot, 3000)
		e.ImportLimitW = 6000
		engine = append(engine, e)
	}
	if _, _, err := o.store.ReplaceEnvelopes(ctx, engine); err != nil {
		t.Fatal(err)
	}
	signal, cancel := o.bus.Subscribe(a.ID)
	defer cancel()

	// Ten minutes into the day, the operator pulls the backstop.
	o.clock.set(at(600))
	event, sites, err := o.backstops.Trigger(ctx, domain.BackstopEvent{FeederID: feeder, Reason: "transformer fault", ExportLimitW: 500}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if event.TriggeredBy != "operator" || !event.TriggeredAt.Equal(at(600)) || event.ClearedAt != nil || event.ExportLimitW != 500 ||
		len(sites) != 1 || sites[0] != a.ID {
		t.Errorf("the event = %+v over %v, want the one enrolled site", event, sites)
	}
	select {
	case <-signal:
	default:
		t.Error("the site's subscribers were not told")
	}

	// It holds from the half hour in force to the horizon: 48 half hours.
	held := o.backstopEnvelopes(t, a.ID, at(600))
	if len(held) != 48 || !held[0].ValidFrom.Equal(at(0)) || !held[47].ValidFrom.Equal(at(47*1800)) {
		t.Fatalf("%d backstop envelopes", len(held))
	}
	for i, e := range held {
		// Import keeps the engine's limit where there was one, and the
		// site's cap where there was none.
		wantImport := 7000.0
		if i < 6 {
			wantImport = 6000
		}
		if e.ExportLimitW != 500 || e.ImportLimitW != wantImport || e.BackstopEventID == nil || *e.BackstopEventID != event.ID || e.EnvelopeRunID != nil {
			t.Fatalf("backstop envelope %d = %+v", i, e)
		}
	}
	current, err := o.envelopes.Current(ctx, a.ID, time.Time{})
	if err != nil || current == nil || current.Source != domain.SourceBackstop || current.ExportLimitW != 500 {
		t.Errorf("the envelope in force = %+v, %v", current, err)
	}

	got, covered, err := o.backstops.Get(ctx, event.ID)
	if err != nil || got.ID != event.ID || len(covered) != 1 || covered[0] != a.ID {
		t.Errorf("Get = %+v over %v, %v", got, covered, err)
	}
	listed, _, err := o.backstops.List(ctx, feeder, domain.Page{})
	if err != nil || len(listed) != 1 || listed[0].ID != event.ID {
		t.Errorf("List = %+v, %v", listed, err)
	}

	// One at a time.
	if _, _, err := o.backstops.Trigger(ctx, domain.BackstopEvent{FeederID: feeder, Reason: "again"}, nil); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Errorf("a second backstop: %v", err)
	}

	// The engine may not publish over it; a site it does not cover is free.
	batch := []domain.Envelope{repotest.Envelope(b.ID, o.run.ID, 0, 100), repotest.Envelope(a.ID, o.run.ID, 0, 4000)}
	_, err = o.envelopes.Publish(ctx, o.run.ID, "batch-0001", batch)
	if !errors.Is(err, domain.ErrFailedPrecondition) || !strings.Contains(err.Error(), "transformer fault") {
		t.Errorf("publishing over a backstop: %v", err)
	}
	if result, err := o.envelopes.Publish(ctx, o.run.ID, "batch-0002", batch[:1]); err != nil || result.Published != 1 {
		t.Errorf("publishing for a site outside the backstop: %+v, %v", result, err)
	}

	// Time moves on. Extending with nothing to add changes nothing; an hour
	// later it adds the two half hours that have come into the horizon.
	if active, err := o.backstops.Extend(ctx); err != nil || active != 1 {
		t.Errorf("Extend = %d, %v", active, err)
	}
	if n := len(o.backstopEnvelopes(t, a.ID, at(0))); n != 48 {
		t.Errorf("%d backstop envelopes after an extension with nothing to add", n)
	}
	o.clock.set(at(4200))
	if active, err := o.backstops.Extend(ctx); err != nil || active != 1 {
		t.Errorf("Extend an hour on = %d, %v", active, err)
	}
	if n := len(o.backstopEnvelopes(t, a.ID, at(0))); n != 50 {
		t.Errorf("%d backstop envelopes after an hour, want 50", n)
	}

	// Seventy minutes into the day the operator clears it: the engine's
	// envelopes come back from the half hour in force.
	cleared, err := o.backstops.Clear(ctx, event.ID)
	if err != nil || cleared.ClearedAt == nil || !cleared.ClearedAt.Equal(at(4200)) || cleared.ClearedBy == nil || *cleared.ClearedBy != "operator" {
		t.Errorf("Clear = %+v, %v", cleared, err)
	}
	select {
	case <-signal:
	default:
		t.Error("the site's subscribers were not told of the end")
	}
	if left := o.backstopEnvelopes(t, a.ID, at(4200)); len(left) != 0 {
		t.Errorf("%d backstop envelopes still active", len(left))
	}
	current, err = o.envelopes.Current(ctx, a.ID, time.Time{})
	if err != nil || current == nil || current.Source != domain.SourceEngine || current.ExportLimitW != 3000 || current.ImportLimitW != 6000 ||
		current.EnvelopeRunID == nil || *current.EnvelopeRunID != o.run.ID || current.ID == engine[2].ID ||
		current.ExportBinding != domain.BindingVoltageHigh || !current.ValidFrom.Equal(at(3600)) {
		t.Errorf("the envelope in force after the backstop = %+v, %v", current, err)
	}
	// Past what the engine covered there is no envelope.
	if e, err := o.envelopes.Current(ctx, a.ID, at(6*1800)); err != nil || e != nil {
		t.Errorf("past the engine's envelopes: %+v, %v", e, err)
	}
	// What the backstop held in the past stays as the record.
	if past, err := o.envelopes.Current(ctx, a.ID, at(600)); err != nil || past == nil || past.Source != domain.SourceBackstop {
		t.Errorf("the envelope of the past = %+v, %v", past, err)
	}

	// Clearing again changes nothing.
	again, err := o.backstops.Clear(ctx, event.ID)
	if err != nil || !again.ClearedAt.Equal(*cleared.ClearedAt) {
		t.Errorf("Clear again = %+v, %v", again, err)
	}
	if active, err := o.backstops.Extend(ctx); err != nil || active != 0 {
		t.Errorf("Extend with nothing active = %d, %v", active, err)
	}
	// The engine publishes again.
	if result, err := o.envelopes.Publish(ctx, o.run.ID, "batch-0003", []domain.Envelope{repotest.Envelope(a.ID, o.run.ID, 3, 4000)}); err != nil || result.Published != 1 {
		t.Errorf("publishing after the backstop: %+v, %v", result, err)
	}
}

func TestBackstopRefusals(t *testing.T) {
	t.Parallel()
	o := newOps(t)
	ctx := repotest.Ctx()
	other := repotest.Seed(t, o.store, "LV20", 11)

	trigger := func(feederID uuid.UUID, sites ...uuid.UUID) error {
		_, _, err := o.backstops.Trigger(ctx, domain.BackstopEvent{FeederID: feederID, Reason: "test"}, sites)
		return err
	}
	if err := trigger(uuid.New()); !errors.Is(err, domain.ErrFailedPrecondition) {
		t.Errorf("an unknown feeder: %v", err)
	}
	if err := trigger(o.f.Feeder.ID, other.SiteA.ID); !errors.Is(err, domain.ErrFailedPrecondition) {
		t.Errorf("a site of another feeder: %v", err)
	}
	empty, err := o.store.CreateFeeder(ctx, repotest.NewFeeder("EMPTY"))
	if err != nil {
		t.Fatal(err)
	}
	if err := trigger(empty.ID); !errors.Is(err, domain.ErrFailedPrecondition) {
		t.Errorf("a feeder with no enrolled site: %v", err)
	}
	if _, _, err := o.backstops.Get(ctx, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("an unknown backstop: %v", err)
	}
	if _, err := o.backstops.Clear(ctx, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("clearing an unknown backstop: %v", err)
	}
	if _, _, err := o.backstops.List(ctx, uuid.New(), domain.Page{}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("backstops of an unknown feeder: %v", err)
	}

	// A named site, on a feeder with no config: the defaults apply, a
	// passive site may be covered, and with no engine envelope to give back
	// a clear leaves none.
	event, sites, err := o.backstops.Trigger(ctx, domain.BackstopEvent{FeederID: other.Feeder.ID, Reason: "test"}, []uuid.UUID{other.SiteB.ID})
	if err != nil || len(sites) != 1 || sites[0] != other.SiteB.ID {
		t.Fatalf("a backstop over one named site: %v, %v", sites, err)
	}
	if held := o.backstopEnvelopes(t, other.SiteB.ID, at(0)); len(held) != 48 || held[0].ValidTo.Sub(held[0].ValidFrom) != 30*time.Minute {
		t.Errorf("%d backstop envelopes with the default config", len(held))
	}
	if _, err := o.backstops.Clear(ctx, event.ID); err != nil {
		t.Errorf("Clear: %v", err)
	}
	if e, err := o.envelopes.Current(ctx, other.SiteB.ID, time.Time{}); err != nil || e != nil {
		t.Errorf("after the clear: %+v, %v", e, err)
	}
}

func TestBackstopFailuresLeaveNothingBehind(t *testing.T) {
	t.Parallel()
	ctx := repotest.Ctx()

	for _, step := range []string{"ListAllSites", "CreateBackstopEvent", "GetActiveEnvelopeConfig", "ListActiveEnvelopes", "ReplaceEnvelopes"} {
		t.Run("Trigger/"+step, func(t *testing.T) {
			t.Parallel()
			o := newOps(t)
			o.store.failAt(step)
			if _, _, err := o.backstops.Trigger(ctx, domain.BackstopEvent{FeederID: o.f.Feeder.ID, Reason: "test"}, nil); !errors.Is(err, errDown) {
				t.Fatalf("error = %v, want the store's", err)
			}
			o.store.failAt("")
			if events, _, err := o.backstops.List(ctx, o.f.Feeder.ID, domain.Page{}); err != nil || len(events) != 0 {
				t.Errorf("a backstop was left behind: %+v, %v", events, err)
			}
			if held := o.backstopEnvelopes(t, o.f.SiteA.ID, at(0)); len(held) != 0 {
				t.Errorf("%d backstop envelopes were left behind", len(held))
			}
		})
	}

	// With a backstop in force.
	active := func(t *testing.T) (*ops, domain.BackstopEvent) {
		t.Helper()
		o := newOps(t)
		o.limit(t, 0, 3000)
		event, _, err := o.backstops.Trigger(ctx, domain.BackstopEvent{FeederID: o.f.Feeder.ID, Reason: "test"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return o, event
	}
	for _, step := range []string{"ListBackstopEventSiteIDs", "ClearBackstopEvent", "SupersedeBackstopEnvelopes", "ListLatestEngineEnvelopes", "ReplaceEnvelopes"} {
		t.Run("Clear/"+step, func(t *testing.T) {
			t.Parallel()
			o, event := active(t)
			o.store.failAt(step)
			if _, err := o.backstops.Clear(ctx, event.ID); !errors.Is(err, errDown) {
				t.Fatalf("error = %v, want the store's", err)
			}
			o.store.failAt("")
			if got, _, err := o.backstops.Get(ctx, event.ID); err != nil || got.ClearedAt != nil {
				t.Errorf("the backstop after a failed clear = %+v, %v", got, err)
			}
			if held := o.backstopEnvelopes(t, o.f.SiteA.ID, at(0)); len(held) != 48 {
				t.Errorf("%d backstop envelopes after a failed clear, want 48", len(held))
			}
		})
	}
	for _, step := range []string{"ListFeeders", "GetActiveBackstopEvent", "ListBackstopEventSiteIDs", "ListAllSites", "ReplaceEnvelopes"} {
		t.Run("Extend/"+step, func(t *testing.T) {
			t.Parallel()
			o, _ := active(t)
			o.clock.set(at(3600))
			o.store.failAt(step)
			if _, err := o.backstops.Extend(ctx); !errors.Is(err, errDown) {
				t.Fatalf("error = %v, want the store's", err)
			}
		})
	}
	t.Run("Get", func(t *testing.T) {
		t.Parallel()
		o, event := active(t)
		o.store.failAt("ListBackstopEventSiteIDs")
		if _, _, err := o.backstops.Get(ctx, event.ID); !errors.Is(err, errDown) {
			t.Errorf("error = %v, want the store's", err)
		}
	})
	for _, step := range []string{"GetActiveBackstopEvent", "ListBackstopEventSiteIDs"} {
		t.Run("Publish/"+step, func(t *testing.T) {
			t.Parallel()
			o, _ := active(t)
			o.store.failAt(step)
			_, err := o.envelopes.Publish(ctx, o.run.ID, "batch-0001", []domain.Envelope{repotest.Envelope(o.f.SiteB.ID, o.run.ID, 0, 100)})
			if !errors.Is(err, errDown) {
				t.Errorf("error = %v, want the store's", err)
			}
		})
	}
}

func TestRunIntervals(t *testing.T) {
	t.Parallel()
	o := newOps(t)
	ctx := repotest.Ctx()
	runs := service.NewEnvelopeRuns(o.store)
	other := repotest.Seed(t, o.store, "LV20", 11)

	// The feeder of a row is the run's, whatever the row says.
	rows := []domain.EnvelopeRunInterval{
		repotest.Interval(uuid.New(), other.Feeder.ID, 0, 10000), repotest.Interval(uuid.New(), other.Feeder.ID, 1, 11000),
	}
	if created, err := runs.CreateIntervals(ctx, o.run.ID, rows); err != nil || created != 2 {
		t.Fatalf("CreateIntervals = %d, %v", created, err)
	}
	got, err := runs.ListIntervals(ctx, o.run.ID)
	if err != nil || len(got) != 2 || got[0].FeederID != o.f.Feeder.ID || got[0].EnvelopeRunID != o.run.ID || got[1].ForecastNetLoadW != 11000 {
		t.Errorf("ListIntervals = %+v, %v", got, err)
	}

	outside := []domain.EnvelopeRunInterval{repotest.Interval(o.run.ID, o.f.Feeder.ID, 48, 1)}
	if _, err := runs.CreateIntervals(ctx, o.run.ID, outside); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("an interval after the horizon: %v", err)
	}
	outside = []domain.EnvelopeRunInterval{repotest.Interval(o.run.ID, o.f.Feeder.ID, -1, 1)}
	if _, err := runs.CreateIntervals(ctx, o.run.ID, outside); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("an interval before the horizon: %v", err)
	}
	if _, err := runs.CreateIntervals(ctx, uuid.New(), rows); !errors.Is(err, domain.ErrFailedPrecondition) {
		t.Errorf("an unknown run: %v", err)
	}
	if _, err := runs.ListIntervals(ctx, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("intervals of an unknown run: %v", err)
	}
	if _, err := runs.Complete(ctx, o.run.ID, service.RunResult{Status: domain.RunCompleted}); err != nil {
		t.Fatal(err)
	}
	late := []domain.EnvelopeRunInterval{repotest.Interval(o.run.ID, o.f.Feeder.ID, 5, 1)}
	if _, err := runs.CreateIntervals(ctx, o.run.ID, late); !errors.Is(err, domain.ErrFailedPrecondition) {
		t.Errorf("a run that has finished: %v", err)
	}
}
