package engine

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"
)

// intervalsPerDay is the number of 30-minute intervals in a day.
const intervalsPerDay = 48

// dayBudget is the time allowed for one day of envelopes on LV10: 48
// intervals, an export and an import search each, 94 sites, 223 buses.
//
// Measured 2026-09-20 on an Apple M-series laptop: about 0.25 s, single
// threaded (see `just bench`). The budget is four times that, to absorb a
// slower CI runner without hiding a real regression: the dense solver, or a
// search that lost its warm start, would miss it by an order of magnitude.
const dayBudget = time.Second

// syntheticDay builds 48 intervals with a household shape: a morning bump and
// an evening peak in the passive sites' load. Sites with DER keep the same
// caps all day.
func syntheticDay(sites int) [][]SiteInput {
	rng := rand.New(rand.NewPCG(2026, 1012))
	hasDER := make([]bool, sites)
	scale := make([]float64, sites)
	for i := range hasDER {
		hasDER[i] = rng.Float64() < 0.6
		scale[i] = 0.5 + rng.Float64()
	}
	day := make([][]SiteInput, intervalsPerDay)
	for k := range day {
		hour := float64(k) / 2
		morning, evening := hour-7.5, hour-18.5
		shape := 300 + 500*math.Exp(-morning*morning/4) + 1700*math.Exp(-evening*evening/6)
		day[k] = make([]SiteInput, sites)
		for i := range day[k] {
			if hasDER[i] {
				day[k][i] = SiteInput{ExportCapW: 10000, ImportCapW: 14000}
				continue
			}
			p := shape * scale[i]
			day[k][i].Base = complex(p, p*math.Tan(math.Acos(0.95)))
		}
	}
	return day
}

// runDay computes the envelopes of every interval, one after the other, and
// returns how many intervals the network limited, rather than the sites' caps.
func runDay(tb testing.TB, e *Engine, day [][]SiteInput) (limited int) {
	tb.Helper()
	var sol Solution
	for _, interval := range day {
		r, err := e.Envelope(interval, &sol)
		if err != nil {
			tb.Fatal(err)
		}
		if r.Export.Constraint != ConstraintSiteCap {
			limited++
		}
	}
	return limited
}

func BenchmarkDay(b *testing.B) {
	net := lv10Demo()
	e := mustEngine(b, net, lv10Config())
	day := syntheticDay(len(net.Sites))
	b.ReportAllocs()
	for b.Loop() {
		runDay(b, e, day)
	}
	b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(b.N)/intervalsPerDay, "µs/interval")
}

func BenchmarkSolve(b *testing.B) {
	for name, method := range map[string]Method{"tree": Tree, "dense": Dense} {
		b.Run(name, func(b *testing.B) {
			net := lv10()
			pf, err := NewPowerFlow(net, method)
			if err != nil {
				b.Fatal(err)
			}
			var snap snapshot
			readJSON(b, "testdata/lv10_peak.json", &snap)
			load := loadsFor(b, net, &snap)
			b.ReportAllocs()
			for b.Loop() {
				var sol Solution
				if err := pf.Solve(load, &sol); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// TestDayBudget fails when a day of envelopes takes longer than dayBudget. It
// measures wall time, so it runs only in a plain build: not under the race
// detector, not with coverage, and not in -short mode.
func TestDayBudget(t *testing.T) {
	if raceEnabled || testing.Short() || testing.CoverMode() != "" {
		t.Skip("timing is only meaningful in a plain build; run `just bench`")
	}
	net := lv10Demo()
	e := mustEngine(t, net, lv10Config())
	day := syntheticDay(len(net.Sites))

	// The best of three, so one scheduling hiccup does not fail the build.
	best := time.Duration(math.MaxInt64)
	limited := 0
	for range 3 {
		start := time.Now()
		limited = runDay(t, e, day)
		best = min(best, time.Since(start))
	}
	t.Logf("one day on LV10: %v (budget %v); the network limited %d of %d intervals", best, dayBudget, limited, intervalsPerDay)
	if best > dayBudget {
		t.Errorf("one day of envelopes took %v, budget %v", best, dayBudget)
	}
	if limited == 0 {
		t.Error("no interval was limited by the network; the benchmark is not exercising the search")
	}
}
