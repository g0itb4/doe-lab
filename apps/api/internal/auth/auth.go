// Package auth decides who a caller is, from a bearer token, and carries the
// answer on the context.
//
// There is no identity provider and no user table. A caller holds one of
// three scopes:
//
//	engine    publishes envelopes and records runs
//	operator  changes configuration and triggers the backstop
//	device    subscribes to one site's envelope and sends its telemetry
//
// The engine and operator tokens are configuration. A device token is
// derived: HMAC-SHA256(secret, NMI). So every device has its own token, a
// token for one NMI is useless for another, and there is nothing to store.
// Reads need no token at all.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"

	"doelab/api/internal/domain"
)

// Scope is what a token allows.
type Scope string

// The scopes.
const (
	// ScopePublic marks a procedure that needs no token. No actor has it.
	ScopePublic   Scope = "public"
	ScopeEngine   Scope = "engine"
	ScopeOperator Scope = "operator"
	ScopeDevice   Scope = "device"
)

// Actor is an authenticated caller.
type Actor struct {
	Scope Scope
	// NMI is the site a device token belongs to. Empty for other scopes.
	NMI string
}

// Name is how the actor appears in the audit trail and in created_by.
func (a Actor) Name() string {
	if a.Scope == ScopeDevice {
		return "device:" + a.NMI
	}
	return string(a.Scope)
}

// devicePrefix starts every device token, so the server can tell which
// verification a token needs without trying them all.
const devicePrefix = "dev_"

// Tokens verifies bearer tokens.
type Tokens struct {
	engine       []byte
	operator     []byte
	deviceSecret []byte
}

// NewTokens builds a verifier from the configured secrets.
func NewTokens(engineToken, operatorToken, deviceSecret string) *Tokens {
	return &Tokens{
		engine:       []byte(engineToken),
		operator:     []byte(operatorToken),
		deviceSecret: []byte(deviceSecret),
	}
}

// DeviceToken returns the token of the device at nmi: the NMI and its MAC,
// "dev_<nmi>_<hex>".
func (t *Tokens) DeviceToken(nmi string) string {
	return devicePrefix + nmi + "_" + hex.EncodeToString(t.mac(nmi))
}

func (t *Tokens) mac(nmi string) []byte {
	m := hmac.New(sha256.New, t.deviceSecret)
	m.Write([]byte(nmi))
	return m.Sum(nil)
}

// Authenticate turns the value of an Authorization header into an actor. Every
// failure is domain.ErrUnauthenticated with no detail: "no credentials" and
// "wrong credentials" are the same answer to anyone probing.
func (t *Tokens) Authenticate(authorization string) (Actor, error) {
	token, ok := strings.CutPrefix(authorization, "Bearer ")
	if !ok || token == "" {
		return Actor{}, domain.ErrUnauthenticated
	}

	if rest, isDevice := strings.CutPrefix(token, devicePrefix); isDevice {
		nmi, macHex, found := strings.Cut(rest, "_")
		got, err := hex.DecodeString(macHex)
		if !found || err != nil || !domain.ValidNMI(nmi) || !hmac.Equal(got, t.mac(nmi)) {
			return Actor{}, domain.ErrUnauthenticated
		}
		return Actor{Scope: ScopeDevice, NMI: nmi}, nil
	}

	// Constant time, and both compared every time, so the response time says
	// nothing about which token was closer.
	isEngine := subtle.ConstantTimeCompare([]byte(token), t.engine)
	isOperator := subtle.ConstantTimeCompare([]byte(token), t.operator)
	switch {
	case isEngine == 1:
		return Actor{Scope: ScopeEngine}, nil
	case isOperator == 1:
		return Actor{Scope: ScopeOperator}, nil
	}
	return Actor{}, domain.ErrUnauthenticated
}

type contextKey struct{}

// NewContext returns ctx carrying the actor.
func NewContext(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, contextKey{}, a)
}

// FromContext returns the actor on ctx. ok is false for an anonymous caller.
func FromContext(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(contextKey{}).(Actor)
	return a, ok
}

// ActorName is the name of the actor on ctx for the audit trail, or
// "anonymous".
func ActorName(ctx context.Context) string {
	if a, ok := FromContext(ctx); ok {
		return a.Name()
	}
	return "anonymous"
}

// Require checks that the caller on ctx may use a procedure that needs scope.
// A procedure with ScopePublic needs nothing.
func Require(ctx context.Context, scope Scope) error {
	if scope == ScopePublic {
		return nil
	}
	a, ok := FromContext(ctx)
	if !ok {
		return fmt.Errorf("%w: this procedure needs the %s scope", domain.ErrUnauthenticated, scope)
	}
	if a.Scope != scope {
		return fmt.Errorf("%w: this procedure needs the %s scope", domain.ErrPermissionDenied, scope)
	}
	return nil
}

// RequireDevice checks that the caller on ctx is the device of the site at
// nmi. A device token is good for its own site only.
func RequireDevice(ctx context.Context, nmi string) error {
	a, ok := FromContext(ctx)
	if !ok {
		return fmt.Errorf("%w: a device token is required", domain.ErrUnauthenticated)
	}
	if a.Scope != ScopeDevice || a.NMI != nmi {
		return fmt.Errorf("%w: the token is not for site %s", domain.ErrPermissionDenied, nmi)
	}
	return nil
}
