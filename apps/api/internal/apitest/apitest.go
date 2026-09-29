// Package apitest runs the whole API in a test: the real handler, with its
// interceptor chain, controllers and services, over an in-memory store and a
// real HTTP/2 listener.
//
// The server's own tests use it, and so do the tests of the API's clients,
// the engine and the simulated devices: they are tested against the API they
// will talk to, not against a stub of it.
package apitest

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	"doelab/api/internal/auth"
	"doelab/api/internal/config"
	"doelab/api/internal/controller"
	"doelab/api/internal/interceptor"
	"doelab/api/internal/repo/bus"
	"doelab/api/internal/repo/mem"
	"doelab/api/internal/repo/repotest"
	"doelab/api/internal/server"
	"doelab/api/internal/service"
	"doelab/api/internal/simclock"
)

// The tokens of the test API.
const (
	EngineToken   = "engine-token-for-tests"
	OperatorToken = "operator-token-for-tests"
	DeviceSecret  = "device-secret-for-tests"
)

// API is a running test API.
type API struct {
	Store *mem.Store
	Bus   *bus.Local
	// Clock is feeder time, which stands still until a test moves it.
	Clock *Clock
	// SimClock is the same clock as the services see it.
	SimClock *simclock.Clock
	// Envelopes is the envelope service, for a test that tunes its keepalive.
	Envelopes *service.Envelopes
	// Compliance and Backstops are the services the API's timer drives; a
	// test calls their sweeps itself.
	Compliance *service.Compliance
	Backstops  *service.Backstops
	Telemetry  *service.Telemetry
	// Objects is the object store that exports are written to.
	Objects *mem.Objects
	Tokens  *auth.Tokens
	// DB is the database as the health check sees it.
	DB   *FakeDB
	Deps server.Deps
	URL  string
	HTTP *http.Client
	// Stop tells the API that it is shutting down: the open streams end.
	Stop func()
	// Close stops the listener. It is also registered as a cleanup.
	Close func()
}

// FakeDB answers the health check's ping.
type FakeDB struct{ Err error }

// Ping returns the configured error.
func (f *FakeDB) Ping(context.Context) error { return f.Err }

// Clock is feeder time under a test's control. Its speed is 60, so a test
// also sees feeder time and the wall clock told apart.
type Clock struct {
	mu   sync.Mutex
	wall time.Time
}

// Anchor is where the test clock starts: the wall clock and feeder time agree
// here, at the first instant of the profile day of the fixtures.
var Anchor = repotest.Day

// Speed is the speed of the test clock.
const Speed = 60

func (c *Clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wall
}

// Advance moves feeder time forward by d.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wall = c.wall.Add(d / Speed)
}

// validator is shared: compiling the CEL rules takes a while, and the result
// is immutable.
var validator = sync.OnceValue(func() interceptor.Validator {
	v, err := interceptor.NewValidator()
	if err != nil {
		panic(err)
	}
	return v
})

// New starts an API over an empty store.
func New(t testing.TB) *API {
	t.Helper()
	store := mem.New()
	a := &API{
		Store:  store,
		Bus:    bus.NewLocal(),
		Clock:  &Clock{wall: Anchor},
		Tokens: auth.NewTokens(EngineToken, OperatorToken, DeviceSecret),
		DB:     &FakeDB{},
	}
	clock, err := simclock.NewAt(Anchor, Speed, a.Clock.now)
	if err != nil {
		t.Fatal(err)
	}
	a.SimClock = clock
	a.Envelopes = service.NewEnvelopes(store, a.Bus, clock)
	// A subscription looks again every 20 ms, so a test that moves the clock
	// sees the effect at once.
	a.Envelopes.Keepalive = 20 * time.Millisecond

	a.Compliance = service.NewCompliance(store, clock)
	a.Backstops = service.NewBackstops(store, a.Bus, clock)
	a.Telemetry = service.NewTelemetry(store, clock, a.Compliance)
	a.Telemetry.WatchEvery = 20 * time.Millisecond

	a.Objects = mem.NewObjects()
	runs := service.NewEnvelopeRuns(store)
	runs.Objects = a.Objects

	stopping, stop := context.WithCancel(context.Background())
	a.Stop = stop
	t.Cleanup(stop)

	a.Deps = server.Deps{
		Feeders:         controller.NewFeeders(service.NewFeeders(store)),
		Sites:           controller.NewSites(service.NewSites(store)),
		Devices:         controller.NewDevices(service.NewDevices(store)),
		EnvelopeConfigs: controller.NewEnvelopeConfigs(service.NewEnvelopeConfigs(store)),
		EnvelopeRuns:    controller.NewEnvelopeRuns(runs),
		Envelopes:       controller.NewEnvelopes(a.Envelopes),
		Clock:           controller.NewClock(clock),
		Telemetry:       controller.NewTelemetry(a.Telemetry),
		Alerts:          controller.NewAlerts(service.NewAlerts(store)),
		Backstops:       controller.NewBackstops(a.Backstops),
		Database:        a.DB,
		Validator:       validator(),
		Tokens:          a.Tokens,
		Stopping:        stopping,
	}
	cfg := config.Config{Env: config.Development, Reflection: true}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewUnstartedServer(server.Handler(cfg, log, a.Deps))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	a.Close = sync.OnceFunc(func() {
		srv.Client().CloseIdleConnections()
		srv.Close()
	})
	t.Cleanup(a.Close)
	a.URL, a.HTTP = srv.URL, srv.Client()
	return a
}

// As returns client options that send a bearer token. An empty token sends no
// header: an anonymous caller.
func As(token string) connect.ClientOption {
	return connect.WithInterceptors(Bearer(token))
}

// Bearer adds a token to unary calls and to streams alike.
type Bearer string

// WrapUnary adds the token to a unary call.
func (b Bearer) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if b != "" {
			req.Header().Set("Authorization", "Bearer "+string(b))
		}
		return next(ctx, req)
	}
}

// WrapStreamingClient adds the token to a stream.
func (b Bearer) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		conn := next(ctx, spec)
		if b != "" {
			conn.RequestHeader().Set("Authorization", "Bearer "+string(b))
		}
		return conn
	}
}

// WrapStreamingHandler does nothing: this is a client's interceptor.
func (b Bearer) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}
