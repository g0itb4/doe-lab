package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"doelab/api/internal/domain"
)

const (
	nmiA = "XDLAB000014" // checksum-valid synthetic NMIs
	nmiB = "XDLAB000020"
)

func tokens() *Tokens {
	return NewTokens("engine-token", "operator-token", "device-secret")
}

func TestFixtureNMIsAreValid(t *testing.T) {
	t.Parallel()
	for _, nmi := range []string{nmiA, nmiB} {
		if !domain.ValidNMI(nmi) {
			want, _ := domain.SyntheticNMI(int(nmi[9] - '0'))
			t.Fatalf("fixture NMI %s is not valid; did you mean %s?", nmi, want)
		}
	}
}

func TestAuthenticate(t *testing.T) {
	t.Parallel()
	tk := tokens()

	tests := []struct {
		name   string
		header string
		want   Actor
	}{
		{name: "engine", header: "Bearer engine-token", want: Actor{Scope: ScopeEngine}},
		{name: "operator", header: "Bearer operator-token", want: Actor{Scope: ScopeOperator}},
		{name: "device", header: "Bearer " + tk.DeviceToken(nmiA), want: Actor{Scope: ScopeDevice, NMI: nmiA}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tk.Authenticate(tt.header)
			if err != nil || got != tt.want {
				t.Errorf("Authenticate = %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}

	deviceA := tk.DeviceToken(nmiA)
	macA := deviceA[strings.LastIndex(deviceA, "_")+1:]
	refused := map[string]string{
		"no header":                   "",
		"no scheme":                   "engine-token",
		"wrong scheme":                "Basic engine-token",
		"empty token":                 "Bearer ",
		"unknown token":               "Bearer something-else",
		"prefix of a token":           "Bearer engine-toke",
		"device MAC of another NMI":   "Bearer dev_" + nmiB + "_" + macA,
		"device token with no MAC":    "Bearer dev_" + nmiA,
		"device MAC that is not hex":  "Bearer dev_" + nmiA + "_zz",
		"device with an invalid NMI":  "Bearer dev_NOTANNMI_" + macA,
		"device from another secret":  "Bearer " + NewTokens("e", "o", "other").DeviceToken(nmiA),
		"device truncated MAC":        "Bearer " + deviceA[:len(deviceA)-2],
		"lower-case scheme and token": "bearer engine-token",
	}
	for name, header := range refused {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := tk.Authenticate(header)
			if !errors.Is(err, domain.ErrUnauthenticated) || got != (Actor{}) {
				t.Errorf("Authenticate(%q) = %+v, %v; want ErrUnauthenticated", header, got, err)
			}
			// The error must not say why.
			if err != nil && err.Error() != domain.ErrUnauthenticated.Error() {
				t.Errorf("error %q gives a reason", err)
			}
		})
	}
}

func TestDeviceTokensDiffer(t *testing.T) {
	t.Parallel()
	tk := tokens()
	if tk.DeviceToken(nmiA) == tk.DeviceToken(nmiB) {
		t.Error("two NMIs share a device token")
	}
	if !strings.HasPrefix(tk.DeviceToken(nmiA), "dev_"+nmiA+"_") {
		t.Errorf("token = %s", tk.DeviceToken(nmiA))
	}
}

func TestContextAndNames(t *testing.T) {
	t.Parallel()

	if _, ok := FromContext(context.Background()); ok {
		t.Error("an empty context has an actor")
	}
	if got := ActorName(context.Background()); got != "anonymous" {
		t.Errorf("ActorName = %q", got)
	}

	names := map[Actor]string{
		{Scope: ScopeEngine}:            "engine",
		{Scope: ScopeOperator}:          "operator",
		{Scope: ScopeDevice, NMI: nmiA}: "device:" + nmiA,
	}
	for actor, want := range names {
		ctx := NewContext(context.Background(), actor)
		if got, ok := FromContext(ctx); !ok || got != actor {
			t.Errorf("FromContext = %+v, %v", got, ok)
		}
		if got := ActorName(ctx); got != want {
			t.Errorf("ActorName = %q, want %q", got, want)
		}
	}
}

func TestRequire(t *testing.T) {
	t.Parallel()
	anonymous := context.Background()
	operator := NewContext(context.Background(), Actor{Scope: ScopeOperator})
	device := NewContext(context.Background(), Actor{Scope: ScopeDevice, NMI: nmiA})

	if err := Require(anonymous, ScopePublic); err != nil {
		t.Errorf("public procedure, anonymous caller: %v", err)
	}
	if err := Require(operator, ScopeOperator); err != nil {
		t.Errorf("operator procedure, operator caller: %v", err)
	}
	if err := Require(anonymous, ScopeOperator); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("operator procedure, anonymous caller: %v, want ErrUnauthenticated", err)
	}
	if err := Require(operator, ScopeEngine); !errors.Is(err, domain.ErrPermissionDenied) {
		t.Errorf("engine procedure, operator caller: %v, want ErrPermissionDenied", err)
	}

	if err := RequireDevice(device, nmiA); err != nil {
		t.Errorf("device on its own site: %v", err)
	}
	if err := RequireDevice(device, nmiB); !errors.Is(err, domain.ErrPermissionDenied) {
		t.Errorf("device on another site: %v, want ErrPermissionDenied", err)
	}
	if err := RequireDevice(operator, nmiA); !errors.Is(err, domain.ErrPermissionDenied) {
		t.Errorf("operator as a device: %v, want ErrPermissionDenied", err)
	}
	if err := RequireDevice(anonymous, nmiA); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("anonymous as a device: %v, want ErrUnauthenticated", err)
	}
}
