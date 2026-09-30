package dersim

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"doelab/api/internal/repo/repotest"
)

func newLoad(w *world, n int) *Load {
	l := NewLoad(NewAPI(w.HTTP, w.URL), "LV10", w.Tokens.DeviceToken, n, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return l
}

// timer is the load's clock for waiting: it tells the test how long the load
// would wait, and lets it through when the test says.
type timer struct {
	waits chan time.Duration
	fire  chan time.Time
}

func newTimer(l *Load) *timer {
	tm := &timer{waits: make(chan time.Duration), fire: make(chan time.Time)}
	l.after = func(d time.Duration) <-chan time.Time {
		tm.waits <- d
		return tm.fire
	}
	return tm
}

func TestLoadMeasuresDispatch(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	const n = 20
	l := newLoad(w, n)
	l.Rate = 8
	tm := newTimer(l)

	type result struct {
		report LoadReport
		err    error
	}
	done := make(chan result, 1)
	go func() {
		report, err := l.Run(ctx, time.Minute)
		done <- result{report, err}
	}()

	// Twenty subscriptions at eight a second: two pauses of a second, then
	// the hold.
	for range 2 {
		if d := <-tm.waits; d != time.Second {
			t.Fatalf("the load waits %v between steps, want 1s", d)
		}
		tm.fire <- time.Time{}
	}
	if d := <-tm.waits; d != time.Minute {
		t.Fatalf("the load holds for %v, want 1m", d)
	}
	waitFor(t, "every subscription to connect", func() bool { return l.Connected() == n })

	// The first envelope of the interval reaches every subscriber, and is not
	// timed: a subscriber that held nothing cannot tell a dispatch from the
	// turn of an interval. The one that replaces it is.
	w.publish(t, 0, 600)
	w.publish(t, 0, 500)
	waitFor(t, "every subscriber to time the second envelope", func() bool { return l.Dispatches() == n })
	// The same envelope for the next interval is not a dispatch.
	w.publish(t, 1, 700)
	w.Clock.Advance(30 * time.Minute)
	time.Sleep(60 * time.Millisecond)

	tm.fire <- time.Time{}
	r := <-done
	if r.err != nil {
		t.Fatal(r.err)
	}
	got := r.report
	if got.Subscribers != n || got.Connected != n || got.Failed != 0 || got.Dispatches != n || got.Messages < 3*n {
		t.Errorf("report = %+v", got)
	}
	if got.P50 <= 0 || got.P50 > got.P90 || got.P90 > got.P99 || got.P99 > got.Max || got.Max > 5*time.Second {
		t.Errorf("latencies = p50 %v, p90 %v, p99 %v, max %v", got.P50, got.P90, got.P99, got.Max)
	}
}

func TestLoadCountsFailures(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)

	// A token that the API refuses: every subscription fails.
	refused := newLoad(w, 5)
	refused.Token = func(string) string { return "not-a-token" }
	report, err := refused.Run(ctx, 50*time.Millisecond)
	if err != nil || report.Failed != 5 || report.Connected != 0 || report.Dispatches != 0 || report.P99 != 0 {
		t.Errorf("with a refused token: %+v, %v", report, err)
	}

	// An API that cannot be subscribed to at all.
	deaf := newLoad(w, 3)
	deaf.API.Envelopes = noEnvelopes{}
	report, err = deaf.Run(ctx, 10*time.Millisecond)
	if err != nil || report.Failed != 3 {
		t.Errorf("with no subscriptions: %+v, %v", report, err)
	}
}

func TestLoadSetupFailures(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)

	unknown := newLoad(w, 1)
	unknown.FeederCode = "LV99"
	if _, err := unknown.Run(ctx, time.Second); err == nil || !strings.Contains(err.Error(), "feeder LV99: not_found") {
		t.Errorf("an unknown feeder: %v", err)
	}

	// A feeder whose sites take no envelopes.
	_, err := w.Store.CreateFeeder(ctx, repotest.NewFeeder("LV30"))
	noErr(t, "feeder", err)
	none := newLoad(w, 1)
	none.FeederCode = "LV30"
	if _, err := none.Run(ctx, time.Second); err == nil || !strings.Contains(err.Error(), "no site that takes part") {
		t.Errorf("a feeder with no enrolled site: %v", err)
	}
}

func TestLoadStopsWhenAsked(t *testing.T) {
	t.Parallel()
	w := newWorld(t, true)
	l := newLoad(w, 10)
	l.Rate = 2
	tm := newTimer(l)
	running, stop := context.WithCancel(ctx)
	done := make(chan LoadReport, 1)
	go func() {
		report, _ := l.Run(running, time.Hour)
		done <- report
	}()
	// In the middle of the ramp: two are started, eight are not.
	<-tm.waits
	waitFor(t, "the first two to connect", func() bool { return l.Connected() == 2 })
	stop()
	// The ramp ends; Run still asks its timer for the hold, and returns at
	// once because the context has ended.
	go func() { <-tm.waits }()
	report := <-done
	if report.Connected != 2 || report.Failed != 0 {
		t.Errorf("report = %+v, want two connected and none failed", report)
	}
}

func TestPercentile(t *testing.T) {
	t.Parallel()
	ms := func(values ...int) []time.Duration {
		out := make([]time.Duration, len(values))
		for i, v := range values {
			out[i] = time.Duration(v) * time.Millisecond
		}
		return out
	}
	if got := percentile(nil, 99); got != 0 {
		t.Errorf("of nothing = %v", got)
	}
	hundred := make([]int, 100)
	for i := range hundred {
		hundred[i] = i + 1
	}
	for p, want := range map[int]int{50: 50, 90: 90, 99: 99, 100: 100, 1: 1, 0: 1} {
		if got := percentile(ms(hundred...), p); got != time.Duration(want)*time.Millisecond {
			t.Errorf("p%d of 1..100 ms = %v, want %d ms", p, got, want)
		}
	}
	// With few samples a high percentile is the largest.
	if got := percentile(ms(5, 7, 40), 99); got != 40*time.Millisecond {
		t.Errorf("p99 of three = %v", got)
	}
	if got := percentile(ms(5, 7, 40), 50); got != 7*time.Millisecond {
		t.Errorf("p50 of three = %v", got)
	}
}

func TestScrapeProcess(t *testing.T) {
	t.Parallel()
	page := "# HELP go_goroutines Number of goroutines.\n# TYPE go_goroutines gauge\ngo_goroutines 20143\n" +
		"go_memstats_heap_inuse_bytes 1.8612224e+08\nprocess_resident_memory_bytes 3.14572800e+08\n" +
		`doelab_subscriptions_active{otel_scope_name="doelab",otel_scope_version=""} 10000` + "\n" +
		"rpc_server_call_duration_seconds_count{rpc_method=\"x\"} 7\n"
	var body string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	body = page
	p, err := ScrapeProcess(ctx, srv.Client(), srv.URL)
	if err != nil || p != (Process{ResidentBytes: 314572800, HeapBytes: 186122240, Goroutines: 20143, Subscriptions: 10000}) {
		t.Errorf("process = %+v, %v", p, err)
	}

	for name, tt := range map[string]struct {
		body   string
		status int
		url    string
		want   string
	}{
		"a page with none of the metrics": {"up 1\n", 200, srv.URL, "none of the process metrics"},
		"a value that is not a number":    {"go_goroutines many\n", 200, srv.URL, "metric go_goroutines"},
		"a line too long to read":         {"go_goroutines 1\n" + strings.Repeat("x", 2<<20) + "\n", 200, srv.URL, "token too long"},
		"an error page":                   {"no", 500, srv.URL, "500 Internal Server Error"},
		"an address that is not one":      {"", 200, "::not a url", "missing protocol scheme"},
		"nothing listening":               {"", 200, "http://127.0.0.1:1/metrics", "connect"},
	} {
		body, status = tt.body, tt.status
		if _, err := ScrapeProcess(ctx, srv.Client(), tt.url); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v, want %q", name, err, tt.want)
		}
	}
}
