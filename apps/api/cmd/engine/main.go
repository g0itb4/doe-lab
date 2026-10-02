// Command engine computes operating envelopes and publishes them to the API.
//
//	engine -once            one run, for the horizon that starts now
//	engine                  a run now, then another every -every intervals
//	engine -feeder LV10     one feeder, not every feeder of the API
//
// It is a client of the API and nothing else: it reads the feeder and the
// forecast over RPC, and never opens the database. This file is wiring; the
// work is in internal/enginerun and internal/engine.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"doelab/api/internal/config"
	"doelab/api/internal/enginerun"
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
	feeder := flag.String("feeder", allFeeders, "code of the feeder to run, or all of them")
	once := flag.Bool("once", false, "run once and exit")
	force := flag.Bool("force", false, "with -once: run even when this horizon has already been run")
	every := flag.Int("every", 2, "intervals of feeder time between runs")
	workers := flag.Int("workers", 4, "intervals solved at once")
	flag.Parse()
	if *every < 1 {
		return fmt.Errorf("-every must be at least 1, got %d", *every)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	api := enginerun.NewAPI(&http.Client{Timeout: 60 * time.Second}, cfg.APIURL, enginerun.Token(cfg.EngineToken))

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
	codes := []string{*feeder}
	if *feeder == allFeeders {
		if codes, err = api.FeederCodes(ctx); err != nil {
			return fmt.Errorf("ask %s for its feeders: %w", cfg.APIURL, err)
		}
		// Not an idle success: under a supervisor this is tried again, and
		// the import may have run by then.
		if len(codes) == 0 {
			return fmt.Errorf("%s has no feeder to run; run the import", cfg.APIURL)
		}
	}
	log.Info("engine", "api", cfg.APIURL, "feeders", len(codes), "clock_speed", speed, "feeder_time", clock.Now().Format(time.RFC3339))

	runner := func(code string) *enginerun.Runner {
		r := enginerun.NewRunner(api, code, cfg.Version, log.With("feeder", code))
		r.Workers = *workers
		return r
	}
	if !*once {
		// Each feeder on its own schedule: they share nothing but the API.
		var loops sync.WaitGroup
		for _, code := range codes {
			loops.Go(func() { _ = runner(code).Loop(ctx, clock, *every, time.After) })
		}
		loops.Wait()
		return nil
	}
	suffix := ""
	if *force {
		suffix = ":" + time.Now().UTC().Format("150405.000")
	}
	for _, code := range codes {
		summary, err := runner(code).Run(ctx, clock.Now(), suffix)
		if err != nil {
			return fmt.Errorf("feeder %s: %w", code, err)
		}
		log.Info("run", "feeder", code, "run", summary.RunID, "from", summary.From.Format(time.RFC3339), "to", summary.To.Format(time.RFC3339),
			"intervals", summary.Intervals, "sites", summary.Sites, "published", summary.Published,
			"superseded", summary.Superseded, "skipped", summary.Skipped,
			"min_export_w", summary.MinExportW, "max_export_w", summary.MaxExportW, "ms", summary.Duration.Milliseconds())
	}
	return nil
}

// allFeeders is what -feeder says to run every feeder that the API has.
const allFeeders = "all"
