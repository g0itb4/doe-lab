// Package domain holds the data model as plain Go types: one struct per table
// of the PostgreSQL schema, with the same fields, units and nullability, and
// the errors that every layer speaks.
//
// The schema in packages/db/migrations is the source of truth. A field here
// carries a `db` tag naming its column, and a parity test fails when a struct
// and its table disagree. A nullable column is a pointer; a NOT NULL column is
// not.
//
// The package does no I/O and imports nothing from this module.
package domain

import "errors"

// The errors that cross layers. A repository maps driver errors onto these, a
// service returns them, and the errors interceptor maps them onto Connect
// codes. Wrap them with context; compare with errors.Is.
var (
	// ErrNotFound: the row does not exist, or is soft-deleted.
	ErrNotFound = errors.New("not found")
	// ErrAlreadyExists: a unique constraint refused the write.
	ErrAlreadyExists = errors.New("already exists")
	// ErrInvalid: the request breaks a rule about its own content.
	ErrInvalid = errors.New("invalid")
	// ErrFailedPrecondition: the request is well formed, but the state of
	// the system refuses it: a missing parent row, an immutable record, an
	// active backstop.
	ErrFailedPrecondition = errors.New("failed precondition")
	// ErrRetryable: a serialisation failure or a deadlock; the same request
	// may succeed if sent again.
	ErrRetryable = errors.New("try again")
	// ErrUnauthenticated: no credentials, or credentials that do not verify.
	ErrUnauthenticated = errors.New("unauthenticated")
	// ErrPermissionDenied: the credentials are good but lack the scope.
	ErrPermissionDenied = errors.New("permission denied")
	// ErrExhausted: the request is fine, but its ration is spent: a rate
	// limit or a budget. Later it may pass.
	ErrExhausted = errors.New("exhausted")
)
