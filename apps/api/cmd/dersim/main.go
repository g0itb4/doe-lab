// Command dersim simulates the devices of a feeder: a virtual inverter for
// every site that has one. Each subscribes to its operating envelope, plays
// back its load and its sun, keeps its export inside the envelope, and
// reports what it does.
//
//	dersim                         every device behaves
//	dersim -rogue 0.05 -flaky 0.05 one in twenty ignores its limit, and one
//	                               in twenty goes silent from time to time
//	dersim -load 10000             a load test: hold that many subscriptions
//	                               open, and time each envelope's arrival
//
// It is a client of the API and nothing else. This file is wiring; the work
// is in internal/dersim.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // the feeder's zone, on a host with no zone database

	"doelab/api/internal/auth"
	"doelab/api/internal/config"
	"doelab/api/internal/dersim"
	"doelab/api/internal/simclock"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	feeder := flag.String("feeder", "LV10", "code of the feeder whose devices to simulate")
	rogue := flag.Float64("rogue", 0, "share of the devices that ignore their envelope, 0 to 1")
	flaky := flag.Float64("flaky", 0, "share of the devices that go silent from time to time, 0 to 1")
	every := flag.Duration("every", dersim.DefaultEvery, "feeder time between two readings of a device")
	fallback := flag.Float64("default-export", dersim.DefaultExportW, "export limit, in watts, of a device with no envelope")
	seed := flag.Uint64("seed", 1, "seed of the fleet's randomness")
	load := flag.Int("load", 0, "load test: hold this many subscriptions open instead of simulating the devices")
	rate := flag.Int("rate", 500, "with -load: subscriptions opened a second")
	hold := flag.Duration("hold", 2*time.Minute, "with -load: how long to hold the subscriptions once they are all started")
	metrics := flag.String("metrics", "http://127.0.0.1:9464/metrics", "with -load: the API's metrics page, for its memory; empty to skip")
	flag.Parse()
	if *rogue < 0 || *rogue > 1 || *flaky < 0 || *flaky > 1 || *rogue+*flaky > 1 {
		return fmt.Errorf("-rogue and -flaky are shares from 0 to 1 that sum to at most 1, got %v and %v", *rogue, *flaky)
	}
	if *every < time.Second {
		return fmt.Errorf("-every must be at least 1s, got %v", *every)
	}
	if *load < 0 || *rate < 1 {
		return fmt.Errorf("-load must not be negative and -rate must be at least 1, got %d and %d", *load, *rate)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	api := dersim.NewAPI(client(cfg.APIURL), cfg.APIURL)
	// The API owns feeder time. Ask it, so the engine, the devices and the
	// API agree on what "now" is.
	anchor, speed, err := api.ClockSettings(ctx)
	if err != nil {
		return fmt.Errorf("ask %s for the clock: %w", cfg.APIURL, err)
	}
	clock, err := simclock.New(anchor, speed)
	if err != nil {
		return err
	}

	// A device's token is derived from its NMI, as a commissioning system
	// would hand it out.
	tokens := auth.NewTokens(cfg.EngineToken, cfg.OperatorToken, cfg.DeviceTokenSecret)
	if *load > 0 {
		return runLoad(ctx, log, api, tokens, *feeder, *load, *rate, *hold, *metrics)
	}
	fleet := dersim.NewFleet(api, *feeder, tokens.DeviceToken, log)
	fleet.Every, fleet.DefaultExportW = *every, *fallback
	fleet.RogueFraction, fleet.FlakyFraction, fleet.Seed = *rogue, *flaky, *seed
	if err := fleet.Connect(ctx); err != nil {
		return err
	}
	defer fleet.Close()
	log.Info("dersim", "api", cfg.APIURL, "feeder", *feeder, "sites", fleet.Units(), "rogue", *rogue, "flaky", *flaky,
		"every", every.String(), "clock_speed", speed, "feeder_time", clock.Now().Format(time.RFC3339))

	fleet.Run(ctx, clock)
	return nil
}

// client returns the HTTP client of the fleet. It has no timeout of its own:
// a subscription and a stream of readings stay open, and the fleet bounds
// its unary calls itself.
//
// Every device holds two streams, so the fleet needs HTTP/2, where they
// share one connection. Over TLS that is negotiated; to a plaintext URL the
// client speaks HTTP/2 with prior knowledge, which the API accepts.
func client(apiURL string) *http.Client {
	protocols := new(http.Protocols)
	if strings.HasPrefix(apiURL, "http://") {
		protocols.SetUnencryptedHTTP2(true)
	} else {
		protocols.SetHTTP2(true)
	}
	return &http.Client{Transport: &http.Transport{
		Protocols: protocols,
		// A connection that has gone quiet is probed, and dropped when the
		// probe gets no answer, so a dead API is noticed.
		HTTP2: &http.HTTP2Config{SendPingTimeout: 15 * time.Second, PingTimeout: 10 * time.Second},
	}}
}

// runLoad is the load test: it holds n subscriptions open, and prints how
// long envelopes took to reach them and what the API's process looked like
// with them open.
func runLoad(ctx context.Context, log *slog.Logger, api *dersim.API, tokens *auth.Tokens, feeder string, n, rate int, hold time.Duration, metrics string) error {
	l := dersim.NewLoad(api, feeder, tokens.DeviceToken, n, log)
	l.Rate = rate
	scrape := func(when string) {
		if metrics == "" {
			return
		}
		p, err := dersim.ScrapeProcess(ctx, http.DefaultClient, metrics)
		if err != nil {
			log.Warn("the API's metrics", "err", err)
			return
		}
		log.Info("api process", "when", when, "subscriptions", p.Subscriptions, "goroutines", p.Goroutines,
			"resident_mb", fmt.Sprintf("%.0f", p.ResidentBytes/(1<<20)), "heap_mb", fmt.Sprintf("%.0f", p.HeapBytes/(1<<20)))
	}
	scrape("before")
	log.Info("load", "feeder", feeder, "subscribers", n, "rate", rate, "hold", hold.String())

	// The process is looked at while the subscriptions are open: shortly
	// before the hold ends.
	watching, stop := context.WithCancel(ctx)
	defer stop()
	go func() {
		ramp := time.Duration(n/rate) * time.Second
		select {
		case <-watching.Done():
		case <-time.After(ramp + hold*9/10):
			scrape("under load")
		}
	}()

	report, err := l.Run(ctx, hold)
	if err != nil {
		return err
	}
	log.Info("load report", "subscribers", report.Subscribers, "connected", report.Connected, "failed", report.Failed,
		"messages", report.Messages, "dispatches", report.Dispatches,
		"p50_ms", ms(report.P50), "p90_ms", ms(report.P90), "p99_ms", ms(report.P99), "max_ms", ms(report.Max))
	return nil
}

func ms(d time.Duration) string { return fmt.Sprintf("%.1f", float64(d.Microseconds())/1000) }
