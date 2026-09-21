// Package controller holds the ConnectRPC handlers. A handler parses the
// request into domain types, calls one service method, and maps the result
// back with protomap.
//
// It contains no business rules, no validation (the validate interceptor has
// run the CEL rules before a handler sees the message) and no error mapping
// (the errors interceptor turns domain errors into Connect codes).
package controller

import (
	"fmt"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"doelab/api/internal/domain"
)

// parseID parses a UUID field. Validation has already checked the format, so
// a failure here means a field that has no rule; it is still the caller's
// mistake, and is reported as one.
func parseID(field, value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("%s: not a UUID", field))
	}
	return id, nil
}

// pager is the pagination fields that every List request carries.
type pager interface {
	GetPageSize() int32
	GetPageToken() string
}

func page(req pager) domain.Page {
	return domain.Page{Size: req.GetPageSize(), Token: req.GetPageToken()}
}

// masked reports whether an update mask names a path.
func masked(paths []string, path string) bool {
	for _, p := range paths {
		if p == path {
			return true
		}
	}
	return false
}

// when returns a pointer to v when the mask names path, and nil otherwise:
// the shape of a patch field.
func when[T any](paths []string, path string, v T) *T {
	if !masked(paths, path) {
		return nil
	}
	return &v
}
