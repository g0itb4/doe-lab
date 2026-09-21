package interceptor

import (
	"context"
	"fmt"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
)

// Validator is protovalidate's interface, aliased so packages that only pass
// one around do not import protovalidate themselves.
type Validator = protovalidate.Validator

// NewValidator compiles the constraint set once, at startup. A failure here is
// a malformed annotation in a .proto file: a build-time mistake that should
// stop the process rather than surface as a per-request error.
func NewValidator() (Validator, error) {
	v, err := protovalidate.New()
	if err != nil {
		return nil, fmt.Errorf("compile proto constraints: %w", err)
	}
	return v, nil
}

// Validate enforces the CEL rules declared on the request messages before any
// handler sees them. It is innermost in the chain, so no handler runs on a
// message that failed, and it runs after Auth, so field-level messages go
// only to a caller who may use the procedure.
//
// On a stream, every message the client sends is validated as it is received.
func Validate(v Validator) connect.Interceptor {
	return validator{v: v}
}

type validator struct {
	clientOnly
	v Validator
}

func (v validator) check(msg any) error {
	m, ok := msg.(proto.Message)
	if !ok {
		// Not a protobuf message: nothing to validate.
		return nil
	}
	if err := v.v.Validate(m); err != nil {
		// protovalidate's error names the field and the rule, which is what a
		// form wants to show inline. It describes the request, not the
		// server, so it is safe to return.
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	return nil
}

func (v validator) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if err := v.check(req.Any()); err != nil {
			return nil, err
		}
		return next(ctx, req)
	}
}

func (v validator) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		return next(ctx, validatingConn{StreamingHandlerConn: conn, v: v})
	}
}

// validatingConn validates each message as the handler receives it.
type validatingConn struct {
	connect.StreamingHandlerConn
	v validator
}

func (c validatingConn) Receive(msg any) error {
	if err := c.StreamingHandlerConn.Receive(msg); err != nil {
		return err
	}
	return c.v.check(msg)
}
