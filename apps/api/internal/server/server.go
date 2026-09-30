// Package server mounts the handlers and owns the HTTP surface.
package server

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/grpchealth"
	"connectrpc.com/grpcreflect"

	"doelab/api/gen/doelab/v1/doelabv1connect"
	"doelab/api/internal/auth"
	"doelab/api/internal/config"
	"doelab/api/internal/interceptor"
)

// Pinger reports whether the database can serve. *pgxpool.Pool is one.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps is everything the server mounts or calls.
type Deps struct {
	Feeders         doelabv1connect.FeederServiceHandler
	Sites           doelabv1connect.SiteServiceHandler
	Devices         doelabv1connect.DeviceServiceHandler
	EnvelopeConfigs doelabv1connect.EnvelopeConfigServiceHandler
	EnvelopeRuns    doelabv1connect.EnvelopeRunServiceHandler
	Envelopes       doelabv1connect.EnvelopeServiceHandler
	Clock           doelabv1connect.ClockServiceHandler
	Telemetry       doelabv1connect.TelemetryServiceHandler
	Alerts          doelabv1connect.AlertServiceHandler
	Backstops       doelabv1connect.BackstopServiceHandler

	// Database answers the health check.
	Database  Pinger
	Validator interceptor.Validator
	Tokens    *auth.Tokens
	// Extra interceptors run outermost, before Logging. Tracing goes here.
	Extra []connect.Interceptor
	// Stopping ends when the server begins to shut down, and the open streams
	// with it. Unset, the streams are never told to end.
	Stopping context.Context
}

// services names every service the server mounts, for health and reflection.
var services = []string{
	doelabv1connect.FeederServiceName,
	doelabv1connect.SiteServiceName,
	doelabv1connect.DeviceServiceName,
	doelabv1connect.EnvelopeConfigServiceName,
	doelabv1connect.EnvelopeRunServiceName,
	doelabv1connect.EnvelopeServiceName,
	doelabv1connect.ClockServiceName,
	doelabv1connect.TelemetryServiceName,
	doelabv1connect.AlertServiceName,
	doelabv1connect.BackstopServiceName,
}

// Handler builds the mux: every service with the interceptor chain, health,
// and reflection in development.
func Handler(cfg config.Config, log *slog.Logger, deps Deps) http.Handler {
	mux := http.NewServeMux()

	chain := append([]connect.Interceptor{}, deps.Extra...)
	// Applied outermost first, and the order is load-bearing: Logging sees
	// the final code; Timeout covers everything below it; Errors is the one
	// place a message is scrubbed; Throttle refuses before any work is done;
	// Auth runs before Validate so field-level messages go only to a caller
	// who may use the procedure; Validate is innermost so no handler sees a
	// message that failed. Drain sits under Logging, so a stream that a
	// shutdown ended is logged as ended.
	chain = append(chain,
		interceptor.Logging(log, cfg.LogRequests),
		interceptor.Timeout(cfg.RequestTimeout),
		interceptor.Drain(cmp.Or(deps.Stopping, context.Background())),
		interceptor.Errors(log),
		interceptor.NewThrottle(cfg.TrustProxy, cfg.RateLimitPerSecond, cfg.RateLimitBurst),
		interceptor.Auth(deps.Tokens, Policy()),
		interceptor.Validate(deps.Validator),
	)

	opts := []connect.HandlerOption{
		// Not an interceptor: WithRecover wraps the ENTIRE chain, so it
		// catches a panic in an interceptor as well as in a handler.
		connect.WithRecover(func(ctx context.Context, spec connect.Spec, _ http.Header, p any) error {
			log.ErrorContext(ctx, "panic",
				"procedure", spec.Procedure, "panic", p, "stack", string(debug.Stack()))
			return connect.NewError(connect.CodeInternal, errors.New("internal error"))
		}),
		connect.WithInterceptors(chain...),
		connect.WithCompressMinBytes(1024),
		// Connect buffers a whole message before any interceptor runs, so
		// this is the only bound on an oversized body, and the only bound on
		// DECOMPRESSED size: it turns a gzip bomb into a bounded waste.
		connect.WithReadMaxBytes(4 << 20),
	}

	// Mount every service TWICE.
	//
	// At the root because that is what gRPC requires: a stub computes its
	// path as /package.Service/Method and cannot be told otherwise.
	//
	// Under /rpc because that is the same-origin convention the web app and
	// Vite's proxy are built around. StripPrefix is safe because the Connect
	// handler matches on the remaining path.
	mount := func(path string, h http.Handler) {
		mux.Handle(path, h)
		mux.Handle("/rpc"+path, http.StripPrefix("/rpc", h))
	}
	mount(doelabv1connect.NewFeederServiceHandler(deps.Feeders, opts...))
	mount(doelabv1connect.NewSiteServiceHandler(deps.Sites, opts...))
	mount(doelabv1connect.NewDeviceServiceHandler(deps.Devices, opts...))
	mount(doelabv1connect.NewEnvelopeConfigServiceHandler(deps.EnvelopeConfigs, opts...))
	mount(doelabv1connect.NewEnvelopeRunServiceHandler(deps.EnvelopeRuns, opts...))
	mount(doelabv1connect.NewEnvelopeServiceHandler(deps.Envelopes, opts...))
	mount(doelabv1connect.NewClockServiceHandler(deps.Clock, opts...))
	mount(doelabv1connect.NewTelemetryServiceHandler(deps.Telemetry, opts...))
	mount(doelabv1connect.NewAlertServiceHandler(deps.Alerts, opts...))
	mount(doelabv1connect.NewBackstopServiceHandler(deps.Backstops, opts...))

	// Health is the gRPC Health service and nothing else. Connect handlers
	// serve all three protocols, so an uptime checker that speaks only HTTP
	// and JSON can call
	//
	//   POST /grpc.health.v1.Health/Check
	//   content-type: application/json
	//   {"service":""}
	//
	// and read {"status":"SERVING_STATUS_SERVING"} back. The answer comes
	// from the DATABASE, so SERVING means "can serve", not "is running".
	mount(grpchealth.NewHandler(newChecker(deps.Database, services)))
	if cfg.Reflection {
		reflector := grpcreflect.NewStaticReflector(services...)
		mux.Handle(grpcreflect.NewHandlerV1(reflector))
		// grpcurl still asks for v1alpha.
		mux.Handle(grpcreflect.NewHandlerV1Alpha(reflector))
	}
	return mux
}

// New builds the http.Server around Handler.
func New(cfg config.Config, log *slog.Logger, deps Deps) *http.Server {
	return &http.Server{
		Addr:    net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Handler: Handler(cfg, log, deps),
		// Cleartext HTTP/2 beside HTTP/1.1. TLS is the edge's job; the hop
		// behind it is plaintext, and without unencrypted HTTP/2 a gRPC
		// client speaking prior-knowledge HTTP/2 gets a 400 while Connect
		// keeps working.
		Protocols: protocols(),
		// ReadHeaderTimeout bounds a slowloris that dribbles headers. There
		// is no ReadTimeout and no WriteTimeout on purpose: either would cap
		// a stream at a fixed wall clock. A unary RPC is bounded by the
		// Timeout interceptor, and its body by WithReadMaxBytes.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
}

func protocols() *http.Protocols {
	p := new(http.Protocols)
	p.SetHTTP1(true)
	p.SetUnencryptedHTTP2(true)
	return p
}

// checker answers the gRPC Health service from the database's actual state.
type checker struct {
	db    Pinger
	known map[string]struct{}
}

func newChecker(db Pinger, services []string) *checker {
	known := make(map[string]struct{}, len(services))
	for _, s := range services {
		known[s] = struct{}{}
	}
	return &checker{db: db, known: known}
}

// Check implements grpchealth.Checker.
func (c *checker) Check(ctx context.Context, req *grpchealth.CheckRequest) (*grpchealth.CheckResponse, error) {
	// An empty service name asks about the server as a whole, which is what
	// a load balancer sends.
	if req.Service != "" {
		if _, ok := c.known[req.Service]; !ok {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("unknown service "+req.Service))
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := c.db.Ping(ctx); err != nil {
		return &grpchealth.CheckResponse{Status: grpchealth.StatusNotServing}, nil
	}
	return &grpchealth.CheckResponse{Status: grpchealth.StatusServing}, nil
}
