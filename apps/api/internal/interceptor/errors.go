package interceptor

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"

	"doelab/api/internal/domain"
)

// Errors is the second and last hop of error translation: domain errors into
// Connect codes.
//
// It is an interceptor and not a helper called from each handler, because the
// alternative is the same switch in every handler, and one of them will get
// it wrong. The repository already turned driver errors into domain errors,
// so anything that reaches here is a domain error, an error a handler built
// on purpose, or a bug.
func Errors(log *slog.Logger) connect.Interceptor {
	return errorMapper{log: log}
}

type errorMapper struct {
	clientOnly
	log *slog.Logger
}

func (e errorMapper) translate(ctx context.Context, procedure string, err error) error {
	if err == nil {
		return nil
	}
	// Already a Connect error: auth, validation, a controller's own parse
	// failure. It said what it meant.
	var ce *connect.Error
	if errors.As(err, &ce) {
		return err
	}

	code := connect.CodeInternal
	switch {
	case errors.Is(err, domain.ErrNotFound):
		code = connect.CodeNotFound
	case errors.Is(err, domain.ErrAlreadyExists):
		code = connect.CodeAlreadyExists
	case errors.Is(err, domain.ErrInvalid):
		code = connect.CodeInvalidArgument
	case errors.Is(err, domain.ErrFailedPrecondition):
		code = connect.CodeFailedPrecondition
	case errors.Is(err, domain.ErrRetryable):
		code = connect.CodeUnavailable
	case errors.Is(err, domain.ErrUnauthenticated):
		code = connect.CodeUnauthenticated
	case errors.Is(err, domain.ErrPermissionDenied):
		code = connect.CodePermissionDenied
	case errors.Is(err, context.DeadlineExceeded):
		code = connect.CodeDeadlineExceeded
	case errors.Is(err, context.Canceled):
		code = connect.CodeCanceled
	}

	if code == connect.CodeInternal {
		// The one case where the message must NOT reach the client. An
		// unclassified error is a bug or a driver failure, and its text can
		// carry a connection string, a query or a row.
		e.log.ErrorContext(ctx, "unhandled error", "procedure", procedure, "err", err)
		return connect.NewError(code, errors.New("internal error"))
	}
	return connect.NewError(code, err)
}

func (e errorMapper) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		res, err := next(ctx, req)
		if err != nil {
			return nil, e.translate(ctx, req.Spec().Procedure, err)
		}
		return res, nil
	}
}

func (e errorMapper) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		return e.translate(ctx, conn.Spec().Procedure, next(ctx, conn))
	}
}
