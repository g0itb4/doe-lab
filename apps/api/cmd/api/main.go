// Command api serves the RPC surface: Connect, gRPC and gRPC-Web on one port.
//
// This file is wiring and nothing else: config, pool, repositories, services,
// controllers, server, signals. Every decision it looks like it is making was
// made in the package it is calling.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // the feeder's zone, on a host with no zone database

	"connectrpc.com/connect"

	"doelab/api/internal/auth"
	"doelab/api/internal/config"
	"doelab/api/internal/controller"
	"doelab/api/internal/interceptor"
	"doelab/api/internal/obs"
	"doelab/api/internal/repo/llm"
	"doelab/api/internal/repo/objstore"
	"doelab/api/internal/repo/pg"
	"doelab/api/internal/repo/pgbus"
	"doelab/api/internal/server"
	"doelab/api/internal/service"
	"doelab/api/internal/simclock"
)

func main() {
	// A line that is logged with a request's context carries its trace id.
	log := slog.New(obs.LogHandler(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pg.NewPool(ctx, cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()

	// Compile the CEL rules once, at startup. A failure here is a malformed
	// annotation in a .proto file.
	validator, err := interceptor.NewValidator()
	if err != nil {
		return err
	}

	// The dependency arrow points inwards the whole way: pg implements the
	// interfaces that service declares, and service knows nothing about pg.
	store := pg.NewStore(pool)

	clock, err := simclock.New(cfg.ClockAnchor, cfg.ClockSpeed)
	if err != nil {
		return err
	}
	// LISTEN/NOTIFY, so several API instances stay in step. It reconnects on
	// its own; a subscription that misses a signal catches up at its next
	// keepalive.
	envelopeBus := pgbus.New(pool, log)
	go func() { _ = envelopeBus.Run(ctx) }()

	// Metrics for a scraper on this host, and a trace for every RPC.
	tel, err := obs.New(ctx, obs.Options{
		Service: "doelab-api", Version: cfg.Version, Env: string(cfg.Env), OTLPEndpoint: cfg.OTLPEndpoint,
	})
	if err != nil {
		return err
	}
	metrics := &http.Server{
		Addr: cfg.MetricsAddr, Handler: metricsMux(tel.Handler),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := metrics.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("metrics listener", "addr", cfg.MetricsAddr, "err", err)
		}
	}()

	compliance := service.NewCompliance(store, clock)
	compliance.Metrics = tel.Recorder
	backstops := service.NewBackstops(store, envelopeBus, clock)
	telemetry := service.NewTelemetry(store, clock, compliance)
	telemetry.Metrics = tel.Recorder
	envelopes := service.NewEnvelopes(store, envelopeBus, clock)
	envelopes.Metrics = tel.Recorder
	runs := service.NewEnvelopeRuns(store)
	runs.Metrics, runs.Objects = tel.Recorder, objstore.New(cfg.S3)
	// The fleet summary moves once a minute of feeder time, and at least
	// four times a second of wall time.
	telemetry.WatchEvery = min(time.Second, max(250*time.Millisecond, clock.Real(time.Minute)))
	go sweep(ctx, log, clock, compliance, backstops)
	retention := service.NewRetention(store, clock, service.Keep{
		Readings: cfg.KeepReadings, Envelopes: cfg.KeepEnvelopes, Alerts: cfg.KeepAlerts,
	})
	go retain(ctx, log, retention)

	// No key, no model, and the assistant says that it is off.
	var model service.Model
	if cfg.AnthropicAPIKey != "" {
		model = llm.New(cfg.AnthropicAPIKey, log)
	}
	assistant := service.NewAssistant(store, clock, model, cfg.AssistantDailyBudgetMicroUSD)

	// Ends when the server begins to shut down, and the open streams with it.
	stopping, stopStreams := context.WithCancel(context.Background())
	defer stopStreams()

	srv := server.New(cfg, log, server.Deps{
		Feeders:         controller.NewFeeders(service.NewFeeders(store)),
		Sites:           controller.NewSites(service.NewSites(store)),
		Devices:         controller.NewDevices(service.NewDevices(store)),
		EnvelopeConfigs: controller.NewEnvelopeConfigs(service.NewEnvelopeConfigs(store)),
		EnvelopeRuns:    controller.NewEnvelopeRuns(runs),
		Envelopes:       controller.NewEnvelopes(envelopes),
		Clock:           controller.NewClock(clock),
		Telemetry:       controller.NewTelemetry(telemetry),
		Alerts:          controller.NewAlerts(service.NewAlerts(store)),
		Backstops:       controller.NewBackstops(backstops),
		Assistant:       controller.NewAssistant(assistant, cfg.TrustProxy),
		Database:        pool,
		Validator:       validator,
		Tokens:          auth.NewTokens(cfg.EngineToken, cfg.OperatorToken, cfg.DeviceTokenSecret),
		Stopping:        stopping,
		Extra:           []connect.Interceptor{tel.Interceptor},
	})

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr, "version", cfg.Version, "env", string(cfg.Env))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	// Drain in-flight requests before closing the pool, or the last few
	// responses fail on a connection that has already gone. The server
	// streams are told to end first: a subscription never finishes by itself,
	// and Shutdown would wait for it.
	log.Info("shutting down")
	stopStreams()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	// The last spans are sent, and the metrics listener closed, whatever the
	// API's own shutdown said.
	_ = metrics.Shutdown(shutdownCtx)
	_ = tel.Shutdown(shutdownCtx)
	if errors.Is(err, context.DeadlineExceeded) {
		// What is left is a device that still holds a stream of readings
		// open. It is cut, and sends again to the next process: a reading
		// that was already stored is skipped.
		log.Info("closing the connections that are still open")
		return srv.Close()
	}
	return err
}

// metricsMux serves the metrics, and nothing else, on the metrics listener.
func metricsMux(metrics http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", metrics)
	return mux
}

// retain removes old history until ctx ends: a minute after the start, so a
// restart loop cannot hammer the database, and every ten minutes after. A
// sweep that finds nothing to remove costs a few index reads.
func retain(ctx context.Context, log *slog.Logger, retention *service.Retention) {
	wait := time.Minute
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = 10 * time.Minute
		removed, err := retention.Sweep(ctx)
		switch {
		case err != nil:
			log.WarnContext(ctx, "retention sweep failed", "err", err)
		case removed > 0:
			log.InfoContext(ctx, "retention", "removed", removed)
		}
	}
}

// sweep runs the periodic work until ctx ends: it looks for devices that have
// gone silent, and keeps an active backstop covering its feeder's horizon. A
// minute of feeder time between sweeps, and never less than a second.
func sweep(ctx context.Context, log *slog.Logger, clock *simclock.Clock, compliance *service.Compliance, backstops *service.Backstops) {
	every := max(time.Second, clock.Real(time.Minute))
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if opened, err := compliance.Sweep(ctx); err != nil {
			log.WarnContext(ctx, "compliance sweep failed", "err", err)
		} else if opened > 0 {
			log.InfoContext(ctx, "devices offline", "alerts_opened", opened)
		}
		if _, err := backstops.Extend(ctx); err != nil {
			log.WarnContext(ctx, "backstop extension failed", "err", err)
		}
	}
}
