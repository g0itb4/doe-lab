// Package pg implements the service ports on PostgreSQL. It is the only
// package that knows about SQL, pgx or Postgres error codes; everything above
// it speaks domain types and domain errors.
//
// The queries are in packages/db/queries and are compiled by sqlc into the
// gen package. This package maps their rows onto domain types, by hand, so
// the compiler checks every field.
package pg

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"doelab/api/internal/auth"
	"doelab/api/internal/domain"
	"doelab/api/internal/repo/pg/gen"
	"doelab/api/internal/service"
)

// Store owns the pool and implements service.Store.
type Store struct {
	repos
	pool *pgxpool.Pool
}

// NewStore builds a store on a pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{repos: repos{q: gen.New(pool), db: pool}, pool: pool}
}

var _ service.Store = (*Store)(nil)

// repos implements service.Repos over either the pool or one transaction:
// sqlc's queries take a DBTX, and both satisfy it.
type repos struct {
	q  *gen.Queries
	db gen.DBTX
}

// Tx runs fn inside one transaction and hands it repositories bound to that
// transaction. A transaction crosses the service boundary as a service.Repos,
// never as a pgx.Tx.
func (s *Store) Tx(ctx context.Context, fn func(context.Context, service.Repos) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", pgErr(err))
	}
	// Rollback after a successful Commit is a no-op, so this is unconditional.
	// WithoutCancel so a cancelled request still rolls back rather than
	// leaking the transaction until the connection is reaped.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	// The audit actor, as the transaction's first statement.
	// record_row_history() reads it into row_history.changed_by. set_actor
	// scopes the value to this transaction, which is what makes it safe on
	// a pooled connection.
	if actor, ok := auth.FromContext(ctx); ok {
		if _, err := tx.Exec(ctx, `SELECT set_actor($1, '')`, actor.Name()); err != nil {
			return fmt.Errorf("set_actor: %w", pgErr(err))
		}
	}

	if err := fn(ctx, &repos{q: gen.New(tx), db: tx}); err != nil {
		return err
	}
	return pgErr(tx.Commit(ctx))
}

// NewPool builds the connection pool.
//
// pgx defaults to the extended protocol with a per-connection
// prepared-statement cache: every statement is prepared once per connection
// and then costs one round trip with no parse or plan. Putting pgbouncer in
// transaction mode in front of Postgres would break that, and the mode would
// have to become QueryExecModeExec.
func NewPool(ctx context.Context, url string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	cfg.MaxConns = maxConns
	// Warm. A cold connect is TCP plus auth plus startup, several times the
	// query it is about to run.
	cfg.MinConns = min(4, maxConns)
	cfg.MaxConnLifetime = time.Hour
	// Without jitter every connection opened at boot recycles at the same
	// instant.
	cfg.MaxConnLifetimeJitter = 5 * time.Minute
	cfg.MaxConnIdleTime = 30 * time.Minute
	cfg.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return pool, nil
}

// SQLSTATE codes, spelled out rather than pulled in as a dependency.
// https://www.postgresql.org/docs/current/errcodes-appendix.html
const (
	uniqueViolation     = "23505"
	foreignKeyViolation = "23503"
	checkViolation      = "23514"
	notNullViolation    = "23502"
	restrictViolation   = "23001"
	serializationFail   = "40001"
	deadlockDetected    = "40P01"
	invalidTextRep      = "22P02"
)

// chunkPrefix is what TimescaleDB puts before a constraint's name on a chunk
// of a hypertable: "_hyper_6_1_chunk_envelopes_one_active_key".
var chunkPrefix = regexp.MustCompile(`^_hyper_\d+_\d+_chunk_`)

// pgErr is where the driver stops: Postgres codes become domain errors, and
// the errors interceptor turns those into Connect codes. Everything except an
// unclassified error reaches the client, so each message is a disclosure
// decision: a constraint's name is schema shape, not data.
func pgErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}

	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	constraint := chunkPrefix.ReplaceAllString(pg.ConstraintName, "")

	switch pg.Code {
	case uniqueViolation:
		return fmt.Errorf("%s: %w", constraint, domain.ErrAlreadyExists)
	case foreignKeyViolation:
		return fmt.Errorf("%s: %w", constraint, domain.ErrFailedPrecondition)
	case checkViolation, notNullViolation, invalidTextRep:
		return fmt.Errorf("%s: %w", constraint, domain.ErrInvalid)
	case restrictViolation:
		// RAISE ... USING ERRCODE from the schema's own guards: a config
		// version or an envelope is immutable, row_history is append-only.
		return fmt.Errorf("%s: %w", pg.Message, domain.ErrFailedPrecondition)
	case serializationFail, deadlockDetected:
		return fmt.Errorf("%s: %w", pg.Code, domain.ErrRetryable)
	}
	return err
}

// one maps the result of a :one query.
func one[Row, Out any](row Row, err error, convert func(Row) Out) (Out, error) {
	if err != nil {
		var zero Out
		return zero, pgErr(err)
	}
	return convert(row), nil
}

// many maps the result of a :many query.
func many[Row, Out any](rows []Row, err error, convert func(Row) Out) ([]Out, error) {
	if err != nil {
		return nil, pgErr(err)
	}
	out := make([]Out, len(rows))
	for i, row := range rows {
		out[i] = convert(row)
	}
	return out, nil
}
