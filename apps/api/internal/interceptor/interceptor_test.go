package interceptor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/internal/auth"
	"doelab/api/internal/domain"
)

const procedure = "/doelab.v1.SiteService/UpdateSite"

// request is a unary request as an interceptor sees it.
type request struct {
	connect.AnyRequest
	msg    any
	header http.Header
	peer   connect.Peer
}

func newRequest(msg any) *request {
	return &request{msg: msg, header: http.Header{}, peer: connect.Peer{Addr: "203.0.113.7:4455", Protocol: "connect"}}
}

func (r *request) Any() any            { return r.msg }
func (r *request) Spec() connect.Spec  { return connect.Spec{Procedure: procedure} }
func (r *request) Header() http.Header { return r.header }
func (r *request) Peer() connect.Peer  { return r.peer }
func (r *request) HTTPMethod() string  { return http.MethodPost }

// stream is a streaming handler connection as an interceptor sees it. It
// delivers the queued messages to Receive, then io.EOF's stand-in.
type stream struct {
	connect.StreamingHandlerConn
	header   http.Header
	peer     connect.Peer
	incoming []*doelabv1.GetSiteRequest
}

func newStream() *stream {
	return &stream{header: http.Header{}, peer: connect.Peer{Addr: "203.0.113.7:4455", Protocol: "grpc"}}
}

var errEndOfStream = errors.New("end of stream")

func (s *stream) Spec() connect.Spec {
	return connect.Spec{Procedure: procedure, StreamType: connect.StreamTypeClient}
}
func (s *stream) RequestHeader() http.Header { return s.header }
func (s *stream) Peer() connect.Peer         { return s.peer }
func (s *stream) Receive(msg any) error {
	if len(s.incoming) == 0 {
		return errEndOfStream
	}
	next := s.incoming[0]
	s.incoming = s.incoming[1:]
	out := msg.(*doelabv1.GetSiteRequest)
	out.Key = next.Key
	return nil
}

func ok(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
	return connect.NewResponse(&doelabv1.GetSiteResponse{}), nil
}

func failing(err error) connect.UnaryFunc {
	return func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) { return nil, err }
}

func TestStreamingClientIsUntouched(t *testing.T) {
	t.Parallel()
	called := false
	next := connect.StreamingClientFunc(func(context.Context, connect.Spec) connect.StreamingClientConn {
		called = true
		return nil
	})
	clientOnly{}.WrapStreamingClient(next)(context.Background(), connect.Spec{})
	if !called {
		t.Error("the client leg was not passed through")
	}
}

func TestLogging(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	log := slog.New(slog.NewTextHandler(&out, nil))
	clock := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	l := logging{log: log, enabled: true, now: func() time.Time {
		clock = clock.Add(7 * time.Millisecond)
		return clock
	}}

	if _, err := l.WrapUnary(ok)(context.Background(), newRequest(nil)); err != nil {
		t.Fatal(err)
	}
	line := out.String()
	if !strings.Contains(line, "procedure="+procedure) || !strings.Contains(line, "protocol=connect") ||
		!strings.Contains(line, "ms=7") || strings.Contains(line, "code=") || strings.Contains(line, "stream=") {
		t.Errorf("a successful unary call logged %q", line)
	}

	out.Reset()
	_, err := l.WrapUnary(failing(connect.NewError(connect.CodeNotFound, errors.New("secret detail"))))(context.Background(), newRequest(nil))
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("the error was changed: %v", err)
	}
	// The code is logged; the message is not.
	if line := out.String(); !strings.Contains(line, "code=not_found") || strings.Contains(line, "secret detail") {
		t.Errorf("a failed unary call logged %q", line)
	}

	out.Reset()
	boom := connect.NewError(connect.CodeUnavailable, errors.New("x"))
	err = l.WrapStreamingHandler(func(context.Context, connect.StreamingHandlerConn) error { return boom })(context.Background(), newStream())
	if !errors.Is(err, boom) {
		t.Errorf("the stream's error was changed: %v", err)
	}
	if line := out.String(); !strings.Contains(line, "stream=client") || !strings.Contains(line, "code=unavailable") || !strings.Contains(line, "protocol=grpc") {
		t.Errorf("a stream logged %q", line)
	}

	// Disabled: nothing is logged, and the handler is the handler.
	out.Reset()
	off := Logging(log, false)
	if _, err := off.WrapUnary(ok)(context.Background(), newRequest(nil)); err != nil {
		t.Fatal(err)
	}
	if err := off.WrapStreamingHandler(func(context.Context, connect.StreamingHandlerConn) error { return nil })(context.Background(), newStream()); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Errorf("disabled logging wrote %q", out.String())
	}
	// Enabled through the constructor: it uses the real clock.
	if _, err := Logging(log, true).WrapUnary(ok)(context.Background(), newRequest(nil)); err != nil || out.Len() == 0 {
		t.Errorf("enabled logging wrote nothing (error %v)", err)
	}
}

func TestTimeout(t *testing.T) {
	t.Parallel()
	hasDeadline := func(want bool) connect.UnaryFunc {
		return func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
			if _, ok := ctx.Deadline(); ok != want {
				return nil, fmt.Errorf("deadline present = %v, want %v", ok, want)
			}
			return nil, nil
		}
	}
	if _, err := Timeout(time.Second).WrapUnary(hasDeadline(true))(context.Background(), newRequest(nil)); err != nil {
		t.Error(err)
	}
	if _, err := Timeout(0).WrapUnary(hasDeadline(false))(context.Background(), newRequest(nil)); err != nil {
		t.Error(err)
	}
	// A stream is not bounded: a subscription is meant to stay open.
	err := Timeout(time.Second).WrapStreamingHandler(func(ctx context.Context, _ connect.StreamingHandlerConn) error {
		if _, ok := ctx.Deadline(); ok {
			return errors.New("a stream was given a deadline")
		}
		return nil
	})(context.Background(), newStream())
	if err != nil {
		t.Error(err)
	}
}

func TestErrors(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	e := Errors(slog.New(slog.NewTextHandler(&out, nil)))

	tests := []struct {
		err  error
		want connect.Code
	}{
		{fmt.Errorf("site 7: %w", domain.ErrNotFound), connect.CodeNotFound},
		{fmt.Errorf("sites_nmi_key: %w", domain.ErrAlreadyExists), connect.CodeAlreadyExists},
		{domain.ErrInvalid, connect.CodeInvalidArgument},
		{domain.ErrFailedPrecondition, connect.CodeFailedPrecondition},
		{domain.ErrRetryable, connect.CodeUnavailable},
		{domain.ErrUnauthenticated, connect.CodeUnauthenticated},
		{domain.ErrPermissionDenied, connect.CodePermissionDenied},
		{context.DeadlineExceeded, connect.CodeDeadlineExceeded},
		{context.Canceled, connect.CodeCanceled},
		// Already a Connect error: passed through untouched.
		{connect.NewError(connect.CodeResourceExhausted, errors.New("too many requests")), connect.CodeResourceExhausted},
	}
	for _, tt := range tests {
		_, err := e.WrapUnary(failing(tt.err))(context.Background(), newRequest(nil))
		if connect.CodeOf(err) != tt.want {
			t.Errorf("%v became %v, want %v", tt.err, connect.CodeOf(err), tt.want)
		}
		// A classified error keeps its message: it describes the request.
		if !strings.Contains(err.Error(), tt.err.Error()) && !errors.Is(tt.err, context.Canceled) {
			t.Errorf("%v lost its message: %v", tt.err, err)
		}
		streamErr := e.WrapStreamingHandler(func(context.Context, connect.StreamingHandlerConn) error { return tt.err })(context.Background(), newStream())
		if connect.CodeOf(streamErr) != tt.want {
			t.Errorf("on a stream, %v became %v, want %v", tt.err, connect.CodeOf(streamErr), tt.want)
		}
	}

	// An unclassified error is a bug or a driver failure. Its text can carry
	// a connection string, so it is logged and never sent.
	out.Reset()
	leak := errors.New("dial tcp: password=hunter2 refused")
	_, err := e.WrapUnary(failing(leak))(context.Background(), newRequest(nil))
	if connect.CodeOf(err) != connect.CodeInternal || strings.Contains(err.Error(), "hunter2") || !strings.Contains(err.Error(), "internal error") {
		t.Errorf("an unclassified error reached the client as %v", err)
	}
	if !strings.Contains(out.String(), "hunter2") || !strings.Contains(out.String(), procedure) {
		t.Errorf("the unclassified error was not logged: %q", out.String())
	}

	if _, err := e.WrapUnary(ok)(context.Background(), newRequest(nil)); err != nil {
		t.Errorf("a success became %v", err)
	}
	if err := e.WrapStreamingHandler(func(context.Context, connect.StreamingHandlerConn) error { return nil })(context.Background(), newStream()); err != nil {
		t.Errorf("a successful stream became %v", err)
	}
}

func TestThrottle(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	th := NewThrottle(false)
	th.now = func() time.Time { return now }
	th.burst, th.perSecond = 3, 1

	call := func(addr string) error {
		r := newRequest(nil)
		r.peer.Addr = addr
		_, err := th.WrapUnary(ok)(context.Background(), r)
		return err
	}
	for i := range 3 {
		if err := call("198.51.100.1:1000"); err != nil {
			t.Fatalf("call %d within the burst: %v", i+1, err)
		}
	}
	if err := call("198.51.100.1:1001"); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Errorf("the call past the burst: %v, want resource_exhausted", err)
	}
	// Another address has its own bucket.
	if err := call("198.51.100.2:1000"); err != nil {
		t.Errorf("another address: %v", err)
	}
	// One second refills one token, and no more than the burst accumulates.
	now = now.Add(time.Second)
	if err := call("198.51.100.1:1002"); err != nil {
		t.Errorf("after a refill: %v", err)
	}
	if err := call("198.51.100.1:1003"); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Errorf("a second call after one refill: %v", err)
	}
	now = now.Add(time.Hour)
	for i := range 3 {
		if err := call("198.51.100.1:1004"); err != nil {
			t.Fatalf("call %d after a long idle: %v", i+1, err)
		}
	}
	if err := call("198.51.100.1:1005"); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Errorf("idle time banked more than the burst: %v", err)
	}

	// A stream spends one token when it opens.
	s := newStream()
	s.peer.Addr = "198.51.100.9:1"
	open := th.WrapStreamingHandler(func(context.Context, connect.StreamingHandlerConn) error { return nil })
	for i := range 3 {
		if err := open(context.Background(), s); err != nil {
			t.Fatalf("stream %d within the burst: %v", i+1, err)
		}
	}
	if err := open(context.Background(), s); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Errorf("the stream past the burst: %v", err)
	}
}

func TestThrottleEviction(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	th := NewThrottle(false)
	th.now = func() time.Time { return now }

	// Fill the table with buckets that have each spent one token.
	for i := range throttleMax {
		th.allow(fmt.Sprintf("10.0.%d.%d", i/256, i%256))
	}
	if len(th.buckets) != throttleMax {
		t.Fatalf("%d buckets, want %d", len(th.buckets), throttleMax)
	}
	// No time has passed, so none has refilled: the table is dropped whole
	// rather than grown.
	th.allow("192.0.2.1")
	if len(th.buckets) != 1 {
		t.Errorf("%d buckets after an eviction with nothing refilled, want 1", len(th.buckets))
	}

	// Fill it again; this time let them refill, and only the full ones go.
	for i := range throttleMax - 1 {
		th.allow(fmt.Sprintf("10.1.%d.%d", i/256, i%256))
	}
	now = now.Add(time.Minute)
	th.allow("192.0.2.2")
	if len(th.buckets) != 1 {
		t.Errorf("%d buckets after an eviction of refilled buckets, want 1", len(th.buckets))
	}
}

func TestClientIP(t *testing.T) {
	t.Parallel()
	forwarded := http.Header{"X-Forwarded-For": {"1.1.1.1, 2.2.2.2 , 203.0.113.50"}}

	tests := []struct {
		name       string
		peer       string
		header     http.Header
		trustProxy bool
		want       string
	}{
		{"direct", "198.51.100.4:5000", http.Header{}, false, "198.51.100.4"},
		{"forwarded but untrusted", "198.51.100.4:5000", forwarded, false, "198.51.100.4"},
		// The edge appends the address it saw; earlier entries are the
		// caller's own claim.
		{"forwarded and trusted", "127.0.0.1:5000", forwarded, true, "203.0.113.50"},
		{"trusted, single entry", "127.0.0.1:5000", http.Header{"X-Forwarded-For": {"203.0.113.9"}}, true, "203.0.113.9"},
		{"trusted, no header", "127.0.0.1:5000", http.Header{}, true, "127.0.0.1"},
		{"trusted, empty last entry", "127.0.0.1:5000", http.Header{"X-Forwarded-For": {"1.1.1.1, "}}, true, "127.0.0.1"},
		{"peer with no port", "unix-socket", http.Header{}, false, "unix-socket"},
	}
	for _, tt := range tests {
		if got := ClientIP(tt.peer, tt.header, tt.trustProxy); got != tt.want {
			t.Errorf("%s: ClientIP = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestAuth(t *testing.T) {
	t.Parallel()
	tokens := auth.NewTokens("engine-token", "operator-token", "device-secret")
	a := Auth(tokens, Policy{
		procedure:                        auth.ScopeOperator,
		"/doelab.v1.SiteService/GetSite": auth.ScopePublic,
	})
	// The handler reports who it was called as.
	var seen auth.Actor
	var authenticated bool
	spy := func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		seen, authenticated = auth.FromContext(ctx)
		return nil, nil
	}
	call := func(authorization string) error {
		r := newRequest(nil)
		if authorization != "" {
			r.header.Set("Authorization", authorization)
		}
		seen, authenticated = auth.Actor{}, false
		_, err := a.WrapUnary(spy)(context.Background(), r)
		return err
	}

	if err := call("Bearer operator-token"); err != nil || !authenticated || seen.Scope != auth.ScopeOperator {
		t.Errorf("operator: %v, actor %+v", err, seen)
	}
	if err := call(""); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("anonymous: %v, want unauthenticated", err)
	}
	if err := call("Bearer wrong"); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("a wrong token: %v, want unauthenticated", err)
	}
	err := call("Bearer engine-token")
	if connect.CodeOf(err) != connect.CodePermissionDenied || !strings.Contains(err.Error(), "operator scope") {
		t.Errorf("the engine on an operator procedure: %v, want permission_denied naming the scope", err)
	}

	// The same on a stream.
	opened := false
	open := a.WrapStreamingHandler(func(ctx context.Context, _ connect.StreamingHandlerConn) error {
		_, opened = auth.FromContext(ctx)
		return nil
	})
	s := newStream()
	if err := open(context.Background(), s); connect.CodeOf(err) != connect.CodeUnauthenticated || opened {
		t.Errorf("an anonymous stream: %v", err)
	}
	s.header.Set("Authorization", "Bearer operator-token")
	if err := open(context.Background(), s); err != nil || !opened {
		t.Errorf("an operator's stream: %v, opened %v", err, opened)
	}

	// A procedure that is not in the policy is refused, whoever asks.
	unlisted := Auth(tokens, Policy{})
	r := newRequest(nil)
	r.header.Set("Authorization", "Bearer operator-token")
	if _, err := unlisted.WrapUnary(spy)(context.Background(), r); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("a procedure outside the policy: %v, want permission_denied", err)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()
	v, err := NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	validate := Validate(v)
	good := &doelabv1.GetSiteRequest{Key: &doelabv1.GetSiteRequest_Nmi{Nmi: "XDLAB000014"}}
	bad := &doelabv1.GetSiteRequest{Key: &doelabv1.GetSiteRequest_Nmi{Nmi: "nope"}}

	if _, err := validate.WrapUnary(ok)(context.Background(), newRequest(good)); err != nil {
		t.Errorf("a valid message: %v", err)
	}
	_, err = validate.WrapUnary(ok)(context.Background(), newRequest(bad))
	if connect.CodeOf(err) != connect.CodeInvalidArgument || !strings.Contains(err.Error(), "nmi") {
		t.Errorf("an invalid message: %v, want invalid_argument naming the field", err)
	}
	// Not a protobuf message: nothing to validate.
	if _, err := validate.WrapUnary(ok)(context.Background(), newRequest("a string")); err != nil {
		t.Errorf("a non-proto message: %v", err)
	}

	// On a stream, every message is validated as the handler receives it.
	s := newStream()
	s.incoming = []*doelabv1.GetSiteRequest{good, bad}
	var results []error
	err = validate.WrapStreamingHandler(func(_ context.Context, conn connect.StreamingHandlerConn) error {
		for {
			var msg doelabv1.GetSiteRequest
			err := conn.Receive(&msg)
			results = append(results, err)
			if err != nil && connect.CodeOf(err) != connect.CodeInvalidArgument {
				return nil
			}
		}
	})(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 || results[0] != nil || connect.CodeOf(results[1]) != connect.CodeInvalidArgument || !errors.Is(results[2], errEndOfStream) {
		t.Errorf("stream results = %v, want ok, invalid_argument, end of stream", results)
	}
}
