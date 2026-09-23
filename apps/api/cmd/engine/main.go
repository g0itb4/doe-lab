// Command engine computes operating envelopes and publishes them to the API.
//
//	engine -once            one run, for the horizon that starts now
//	engine                  a run now, then another every -every intervals
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
	feeder := flag.String("feeder", "LV10", "code of the feeder to run")
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
	runner := enginerun.NewRunner(api, *feeder, cfg.Version, log)
	runner.Workers = *workers

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
	log.Info("engine", "api", cfg.APIURL, "feeder", *feeder, "clock_speed", speed, "feeder_time", clock.Now().Format(time.RFC3339))

	if !*once {
		return runner.Loop(ctx, clock, *every, time.After)
	}
	suffix := ""
	if *force {
		suffix = ":" + time.Now().UTC().Format("150405.000")
	}
	summary, err := runner.Run(ctx, clock.Now(), suffix)
	if err != nil {
		return err
	}
	log.Info("run", "run", summary.RunID, "from", summary.From.Format(time.RFC3339), "to", summary.To.Format(time.RFC3339),
		"intervals", summary.Intervals, "sites", summary.Sites, "published", summary.Published,
		"superseded", summary.Superseded, "skipped", summary.Skipped,
		"min_export_w", summary.MinExportW, "max_export_w", summary.MaxExportW, "ms", summary.Duration.Milliseconds())
	return nil
}
