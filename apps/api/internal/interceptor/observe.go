// Package interceptor holds the Connect interceptor chain: the cross-cutting
// concerns that apply to every RPC without any handler opting in.
//
// Every interceptor here implements the full connect.Interceptor, for unary
// and for streaming handlers alike. The shortcut type,
// connect.UnaryInterceptorFunc, passes streams straight through, so a chain
// built from it would serve a streaming RPC with no auth, no validation and no
// error scrubbing.
package interceptor

import (
	"context"
	"log/slog"
	"time"

	"connectrpc.com/connect"
)

// clientOnly is embedded by every interceptor: this process is a server, and
// there is no outbound leg to wrap.
type clientOnly struct{}

func (clientOnly) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// Logging records every RPC with its final code and its duration. It is
// outermost in the chain, so the code it sees is the one the client gets, and
// the duration covers auth and validation. A stream is logged once, when it
// ends.
func Logging(log *slog.Logger, enabled bool) connect.Interceptor {
	return logging{log: log, enabled: enabled, now: time.Now}
}

type logging struct {
	clientOnly
	log     *slog.Logger
	enabled bool
	now     func() time.Time
}

func (l logging) record(ctx context.Context, spec connect.Spec, protocol string, start time.Time, err error) {
	attrs := []any{
		"procedure", spec.Procedure,
		"protocol", protocol,
		"ms", l.now().Sub(start).Milliseconds(),
	}
	if spec.StreamType != connect.StreamTypeUnary {
		attrs = append(attrs, "stream", spec.StreamType.String())
	}
	if err != nil {
		// The code, not the message: the message may have been scrubbed, and
		// the code is what an alert would key on.
		attrs = append(attrs, "code", connect.CodeOf(err).String())
	}
	l.log.InfoContext(ctx, "rpc", attrs...)
}

func (l logging) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	if !l.enabled {
		return next
	}
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		start := l.now()
		res, err := next(ctx, req)
		l.record(ctx, req.Spec(), req.Peer().Protocol, start, err)
		return res, err
	}
}

func (l logging) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	if !l.enabled {
		return next
	}
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		start := l.now()
		err := next(ctx, conn)
		l.record(ctx, conn.Spec(), conn.Peer().Protocol, start, err)
		return err
	}
}

// Timeout bounds a unary RPC. It only ever shortens: Connect already carries
// a client's own deadline into the context, and the earlier of the two wins.
//
// A stream is not bounded: a subscription is meant to stay open. Streams are
// kept honest by their keepalive instead, which ends one whose client has
// gone.
func Timeout(d time.Duration) connect.Interceptor {
	return timeout{d: d}
}

type timeout struct {
	clientOnly
	d time.Duration
}

func (t timeout) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	if t.d <= 0 {
		return next
	}
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		ctx, cancel := context.WithTimeout(ctx, t.d)
		defer cancel()
		return next(ctx, req)
	}
}

func (timeout) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}
