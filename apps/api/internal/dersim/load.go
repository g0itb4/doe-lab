package dersim

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"

	doelabv1 "doelab/api/gen/doelab/v1"
)

// Load holds many subscriptions open at once, and measures how long an
// envelope takes to reach them: the load test of the dispatch path.
//
// The subscribers are spread over the sites of the feeder that take part in
// envelopes, many to a site, each with its site's own token. They only
// listen; the fleet of devices is a separate process.
type Load struct {
	API *API
	// FeederCode is the feeder whose sites are subscribed to.
	FeederCode string
	// Token returns the device token of the site at an NMI.
	Token func(nmi string) string
	// Subscribers is how many subscriptions to hold open.
	Subscribers int
	// Rate is how many subscriptions are opened a second while the load
	// builds. The API's rate limit must allow it.
	Rate int
	// Timeout is how long one unary call may take, on the wall clock.
	Timeout time.Duration
	Log     *slog.Logger
	// after and now are time.After and time.Now, replaceable in tests.
	after func(time.Duration) <-chan time.Time
	now   func() time.Time

	mu        sync.Mutex
	connected int
	failed    int
	messages  int
	latencies []time.Duration
}

// NewLoad builds a load of n subscribers with the defaults.
func NewLoad(api *API, feederCode string, token func(nmi string) string, n int, log *slog.Logger) *Load {
	return &Load{
		API: api, FeederCode: feederCode, Token: token, Subscribers: n, Rate: 500,
		Timeout: 10 * time.Second, Log: log, after: time.After, now: time.Now,
	}
}

// LoadReport is what a load measured.
type LoadReport struct {
	// Subscribers is how many subscriptions were asked for, Connected how
	// many received their first message, and Failed how many ended in an
	// error.
	Subscribers int
	Connected   int
	Failed      int
	// Messages is every message received, keepalives included.
	Messages int
	// Dispatches is the number of latency samples: envelopes that replaced
	// the one a subscriber held for the same interval.
	Dispatches int
	// The latency from the write of an envelope to its arrival.
	P50, P90, P99, Max time.Duration
}

// Run opens the subscriptions, holds them for `hold` once they are all
// started, and reports what they measured. It ends early, with what it has,
// when ctx ends.
func (l *Load) Run(ctx context.Context, hold time.Duration) (LoadReport, error) {
	_, sites, err := l.API.feederSites(ctx, l.Timeout, l.FeederCode)
	if err != nil {
		return LoadReport{}, err
	}
	var nmis []string
	for _, site := range sites {
		if site.ExportCapW > 0 {
			nmis = append(nmis, site.NMI)
		}
	}
	if len(nmis) == 0 {
		return LoadReport{}, fmt.Errorf("feeder %s has no site that takes part in envelopes", l.FeederCode)
	}

	ctx, stop := context.WithCancel(ctx)
	var subscribers sync.WaitGroup
	// Open them in steps of Rate a second.
ramp:
	for i := range l.Subscribers {
		if i > 0 && i%l.Rate == 0 {
			l.Log.InfoContext(ctx, "opening subscriptions", "started", i, "connected", l.Connected())
			select {
			case <-ctx.Done():
				break ramp
			case <-l.after(time.Second):
			}
		}
		subscribers.Go(func() { l.subscribe(ctx, nmis[i%len(nmis)]) })
	}
	select {
	case <-ctx.Done():
	case <-l.after(hold):
	}
	stop()
	subscribers.Wait()
	return l.report(), nil
}

// subscribe holds one subscription until ctx ends, and times each envelope
// that replaces the one it holds for the same interval. An envelope that
// arrives because the interval turned is not timed: it was written long
// before.
func (l *Load) subscribe(ctx context.Context, nmi string) {
	req := connect.NewRequest(&doelabv1.SubscribeEnvelopesRequest{Nmi: nmi})
	req.Header().Set("Authorization", "Bearer "+l.Token(nmi))
	stream, err := l.API.Envelopes.SubscribeEnvelopes(ctx, req)
	if err != nil {
		l.count(func() { l.failed++ })
		return
	}
	defer stream.Close() //nolint:errcheck // the stream has ended; there is nothing to do about a close that fails
	var held *doelabv1.Envelope
	first := true
	for stream.Receive() {
		arrived := l.now()
		envelope := stream.Msg().GetEnvelope()
		l.count(func() {
			l.messages++
			if first {
				l.connected++
			}
			if held != nil && envelope != nil && envelope.GetId() != held.GetId() &&
				envelope.GetValidFrom().AsTime().Equal(held.GetValidFrom().AsTime()) {
				l.latencies = append(l.latencies, arrived.Sub(envelope.GetCreatedAt().AsTime()))
			}
		})
		first = false
		if envelope != nil {
			held = envelope
		}
	}
	if err := stream.Err(); err != nil && ctx.Err() == nil {
		l.count(func() { l.failed++ })
	}
}

func (l *Load) count(change func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	change()
}

// Connected is the number of subscriptions that have received a message.
func (l *Load) Connected() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.connected
}

// Dispatches is the number of latency samples so far.
func (l *Load) Dispatches() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.latencies)
}

func (l *Load) report() LoadReport {
	l.mu.Lock()
	defer l.mu.Unlock()
	sorted := slices.Sorted(slices.Values(l.latencies))
	return LoadReport{
		Subscribers: l.Subscribers, Connected: l.connected, Failed: l.failed, Messages: l.messages,
		Dispatches: len(sorted),
		P50:        percentile(sorted, 50), P90: percentile(sorted, 90), P99: percentile(sorted, 99), Max: percentile(sorted, 100),
	}
}

// percentile is the nearest-rank percentile of sorted samples: the smallest
// sample that at least p percent of the samples do not exceed. Zero for no
// samples.
func percentile(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := (len(sorted)*p + 99) / 100
	return sorted[max(rank, 1)-1]
}

// Process is what the API's process looks like from its metrics.
type Process struct {
	ResidentBytes float64
	HeapBytes     float64
	Goroutines    float64
	Subscriptions float64
}

// ScrapeProcess reads the API's memory, goroutines and open subscriptions
// from its /metrics page.
func ScrapeProcess(ctx context.Context, client *http.Client, url string) (Process, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Process{}, err
	}
	res, err := client.Do(req)
	if err != nil {
		return Process{}, err
	}
	defer res.Body.Close() //nolint:errcheck // read to the end below
	if res.StatusCode != http.StatusOK {
		return Process{}, fmt.Errorf("%s answered %s", url, res.Status)
	}
	var p Process
	found := 0
	fields := map[string]*float64{
		"process_resident_memory_bytes": &p.ResidentBytes,
		"go_memstats_heap_inuse_bytes":  &p.HeapBytes,
		"go_goroutines":                 &p.Goroutines,
		"doelab_subscriptions_active":   &p.Subscriptions,
	}
	lines := bufio.NewScanner(res.Body)
	lines.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for lines.Scan() {
		line := lines.Text()
		// "name value" or "name{labels} value".
		name, _, _ := strings.Cut(line, "{")
		name, _, _ = strings.Cut(name, " ")
		target, wanted := fields[name]
		if !wanted {
			continue
		}
		value, err := strconv.ParseFloat(line[strings.LastIndexByte(line, ' ')+1:], 64)
		if err != nil {
			return Process{}, fmt.Errorf("metric %s: %w", name, err)
		}
		*target = value
		found++
	}
	if err := lines.Err(); err != nil {
		return Process{}, err
	}
	if found == 0 {
		return Process{}, fmt.Errorf("%s has none of the process metrics", url)
	}
	return p, nil
}
