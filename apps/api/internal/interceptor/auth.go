package interceptor

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	"doelab/api/internal/auth"
)

// Policy maps each procedure to the scope it needs. It is a table, not a
// naming convention, so what a procedure needs is written down and a test can
// check that no procedure is missing.
type Policy map[string]auth.Scope

// Auth resolves the caller and enforces the policy.
//
// It fails closed twice over. A procedure that is not in the policy is
// refused, so adding an RPC without deciding who may call it does not serve
// it to everyone. And a token that does not verify is refused even on a
// public procedure: a client holding a wrong token finds out at once, rather
// than being served as anonymous and failing later on a write.
func Auth(tokens *auth.Tokens, policy Policy) connect.Interceptor {
	return authInterceptor{tokens: tokens, policy: policy}
}

type authInterceptor struct {
	clientOnly
	tokens *auth.Tokens
	policy Policy
}

// Not echoing the reason: "no credentials" and "wrong credentials" are the
// same answer to anyone probing.
var (
	errCredentials = connect.NewError(connect.CodeUnauthenticated, errors.New("valid credentials are required"))
	errNotInPolicy = connect.NewError(connect.CodePermissionDenied, errors.New("this procedure is not served"))
)

// authorise returns the context to continue with, carrying the actor when
// there is one.
func (a authInterceptor) authorise(ctx context.Context, procedure string, h http.Header) (context.Context, error) {
	scope, known := a.policy[procedure]
	if !known {
		return nil, errNotInPolicy
	}

	if header := h.Get("Authorization"); header != "" {
		actor, err := a.tokens.Authenticate(header)
		if err != nil {
			return nil, errCredentials
		}
		ctx = auth.NewContext(ctx, actor)
	}

	if err := auth.Require(ctx, scope); err != nil {
		if _, authenticated := auth.FromContext(ctx); !authenticated {
			return nil, errCredentials
		}
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	}
	return ctx, nil
}

func (a authInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		ctx, err := a.authorise(ctx, req.Spec().Procedure, req.Header())
		if err != nil {
			return nil, err
		}
		return next(ctx, req)
	}
}

func (a authInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		ctx, err := a.authorise(ctx, conn.Spec().Procedure, conn.RequestHeader())
		if err != nil {
			return err
		}
		return next(ctx, conn)
	}
}
