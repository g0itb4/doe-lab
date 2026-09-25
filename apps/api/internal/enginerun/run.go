package enginerun

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/internal/domain"
	"doelab/api/internal/engine"
	"doelab/api/internal/protomap"
	"doelab/api/internal/topology"
)

// Runner computes and publishes envelopes for one feeder.
type Runner struct {
	API *API
	// FeederCode is the feeder to run.
	FeederCode string
	// Version is recorded on every run.
	Version string
	// Workers is how many intervals are solved at once.
	Workers int
	// MaxBatch is the largest number of envelopes in one publish.
	MaxBatch int
	Log      *slog.Logger
	// now is the wall clock, replaceable in tests.
	now func() time.Time
}

// NewRunner builds a runner with the defaults.
func NewRunner(api *API, feederCode, version string, log *slog.Logger) *Runner {
	return &Runner{API: api, FeederCode: feederCode, Version: version, Workers: 4, MaxBatch: defaultMaxBatch, Log: log, now: time.Now}
}

// loadPowerFactor is the power factor assumed for household load. The
// profiles carry energy only.
const loadPowerFactor = 0.95

// defaultMaxBatch is the largest number of envelopes in one publish. The API
// takes 5000.
const defaultMaxBatch = 4000

// Summary is what a run did.
type Summary struct {
	RunID     string
	From, To  time.Time
	Intervals int
	// Sites is the number of sites that were given an envelope.
	Sites      int
	Published  int
	Superseded int
	// Skipped is true when a run for the same horizon and config had already
	// finished, and nothing was computed.
	Skipped  bool
	Duration time.Duration
	// MinExportW and MaxExportW are the smallest and the largest export limit
	// given to a site over the horizon.
	MinExportW float64
	MaxExportW float64
}

// Run computes the envelopes of the horizon that starts at the interval
// holding from, publishes them and records the run.
//
// A run is identified by its feeder, config version and horizon start. Running
// the same one again continues it if it was interrupted, and does nothing if
// it had finished. suffix makes a run distinct on purpose: a forced rerun.
func (r *Runner) Run(ctx context.Context, from time.Time, suffix string) (Summary, error) {
	started := r.now()
	model, err := r.API.load(ctx, r.FeederCode)
	if err != nil {
		return Summary{}, fmt.Errorf("load feeder %s: %w", r.FeederCode, err)
	}
	config := model.config
	interval := time.Duration(config.IntervalMinutes) * time.Minute
	from = from.UTC().Truncate(interval)
	to := from.Add(time.Duration(config.HorizonIntervals) * interval)
	summary := Summary{From: from, To: to, Intervals: int(config.HorizonIntervals)}

	key := fmt.Sprintf("run:%s:v%d:%s%s", model.feeder.Code, config.Version, from.Format("20060102T150405Z"), suffix)
	created, err := r.API.Runs.CreateEnvelopeRun(ctx, connect.NewRequest(&doelabv1.CreateEnvelopeRunRequest{
		EnvelopeRun: &doelabv1.EnvelopeRun{
			FeederId: model.feeder.ID.String(), EnvelopeConfigId: config.ID.String(), IdempotencyKey: key,
			HorizonFrom: timestamppb.New(from), HorizonTo: timestamppb.New(to), EngineVersion: r.Version,
		},
	}))
	if err != nil {
		return summary, fmt.Errorf("create run: %w", err)
	}
	run := created.Msg.GetEnvelopeRun()
	summary.RunID = run.GetId()
	if run.GetStatus() != doelabv1.RunStatus_RUN_STATUS_RUNNING {
		summary.Skipped = true
		return summary, nil
	}

	batches, intervals, err := r.compute(ctx, model, from, interval, &summary)
	if err == nil {
		err = r.record(ctx, run.GetId(), intervals)
	}
	if err == nil {
		err = r.publish(ctx, run.GetId(), key, batches, &summary)
	}
	summary.Duration = r.now().Sub(started)

	// The run is recorded either way: a failure with its reason.
	complete := &doelabv1.CompleteEnvelopeRunRequest{
		Id: run.GetId(), Status: doelabv1.RunStatus_RUN_STATUS_COMPLETED,
		DurationMs: int32(min(summary.Duration.Milliseconds(), math.MaxInt32)),    //nolint:gosec // G115: capped
		SiteCount:  int32(summary.Sites), IntervalCount: int32(summary.Intervals), //nolint:gosec // G115: a feeder's sites
	}
	if err != nil {
		reason := truncate(err.Error(), 2000)
		complete.Status, complete.Error = doelabv1.RunStatus_RUN_STATUS_FAILED, &reason
	}
	// The record must be written even when ctx is what ended the run.
	if _, completeErr := r.API.Runs.CompleteEnvelopeRun(context.WithoutCancel(ctx), connect.NewRequest(complete)); completeErr != nil {
		return summary, errors.Join(err, fmt.Errorf("complete run: %w", completeErr))
	}
	return summary, err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// compute solves every interval of the horizon. It returns the envelopes in
// batches, in time order, and for each interval the state of the feeder that
// the envelopes were computed against.
func (r *Runner) compute(ctx context.Context, model feederModel, from time.Time, interval time.Duration, summary *Summary) ([][]*doelabv1.Envelope, []*doelabv1.EnvelopeRunInterval, error) {
	net, siteIDs, err := topology.ToNetwork(model.feeder, model.nodes, model.lines, model.sites)
	if err != nil {
		return nil, nil, err
	}
	config := model.config
	policy := engine.Equal
	if config.Policy == domain.PolicyProportional {
		policy = engine.Proportional
	}
	eng, err := engine.New(net, engine.Config{
		Policy:     policy,
		PrecisionW: 1,
		Limits: engine.Limits{
			VMinPU: config.VMinPU, VMaxPU: config.VMaxPU,
			TransformerPU: config.TransformerLimitPct / 100, LinePU: config.LineLimitPct / 100,
		},
	})
	if err != nil {
		return nil, nil, err
	}

	// The sites in the engine's order, with their caps.
	byID := make(map[uuid.UUID]domain.Site, len(model.sites))
	for _, s := range model.sites {
		byID[s.ID] = s
	}
	sites := make([]domain.Site, len(siteIDs))
	for i, id := range siteIDs {
		sites[i] = byID[id]
		if sites[i].ExportCapW > 0 || sites[i].ImportCapW > 0 {
			summary.Sites++
		}
	}

	intervals := summary.Intervals
	points, err := r.API.forecast(ctx, model.feeder.ID, from, from.Add(time.Duration(intervals)*interval))
	if err != nil {
		return nil, nil, fmt.Errorf("forecast: %w", err)
	}

	// Each interval is independent of the others, so they are solved in
	// parallel, each worker with a solution of its own.
	results := make([]engine.Result, intervals)
	reports := make([]engine.Report, intervals)
	errs := make([]error, intervals)
	next := make(chan int, intervals)
	for k := range intervals {
		next <- k
	}
	close(next)
	var wg sync.WaitGroup
	for range max(1, r.Workers) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var sol engine.Solution
			inputs := make([]engine.SiteInput, len(sites))
			tanPhi := math.Tan(math.Acos(loadPowerFactor))
			for k := range next {
				if errs[k] = ctx.Err(); errs[k] != nil {
					continue
				}
				start := from.Add(time.Duration(k) * interval)
				// The forecast is half-hourly; a shorter interval reads the
				// half hour it lies in.
				slot := start.Truncate(30 * time.Minute).Unix()
				for i, site := range sites {
					p, ok := points[forecastKey{site.ID, slot}]
					if !ok {
						errs[k] = fmt.Errorf("no forecast for site %s at %s", site.NMI, start.Format(time.RFC3339))
						break
					}
					// A passive site draws its load at 0.95 lagging, less
					// its PV at unity power factor.
					inputs[i] = engine.SiteInput{
						Base:       complex(p.GetLoadW()-p.GetPvW()*config.PVScale, p.GetLoadW()*tanPhi),
						ExportCapW: site.ExportCapW, ImportCapW: site.ImportCapW,
					}
				}
				if errs[k] == nil {
					results[k], errs[k] = eng.Envelope(inputs, &sol)
				}
				if errs[k] == nil {
					reports[k], errs[k] = eng.Report(inputs, config.StaticLimitW, &sol)
				}
			}
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, nil, err
	}

	binding := func(b engine.Binding) (doelabv1.BindingConstraint, string) {
		return protomap.BindingConstraintToProto(domain.BindingConstraint(b.Constraint.String())), b.Element
	}
	summary.MinExportW = math.Inf(1)
	var batches [][]*doelabv1.Envelope
	var batch []*doelabv1.Envelope
	states := make([]*doelabv1.EnvelopeRunInterval, intervals)
	for k, result := range results {
		start := from.Add(time.Duration(k) * interval)
		exportBinding, exportElement := binding(result.Export)
		importBinding, importElement := binding(result.Import)
		report := reports[k]
		staticBinding, staticElement := binding(report.Static.Worst)
		state := &doelabv1.EnvelopeRunInterval{
			ValidFrom: timestamppb.New(start), ValidTo: timestamppb.New(start.Add(interval)),
			ForecastNetLoadW: real(report.Forecast.SourceVA), ForecastLoadingPct: report.Forecast.LoadingPU * 100,
			ForecastVMinPu: report.Forecast.VMinPU, ForecastVMaxPu: report.Forecast.VMaxPU,
			StaticLimitTotalW: report.StaticTotalW, StaticVMaxPu: report.Static.VMaxPU,
			StaticBinding: staticBinding, StaticBindingElement: staticElement,
		}
		states[k] = state
		for i, site := range sites {
			if site.ExportCapW == 0 && site.ImportCapW == 0 {
				continue // passive: forecast, not controlled
			}
			e := &doelabv1.Envelope{
				SiteId:    site.ID.String(),
				ValidFrom: timestamppb.New(start), ValidTo: timestamppb.New(start.Add(interval)),
				// Whole watts: the search is precise to one.
				ExportLimitW: math.Floor(result.ExportW[i]), ImportLimitW: math.Floor(result.ImportW[i]),
				ExportBinding: exportBinding, ExportBindingElement: exportElement,
				ImportBinding: importBinding, ImportBindingElement: importElement,
			}
			if site.ExportCapW > 0 {
				summary.MinExportW = min(summary.MinExportW, e.GetExportLimitW())
				summary.MaxExportW = max(summary.MaxExportW, e.GetExportLimitW())
			}
			state.ExportLimitTotalW += e.GetExportLimitW()
			state.ImportLimitTotalW += e.GetImportLimitW()
			batch = append(batch, e)
		}
		// Whole intervals in a batch, so a partial publish never leaves an
		// interval half written.
		if len(batch)+summary.Sites > r.MaxBatch {
			batches, batch = append(batches, batch), nil
		}
	}
	if len(batch) > 0 {
		batches = append(batches, batch)
	}
	if math.IsInf(summary.MinExportW, 1) {
		summary.MinExportW = 0
	}
	return batches, states, nil
}

// record stores the state of the feeder for each interval of the run. A run
// that was interrupted and is run again has stored them already; they are the
// same, and stay as they are.
func (r *Runner) record(ctx context.Context, runID string, intervals []*doelabv1.EnvelopeRunInterval) error {
	_, err := r.API.Runs.CreateEnvelopeRunIntervals(ctx, connect.NewRequest(&doelabv1.CreateEnvelopeRunIntervalsRequest{
		EnvelopeRunId: runID, Intervals: intervals,
	}))
	if err != nil && connect.CodeOf(err) != connect.CodeAlreadyExists {
		return fmt.Errorf("record intervals: %w", err)
	}
	return nil
}

// publish sends the batches. Each has a key derived from the run's, so a run
// that is interrupted and run again replays the batches it had sent and
// continues with the rest.
func (r *Runner) publish(ctx context.Context, runID, key string, batches [][]*doelabv1.Envelope, summary *Summary) error {
	for i, batch := range batches {
		res, err := r.API.Envelopes.PublishEnvelopes(ctx, connect.NewRequest(&doelabv1.PublishEnvelopesRequest{
			EnvelopeRunId: runID, IdempotencyKey: fmt.Sprintf("%s:batch%d", key, i), Envelopes: batch,
		}))
		if err != nil {
			return fmt.Errorf("publish batch %d of %d: %w", i+1, len(batches), err)
		}
		summary.Published += int(res.Msg.GetPublished())
		summary.Superseded += int(res.Msg.GetSuperseded())
	}
	return nil
}

// Clock is feeder time, as the scheduler needs it.
type Clock interface {
	Now() time.Time
	Until(t time.Time) time.Duration
}

// Loop runs the engine on a rolling horizon until ctx ends: once now, for the
// horizon that starts with the interval in force, and again every `every`
// intervals of feeder time. Each run's horizon is far longer than the gap
// between runs, so every interval has an envelope well before it begins, and
// a later run replaces the forecast of an earlier one.
//
// A run that fails is logged and the loop carries on: the envelopes already
// published stay in force, and the next run tries again.
func (r *Runner) Loop(ctx context.Context, clock Clock, every int, after func(time.Duration) <-chan time.Time) error {
	for {
		now := clock.Now()
		summary, err := r.Run(ctx, now, "")
		switch {
		case ctx.Err() != nil:
			return nil
		case err != nil:
			r.Log.ErrorContext(ctx, "run failed", "err", err, "run", summary.RunID)
		default:
			r.Log.InfoContext(ctx, "run", "run", summary.RunID, "from", summary.From.Format(time.RFC3339),
				"intervals", summary.Intervals, "sites", summary.Sites, "published", summary.Published,
				"superseded", summary.Superseded, "skipped", summary.Skipped,
				"min_export_w", summary.MinExportW, "max_export_w", summary.MaxExportW,
				"ms", summary.Duration.Milliseconds())
		}

		// The next run is due when feeder time reaches the boundary `every`
		// intervals after the one this run started on. Without a successful
		// load there is no interval length to go by; try again shortly.
		wait := 5 * time.Second
		if summary.Intervals > 0 {
			interval := summary.To.Sub(summary.From) / time.Duration(summary.Intervals)
			wait = clock.Until(summary.From.Add(time.Duration(every) * interval))
		}
		select {
		case <-ctx.Done():
			return nil
		case <-after(wait):
		}
	}
}
