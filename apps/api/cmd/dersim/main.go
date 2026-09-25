// Command dersim simulates the devices of a feeder: a virtual inverter for
// every site that has one. Each subscribes to its operating envelope, plays
// back its load and its sun, keeps its export inside the envelope, and
// reports what it does.
//
//	dersim                         every device behaves
//	dersim -rogue 0.05 -flaky 0.05 one in twenty ignores its limit, and one
//	                               in twenty goes silent from time to time
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
	flag.Parse()
	if *rogue < 0 || *rogue > 1 || *flaky < 0 || *flaky > 1 || *rogue+*flaky > 1 {
		return fmt.Errorf("-rogue and -flaky are shares from 0 to 1 that sum to at most 1, got %v and %v", *rogue, *flaky)
	}
	if *every < time.Second {
		return fmt.Errorf("-every must be at least 1s, got %v", *every)
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
