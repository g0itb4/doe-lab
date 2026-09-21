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

	"doelab/api/internal/auth"
	"doelab/api/internal/config"
	"doelab/api/internal/controller"
	"doelab/api/internal/interceptor"
	"doelab/api/internal/repo/pg"
	"doelab/api/internal/server"
	"doelab/api/internal/service"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
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

	srv := server.New(cfg, log, server.Deps{
		Feeders:         controller.NewFeeders(service.NewFeeders(store)),
		Sites:           controller.NewSites(service.NewSites(store)),
		Devices:         controller.NewDevices(service.NewDevices(store)),
		EnvelopeConfigs: controller.NewEnvelopeConfigs(service.NewEnvelopeConfigs(store)),
		Database:        pool,
		Validator:       validator,
		Tokens:          auth.NewTokens(cfg.EngineToken, cfg.OperatorToken, cfg.DeviceTokenSecret),
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
	// responses fail on a connection that has already gone.
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
