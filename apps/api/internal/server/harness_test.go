package server_test

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"

	"doelab/api/gen/doelab/v1/doelabv1connect"
	"doelab/api/internal/apitest"
	"doelab/api/internal/repo/repotest"
)

// api is the test API of package apitest, with the fixture feeder in it and
// the generated clients to hand.
type api struct {
	*apitest.API
	fixture repotest.Fixture
}

const (
	engineToken   = apitest.EngineToken
	operatorToken = apitest.OperatorToken
)

func newAPI(t *testing.T) *api {
	t.Helper()
	a := apitest.New(t)
	return &api{API: a, fixture: repotest.Seed(t, a.Store, "LV10", 1)}
}

func as(token string) connect.ClientOption { return apitest.As(token) }

func (a *api) runs(token string) doelabv1connect.EnvelopeRunServiceClient {
	return doelabv1connect.NewEnvelopeRunServiceClient(a.HTTP, a.URL, as(token))
}

func (a *api) envelopes(token string) doelabv1connect.EnvelopeServiceClient {
	return doelabv1connect.NewEnvelopeServiceClient(a.HTTP, a.URL, as(token))
}

func (a *api) feeders(token string) doelabv1connect.FeederServiceClient {
	return doelabv1connect.NewFeederServiceClient(a.HTTP, a.URL, as(token))
}

func (a *api) sites(token string) doelabv1connect.SiteServiceClient {
	return doelabv1connect.NewSiteServiceClient(a.HTTP, a.URL, as(token))
}

func (a *api) devices(token string) doelabv1connect.DeviceServiceClient {
	return doelabv1connect.NewDeviceServiceClient(a.HTTP, a.URL, as(token))
}

func (a *api) configs(token string) doelabv1connect.EnvelopeConfigServiceClient {
	return doelabv1connect.NewEnvelopeConfigServiceClient(a.HTTP, a.URL, as(token))
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
