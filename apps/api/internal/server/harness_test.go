package server_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"

	"doelab/api/gen/doelab/v1/doelabv1connect"
	"doelab/api/internal/auth"
	"doelab/api/internal/config"
	"doelab/api/internal/controller"
	"doelab/api/internal/interceptor"
	"doelab/api/internal/repo/mem"
	"doelab/api/internal/repo/repotest"
	"doelab/api/internal/server"
	"doelab/api/internal/service"
)

// api is the whole server over an in-memory store, reached through real HTTP/2
// with the generated clients: the interceptor chain, the controllers, the
// services and protomap all run as they do in production.
type api struct {
	store   *mem.Store
	fixture repotest.Fixture
	tokens  *auth.Tokens
	url     string
	http    *http.Client
	db      *fakeDB
	deps    server.Deps
}

// fakeDB is the database as the health check sees it.
type fakeDB struct{ err error }

func (f *fakeDB) Ping(context.Context) error { return f.err }

const (
	engineToken   = "engine-token-for-tests"
	operatorToken = "operator-token-for-tests"
	deviceSecret  = "device-secret-for-tests"
)

// sharedValidator: compiling the CEL rules takes a while, and the result is
// immutable.
var sharedValidator = func() interceptor.Validator {
	v, err := interceptor.NewValidator()
	if err != nil {
		panic(err)
	}
	return v
}()

func newAPI(t *testing.T) *api {
	t.Helper()
	store := mem.New()
	a := &api{
		store:   store,
		fixture: repotest.Seed(t, store, "LV10", 1),
		tokens:  auth.NewTokens(engineToken, operatorToken, deviceSecret),
		db:      &fakeDB{},
	}
	cfg := config.Config{Env: config.Development, Reflection: true}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a.deps = server.Deps{
		Feeders:         controller.NewFeeders(service.NewFeeders(store)),
		Sites:           controller.NewSites(service.NewSites(store)),
		Devices:         controller.NewDevices(service.NewDevices(store)),
		EnvelopeConfigs: controller.NewEnvelopeConfigs(service.NewEnvelopeConfigs(store)),
		Database:        a.db,
		Validator:       sharedValidator,
		Tokens:          a.tokens,
	}
	srv := httptest.NewUnstartedServer(server.Handler(cfg, log, a.deps))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	a.url, a.http = srv.URL, srv.Client()
	return a
}

// as returns client options that send a bearer token. An empty token sends no
// header: an anonymous caller.
func as(token string) connect.ClientOption {
	return connect.WithInterceptors(connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if token != "" {
				req.Header().Set("Authorization", "Bearer "+token)
			}
			return next(ctx, req)
		}
	}))
}

func (a *api) feeders(token string) doelabv1connect.FeederServiceClient {
	return doelabv1connect.NewFeederServiceClient(a.http, a.url, as(token))
}

func (a *api) sites(token string) doelabv1connect.SiteServiceClient {
	return doelabv1connect.NewSiteServiceClient(a.http, a.url, as(token))
}

func (a *api) devices(token string) doelabv1connect.DeviceServiceClient {
	return doelabv1connect.NewDeviceServiceClient(a.http, a.url, as(token))
}

func (a *api) configs(token string) doelabv1connect.EnvelopeConfigServiceClient {
	return doelabv1connect.NewEnvelopeConfigServiceClient(a.http, a.url, as(token))
}

// wantCode fails unless err is a Connect error with the given code.
func wantCode(t *testing.T, what string, err error, want connect.Code) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: no error, want %v", what, want)
		return
	}
	if got := connect.CodeOf(err); got != want {
		t.Errorf("%s: code = %v (%v), want %v", what, got, err, want)
	}
}

// wantViolation fails unless err is InvalidArgument and names the rule or
// field in its message.
func wantViolation(t *testing.T, what string, err error, mention string) {
	t.Helper()
	wantCode(t, what, err, connect.CodeInvalidArgument)
	var ce *connect.Error
	if errors.As(err, &ce) && mention != "" && !contains(ce.Message(), mention) {
		t.Errorf("%s: message %q does not mention %q", what, ce.Message(), mention)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func noErr(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

func req[T any](msg *T) *connect.Request[T] {
	return connect.NewRequest(msg)
}

var ctx = context.Background()

const (
	unknownID = "0199a0a0-0000-7000-8000-00000000dead"
)
