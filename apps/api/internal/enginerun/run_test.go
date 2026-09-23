package enginerun_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/gen/doelab/v1/doelabv1connect"
	"doelab/api/internal/apitest"
	"doelab/api/internal/domain"
	"doelab/api/internal/engine"
	"doelab/api/internal/enginerun"
	"doelab/api/internal/repo/repotest"
	"doelab/api/internal/service"
)

// world is the test API with LV10 imported into it: 94 sites, 56 of them
// enrolled, and two days of profiles with a household shape.
type world struct {
	*apitest.API
	feeder domain.Feeder
	sites  []domain.Site
	runner *enginerun.Runner
}

func lv10(t *testing.T) *engine.Network {
	t.Helper()
	data, err := os.ReadFile("../engine/testdata/lv10_network.json")
	if err != nil {
		t.Fatal(err)
	}
	var net engine.Network
	if err := json.Unmarshal(data, &net); err != nil {
		t.Fatal(err)
	}
	return &net
}

// enrolled is the number of sites the seeded import enrols.
const enrolled = 56

// horizon is the number of intervals a run covers in these tests: a whole
// day, or in -short mode, which is what the pre-commit hook runs under the
// race detector and coverage, three hours. The engine is some thirty times
// slower there, and the paths are the same.
func horizon() int {
	if testing.Short() {
		return 6
	}
	return 48
}

// perBatch is how many intervals fit one publish when a test makes the
// batches small, so that a run takes three.
func perBatch() int {
	if testing.Short() {
		return 2
	}
	return 21
}

// smallBatches makes a run publish in three batches.
func (w *world) smallBatches() {
	w.runner.MaxBatch = (perBatch()+1)*enrolled - 1
}

func newWorld(t *testing.T, profiles bool) *world {
	t.Helper()
	a := apitest.New(t)
	ctx := repotest.Ctx()
	homes := make([]service.Customer, 300)
	for i := range homes {
		homes[i] = service.Customer{Number: i + 1, CapacityKWp: 1 + float64(i%20)/10}
	}
	importer := service.NewImporter(a.Store, nil)
	imported, err := importer.ImportFeeder(ctx, lv10(t), homes, service.ImportOptions{
		Code: "LV10", Attribution: "test", TapPU: 0.975, PVScale: 3, Seed: 20261001, EnrolledFraction: 0.6,
	})
	if err != nil {
		t.Fatal(err)
	}

	if horizon() != 48 {
		short := repotest.Config(imported.Feeder.ID)
		short.PVScale, short.HorizonIntervals = 3, int32(horizon())
		if _, err := a.Store.CreateEnvelopeConfig(ctx, short); err != nil {
			t.Fatal(err)
		}
	}

	if profiles {
		// The test clock starts on 1 October 2012 UTC; the forecast for it is
		// read from the same local dates of the profile year. Three days
		// around them, each with an evening peak and a midday sun.
		zone, _ := time.LoadLocation("Australia/Sydney")
		start := time.Date(2010, 9, 30, 0, 0, 0, 0, zone)
		series := map[int][]domain.SiteProfile{}
		for _, site := range imported.Sites {
			rows := make([]domain.SiteProfile, 4*48)
			scale := 0.6 + float64(*site.ProfileCustomer%10)/10
			for k := range rows {
				ts := start.Add(time.Duration(k) * 30 * time.Minute)
				hour := float64(ts.In(zone).Hour()) + float64(ts.In(zone).Minute())/60
				rows[k] = domain.SiteProfile{
					TS:    ts,
					LoadW: scale * (300 + 1500*math.Exp(-(hour-18.5)*(hour-18.5)/6)),
					PVW:   scale * 1200 * math.Max(0, math.Cos((hour-12)*math.Pi/12)),
				}
			}
			series[int(*site.ProfileCustomer)] = rows
		}
		if err := importer.ImportProfiles(ctx, imported.Sites, series); err != nil {
			t.Fatal(err)
		}
	}

	api := enginerun.NewAPI(a.HTTP, a.URL, enginerun.Token(apitest.EngineToken))
	runner := enginerun.NewRunner(api, "LV10", "test-1.0", slog.New(slog.NewTextHandler(io.Discard, nil)))
	return &world{API: a, feeder: imported.Feeder, sites: imported.Sites, runner: runner}
}

func (w *world) enrolledSite(t *testing.T) domain.Site {
	t.Helper()
	for _, s := range w.sites {
		if s.ExportCapW > 0 {
			return s
		}
	}
	t.Fatal("no enrolled site")
	return domain.Site{}
}

func (w *world) run(t *testing.T, id string) domain.EnvelopeRun {
	t.Helper()
	runs, _, err := w.Store.ListEnvelopeRuns(context.Background(), w.feeder.ID, nil, domain.Page{Size: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range runs {
		if r.ID.String() == id {
			return r
		}
	}
	t.Fatalf("run %s is not recorded", id)
	return domain.EnvelopeRun{}
}

func TestRun(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	ctx := context.Background()
	// Feeder time is 00:10 on the clock's first day: the horizon starts at
	// the interval in force, 00:00.
	w.Clock.Advance(10 * time.Minute)

	summary, err := w.runner.Run(ctx, w.SimClock.Now(), "")
	if err != nil {
		t.Fatal(err)
	}
	n := horizon()
	if !summary.From.Equal(apitest.Anchor) || !summary.To.Equal(apitest.Anchor.Add(time.Duration(n)*30*time.Minute)) || summary.Intervals != n ||
		summary.Sites != enrolled || summary.Published != n*enrolled || summary.Superseded != 0 || summary.Skipped {
		t.Fatalf("summary = %+v", summary)
	}
	t.Logf("export limits over the day: %.0f to %.0f W per site", summary.MinExportW, summary.MaxExportW)
	if n == 48 && (summary.MaxExportW <= summary.MinExportW || summary.MaxExportW > 10000) {
		t.Errorf("export limits %.0f to %.0f W: want them to vary through the day, within the 10 kW cap", summary.MinExportW, summary.MaxExportW)
	}

	// The run is recorded as completed, with what it did.
	run := w.run(t, summary.RunID)
	if run.Status != domain.RunCompleted || run.SiteCount != enrolled || run.IntervalCount != int32(n) || run.EnvelopeCount != int32(n*enrolled) ||
		run.EngineVersion != "test-1.0" || run.DurationMS == nil || run.Error != nil || !run.HorizonFrom.Equal(summary.From) {
		t.Errorf("run = %+v", run)
	}

	// An enrolled site has an envelope for every interval, each naming what
	// binds it; a passive site has none.
	site := w.enrolledSite(t)
	envelopes, _, err := w.Store.ListEnvelopes(ctx, site.ID, summary.From, summary.To, false, domain.Page{Size: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(envelopes) != n {
		t.Fatalf("the enrolled site has %d envelopes, want %d", len(envelopes), n)
	}
	for i, e := range envelopes {
		if !e.ValidFrom.Equal(summary.From.Add(time.Duration(i)*30*time.Minute)) || e.ValidTo.Sub(e.ValidFrom) != 30*time.Minute ||
			e.Source != domain.SourceEngine || e.EnvelopeRunID.String() != summary.RunID ||
			e.ExportLimitW < 0 || e.ExportLimitW > site.ExportCapW || e.ImportLimitW < 0 || e.ImportLimitW > site.ImportCapW ||
			e.ExportLimitW != math.Floor(e.ExportLimitW) {
			t.Fatalf("envelope %d = %+v", i, e)
		}
		if e.ExportBinding == domain.BindingNone || e.ExportBinding == "" || e.ImportBinding == "" {
			t.Fatalf("envelope %d has no binding: %+v", i, e)
		}
		if e.ExportBinding != domain.BindingSiteCap && e.ExportBindingElement == "" {
			t.Fatalf("envelope %d is bound by %s at no element", i, e.ExportBinding)
		}
	}
	for _, s := range w.sites {
		if s.ExportCapW == 0 {
			passive, _, err := w.Store.ListEnvelopes(ctx, s.ID, summary.From, summary.To, true, domain.Page{Size: 100})
			if err != nil || len(passive) != 0 {
				t.Errorf("passive site %s has %d envelopes", s.NMI, len(passive))
			}
			break
		}
	}

	// The same horizon again: the run had finished, so nothing is computed.
	again, err := w.runner.Run(ctx, w.SimClock.Now(), "")
	if err != nil || !again.Skipped || again.RunID != summary.RunID || again.Published != 0 {
		t.Errorf("the same horizon again: %+v, %v", again, err)
	}
	// A forced rerun is a run of its own, and supersedes the whole horizon.
	forced, err := w.runner.Run(ctx, w.SimClock.Now(), ":forced")
	if err != nil || forced.Skipped || forced.RunID == summary.RunID || forced.Superseded != n*enrolled {
		t.Errorf("a forced rerun: %+v, %v", forced, err)
	}
}

// A later run replaces the earlier one for the intervals they share, and
// leaves the ones that have passed alone.
func TestSecondRunSupersedesOnlyTheOverlap(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	ctx := context.Background()

	first, err := w.runner.Run(ctx, w.SimClock.Now(), "")
	if err != nil {
		t.Fatal(err)
	}
	w.Clock.Advance(time.Hour) // two intervals later
	second, err := w.runner.Run(ctx, w.SimClock.Now(), "")
	if err != nil {
		t.Fatal(err)
	}
	n := horizon()
	if !second.From.Equal(first.From.Add(time.Hour)) || second.Published != n*enrolled || second.Superseded != (n-2)*enrolled {
		t.Fatalf("second run = %+v; want it to supersede the %d intervals the two share", second, n-2)
	}

	site := w.enrolledSite(t)
	active, _, err := w.Store.ListEnvelopes(ctx, site.ID, first.From, second.To, false, domain.Page{Size: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != n+2 {
		t.Fatalf("%d active envelopes, want %d: 2 of the first run, %d of the second", len(active), n+2, n)
	}
	for i, e := range active {
		want := second.RunID
		if i < 2 {
			want = first.RunID
		}
		if e.EnvelopeRunID.String() != want {
			t.Errorf("interval %d is from run %s, want %s", i, e.EnvelopeRunID, want)
		}
	}
	history, _, err := w.Store.ListEnvelopes(ctx, site.ID, first.From, second.To, true, domain.Page{Size: 200})
	if err != nil || len(history) != 2*n {
		t.Errorf("%d envelopes with history, want %d: the superseded rows are kept", len(history), 2*n)
	}
}

func TestProportionalPolicy(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	ctx := repotest.Ctx()
	config := repotest.Config(w.feeder.ID)
	config.Policy, config.PVScale, config.HorizonIntervals = domain.PolicyProportional, 3, 4
	if _, err := w.Store.CreateEnvelopeConfig(ctx, config); err != nil {
		t.Fatal(err)
	}

	summary, err := w.runner.Run(ctx, w.SimClock.Now().Add(12*time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Intervals != 4 || summary.Published != 4*enrolled {
		t.Fatalf("summary = %+v", summary)
	}
	// Under the proportional policy every site has the same fraction of its
	// cap, so two sites with different caps have different limits.
	fractions := map[float64]bool{}
	limits := map[float64]bool{}
	for _, s := range w.sites {
		if s.ExportCapW == 0 {
			continue
		}
		e, err := w.Store.GetCurrentEnvelope(ctx, s.ID, summary.From)
		if err != nil {
			t.Fatal(err)
		}
		limits[e.ExportLimitW] = true
		fractions[math.Round(100*e.ExportLimitW/s.ExportCapW)] = true
	}
	if len(fractions) != 1 || len(limits) < 2 {
		t.Errorf("%d distinct fractions and %d distinct limits; want one fraction, several limits", len(fractions), len(limits))
	}
}

// flaky is the API with faults injected: a publish that fails, a completion
// that fails, a forecast that cancels the run.
type flaky struct {
	doelabv1connect.EnvelopeServiceClient
	doelabv1connect.EnvelopeRunServiceClient
	doelabv1connect.FeederServiceClient
	failPublish  atomic.Int32 // fail the publish with this index, once
	failComplete atomic.Bool
	cancel       context.CancelFunc
	longError    string // fail every publish with this message
}

func (f *flaky) PublishEnvelopes(ctx context.Context, req *connect.Request[doelabv1.PublishEnvelopesRequest]) (*connect.Response[doelabv1.PublishEnvelopesResponse], error) {
	if f.longError != "" {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New(f.longError))
	}
	if strings.HasSuffix(req.Msg.GetIdempotencyKey(), ":batch1") && f.failPublish.CompareAndSwap(1, 0) {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("connection reset"))
	}
	return f.EnvelopeServiceClient.PublishEnvelopes(ctx, req)
}

func (f *flaky) CompleteEnvelopeRun(ctx context.Context, req *connect.Request[doelabv1.CompleteEnvelopeRunRequest]) (*connect.Response[doelabv1.CompleteEnvelopeRunResponse], error) {
	if f.failComplete.CompareAndSwap(true, false) {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("connection reset"))
	}
	return f.EnvelopeRunServiceClient.CompleteEnvelopeRun(ctx, req)
}

func (f *flaky) GetFeederForecast(ctx context.Context, req *connect.Request[doelabv1.GetFeederForecastRequest]) (*connect.Response[doelabv1.GetFeederForecastResponse], error) {
	res, err := f.FeederServiceClient.GetFeederForecast(ctx, req)
	if f.cancel != nil {
		f.cancel()
	}
	return res, err
}

func (w *world) inject() *flaky {
	f := &flaky{
		EnvelopeServiceClient:    w.runner.API.Envelopes,
		EnvelopeRunServiceClient: w.runner.API.Runs,
		FeederServiceClient:      w.runner.API.Feeders,
	}
	w.runner.API.Envelopes, w.runner.API.Runs, w.runner.API.Feeders = f, f, f
	return f
}

// The process dies between two batches: the run stays "running". Running it
// again replays the batch that was sent and continues with the rest.
func TestInterruptedRunResumes(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	ctx := context.Background()
	w.smallBatches()
	faults := w.inject()
	faults.failPublish.Store(1)
	faults.failComplete.Store(true)

	first, err := w.runner.Run(ctx, w.SimClock.Now(), "")
	if err == nil || !strings.Contains(err.Error(), "publish batch 2 of 3") || !strings.Contains(err.Error(), "complete run") {
		t.Fatalf("the interrupted run: %v", err)
	}
	if first.Published != perBatch()*enrolled {
		t.Errorf("the interrupted run published %d envelopes, want the first batch: %d", first.Published, perBatch()*enrolled)
	}
	if run := w.run(t, first.RunID); run.Status != domain.RunRunning {
		t.Fatalf("the interrupted run is %s, want it still running", run.Status)
	}

	second, err := w.runner.Run(ctx, w.SimClock.Now(), "")
	if err != nil {
		t.Fatal(err)
	}
	left := horizon() - perBatch()
	if second.RunID != first.RunID || second.Skipped || second.Published != left*enrolled || second.Superseded != 0 {
		t.Errorf("the resumed run = %+v; want the same run, publishing the %d intervals that were left", second, left)
	}
	run := w.run(t, first.RunID)
	if run.Status != domain.RunCompleted || run.EnvelopeCount != int32(horizon()*enrolled) {
		t.Errorf("after resuming: %s with %d envelopes", run.Status, run.EnvelopeCount)
	}
}

// A run that fails is recorded as failed, with the reason.
func TestFailedRunIsRecorded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("no forecast", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t, false) // no profiles
		summary, err := w.runner.Run(ctx, w.SimClock.Now(), "")
		if err == nil || !strings.Contains(err.Error(), "forecast") {
			t.Fatalf("error = %v", err)
		}
		run := w.run(t, summary.RunID)
		if run.Status != domain.RunFailed || run.Error == nil || !strings.Contains(*run.Error, "run the import") || run.EnvelopeCount != 0 {
			t.Errorf("run = %+v", run)
		}
	})

	t.Run("a publish fails", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t, true)
		w.smallBatches()
		w.inject().failPublish.Store(1)
		summary, err := w.runner.Run(ctx, w.SimClock.Now(), "")
		if err == nil || !strings.Contains(err.Error(), "publish batch 2 of 3") {
			t.Fatalf("error = %v", err)
		}
		run := w.run(t, summary.RunID)
		if run.Status != domain.RunFailed || !strings.Contains(*run.Error, "connection reset") || run.EnvelopeCount != int32(perBatch()*enrolled) {
			t.Errorf("run = %+v", run)
		}
		// What was published before the failure stays in force.
		if _, err := w.Store.GetCurrentEnvelope(ctx, w.enrolledSite(t).ID, summary.From); err != nil {
			t.Errorf("the first batch is gone: %v", err)
		}
	})

	t.Run("cancelled while computing", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t, true)
		runCtx, cancel := context.WithCancel(ctx)
		w.inject().cancel = cancel
		summary, err := w.runner.Run(runCtx, w.SimClock.Now(), "")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
		// The record is written although the context has ended.
		if run := w.run(t, summary.RunID); run.Status != domain.RunFailed || !strings.Contains(*run.Error, "context canceled") {
			t.Errorf("run = %+v", run)
		}
	})

	t.Run("a site the forecast does not cover", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t, true)
		// A site added after the import has no profile: the forecast refuses.
		node := w.sites[0].NodeID
		if _, err := w.Store.CreateSite(repotest.Ctx(), domain.Site{
			NMI: repotest.NMI(t, 5000), FeederID: w.feeder.ID, NodeID: node, Name: "late", Phase: 1,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := w.runner.Run(ctx, w.SimClock.Now(), ""); err == nil || !strings.Contains(err.Error(), "has no profile") {
			t.Errorf("error = %v", err)
		}
	})
}

func TestRunErrorsBeforeARunExists(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	ctx := context.Background()

	w.runner.FeederCode = "LV99"
	if _, err := w.runner.Run(ctx, w.SimClock.Now(), ""); err == nil || !strings.Contains(err.Error(), "load feeder LV99") {
		t.Errorf("an unknown feeder: %v", err)
	}
	w.runner.FeederCode = "LV10"

	// The engine's token is what lets it create a run.
	anonymous := enginerun.NewRunner(enginerun.NewAPI(w.HTTP, w.URL), "LV10", "test", w.runner.Log)
	if _, err := anonymous.Run(ctx, w.SimClock.Now(), ""); connect.CodeOf(err) != connect.CodeUnauthenticated || !strings.Contains(err.Error(), "create run") {
		t.Errorf("with no token: %v", err)
	}
	runs, _, _ := w.Store.ListEnvelopeRuns(ctx, w.feeder.ID, nil, domain.Page{Size: 10})
	if len(runs) != 0 {
		t.Errorf("%d runs were recorded", len(runs))
	}
}

// A feeder that is below its voltage band before any site moves: every limit
// is zero, which is a valid answer and not a failure.
func TestFeederBelowItsBand(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	ctx := repotest.Ctx()
	// The lowest tap, and a lower limit of 1.0 pu.
	feeder := w.feeder
	feeder.TapPU = 0.85
	if _, err := w.Store.UpdateFeeder(ctx, feeder); err != nil {
		t.Fatal(err)
	}
	config := repotest.Config(w.feeder.ID)
	config.VMinPU, config.VMaxPU, config.PVScale, config.HorizonIntervals = 1.0, 1.2, 3, 2
	if _, err := w.Store.CreateEnvelopeConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	summary, err := w.runner.Run(ctx, w.SimClock.Now().Add(18*time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	if summary.MaxExportW != 0 || summary.MinExportW != 0 {
		t.Errorf("limits %v to %v W, want zero", summary.MinExportW, summary.MaxExportW)
	}
	e, err := w.Store.GetCurrentEnvelope(ctx, w.enrolledSite(t).ID, summary.From)
	if err != nil || e.ExportBinding != domain.BindingVoltageLow || e.ImportLimitW != 0 {
		t.Errorf("envelope = %+v, %v; want zero limits bound by voltage_low", e, err)
	}
}

func TestClockSettings(t *testing.T) {
	t.Parallel()
	w := newWorld(t, false)
	anchor, speed, err := w.runner.API.ClockSettings(context.Background())
	if err != nil || speed != apitest.Speed || !anchor.Equal(apitest.Anchor) {
		t.Errorf("clock = speed %v, anchor %v, %v", speed, anchor, err)
	}
	w.Close()
	if _, _, err := w.runner.API.ClockSettings(context.Background()); err == nil {
		t.Error("the clock of a stopped API")
	}
}

// failOn fails every call to the named procedures, as an unreachable API
// would.
func failOn(procedures ...string) connect.ClientOption {
	return connect.WithInterceptors(connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			for _, p := range procedures {
				if req.Spec().Procedure == p {
					return nil, connect.NewError(connect.CodeUnavailable, errors.New("injected: "+p))
				}
			}
			return next(ctx, req)
		}
	}))
}

// Every read of the model can fail; each failure stops the run before
// anything is recorded.
func TestLoadFailures(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	for _, procedure := range []string{
		doelabv1connect.FeederServiceListFeederNodesProcedure,
		doelabv1connect.FeederServiceListFeederLinesProcedure,
		doelabv1connect.SiteServiceListSitesProcedure,
		doelabv1connect.EnvelopeConfigServiceGetActiveEnvelopeConfigProcedure,
	} {
		api := enginerun.NewAPI(w.HTTP, w.URL, enginerun.Token(apitest.EngineToken), failOn(procedure))
		_, err := enginerun.NewRunner(api, "LV10", "test", w.runner.Log).Run(context.Background(), w.SimClock.Now(), "")
		if err == nil || !strings.Contains(err.Error(), "load feeder LV10") || !strings.Contains(err.Error(), procedure) {
			t.Errorf("with %s failing: %v", procedure, err)
		}
	}
	runs, _, _ := w.Store.ListEnvelopeRuns(context.Background(), w.feeder.ID, nil, domain.Page{Size: 10})
	if len(runs) != 0 {
		t.Errorf("%d runs were recorded", len(runs))
	}
}

// A model the engine refuses: the run is recorded as failed, with the reason.
func TestModelTheEngineRefuses(t *testing.T) {
	t.Parallel()
	ctx := repotest.Ctx()

	t.Run("a config with an inverted band", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t, true)
		// The service and the schema both refuse this; the in-memory store
		// lets it through, as a config written by hand would be.
		config := repotest.Config(w.feeder.ID)
		config.VMinPU, config.VMaxPU = 1.10, 0.94
		if _, err := w.Store.CreateEnvelopeConfig(ctx, config); err != nil {
			t.Fatal(err)
		}
		summary, err := w.runner.Run(ctx, w.SimClock.Now(), "")
		if !errors.Is(err, engine.ErrInvalidConfig) && (err == nil || !strings.Contains(err.Error(), "VMinPU")) {
			t.Fatalf("error = %v", err)
		}
		if run := w.run(t, summary.RunID); run.Status != domain.RunFailed || !strings.Contains(*run.Error, "VMinPU") {
			t.Errorf("run = %+v", run)
		}
	})

	t.Run("a tree with a line missing", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t, true)
		// A node with no line to its parent: not a feeder the engine can
		// build.
		nodes, err := w.Store.ListFeederTree(ctx, w.feeder.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Store.CreateFeederNode(ctx, domain.FeederNode{FeederID: w.feeder.ID, Name: "stub", ParentNodeID: &nodes[0].ID}); err != nil {
			t.Fatal(err)
		}
		summary, err := w.runner.Run(ctx, w.SimClock.Now(), "")
		if err == nil || !strings.Contains(err.Error(), "has no line from its parent") {
			t.Fatalf("error = %v", err)
		}
		if run := w.run(t, summary.RunID); run.Status != domain.RunFailed {
			t.Errorf("run = %+v", run)
		}
	})
}

// A very long error is cut to what the run's error column takes.
func TestLongErrorIsTruncated(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	faults := w.inject()
	faults.longError = strings.Repeat("x", 5000)
	summary, err := w.runner.Run(context.Background(), w.SimClock.Now(), "")
	if err == nil {
		t.Fatal("no error")
	}
	if run := w.run(t, summary.RunID); run.Status != domain.RunFailed || len(*run.Error) != 2000 {
		t.Errorf("recorded error has %d characters, want 2000", len(*run.Error))
	}
}

// A run that is cancelled ends the loop without another wait.
func TestLoopEndsWhenARunIsCancelled(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	w.inject().cancel = cancel
	if err := w.runner.Loop(ctx, w.SimClock, 2, func(time.Duration) <-chan time.Time {
		t.Error("the loop waited after its run was cancelled")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLoop(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Each wait is recorded and answered at once, with feeder time moved to
	// where the wait was going.
	var waits []time.Duration
	after := func(d time.Duration) <-chan time.Time {
		waits = append(waits, d)
		if len(waits) == 3 {
			cancel()
		}
		w.Clock.Advance(d * apitest.Speed)
		fired := make(chan time.Time, 1)
		fired <- time.Now()
		return fired
	}
	w.Clock.Advance(10 * time.Minute)
	if err := w.runner.Loop(ctx, w.SimClock, 2, after); err != nil {
		t.Fatal(err)
	}

	// Feeder time was 00:10. The next run is due at 01:00, two intervals
	// after the boundary the first run started on: 50 minutes of feeder time,
	// 50 seconds at speed 60. Then every hour.
	want := []time.Duration{50 * time.Second, time.Minute, time.Minute}
	if len(waits) != 3 || waits[0] != want[0] || waits[1] != want[1] || waits[2] != want[2] {
		t.Errorf("waits = %v, want %v", waits, want)
	}
	runs, _, err := w.Store.ListEnvelopeRuns(context.Background(), w.feeder.ID, nil, domain.Page{Size: 10})
	if err != nil || len(runs) != 3 {
		t.Fatalf("%d runs, want 3", len(runs))
	}
	// Newest first: 02:00, 01:00, 00:00.
	for i, wantFrom := range []time.Duration{2 * time.Hour, time.Hour, 0} {
		if !runs[i].HorizonFrom.Equal(apitest.Anchor.Add(wantFrom)) || runs[i].Status != domain.RunCompleted {
			t.Errorf("run %d starts at %v (%s)", i, runs[i].HorizonFrom, runs[i].Status)
		}
	}
}

// A failing run does not stop the loop.
func TestLoopSurvivesFailures(t *testing.T) {
	t.Parallel()
	w := newWorld(t, false) // no profiles: every run fails
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := 0
	after := func(time.Duration) <-chan time.Time {
		if n++; n == 2 {
			cancel()
		}
		fired := make(chan time.Time, 1)
		fired <- time.Now()
		return fired
	}
	if err := w.runner.Loop(ctx, w.SimClock, 2, after); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("the loop waited %d times, want 2", n)
	}

	// With the feeder gone there is no interval to go by: it waits a fixed
	// five seconds and tries again.
	w.runner.FeederCode = "LV99"
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	var wait time.Duration
	if err := w.runner.Loop(ctx2, w.SimClock, 2, func(d time.Duration) <-chan time.Time {
		wait = d
		cancel2()
		return make(chan time.Time)
	}); err != nil {
		t.Fatal(err)
	}
	if wait != 5*time.Second {
		t.Errorf("wait after a failed load = %v, want 5s", wait)
	}

	// A context that has ended stops the loop before it waits at all.
	done, stop := context.WithCancel(context.Background())
	stop()
	if err := w.runner.Loop(done, w.SimClock, 2, func(time.Duration) <-chan time.Time {
		t.Error("the loop waited after its context ended")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
