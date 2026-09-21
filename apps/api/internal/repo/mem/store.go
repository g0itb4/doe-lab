// Package mem is an in-memory implementation of the service ports.
//
// It exists for the unit tier: services and controllers are tested against it
// with no container. It keeps the rules of the schema that the services lean
// on (uniqueness, existence of parents, soft delete, keyset order), and the
// suite in internal/repo/repotest runs against both this store and the
// Postgres one, so the two cannot drift apart unnoticed.
//
// It is not a database: there is no durability, and a transaction is one
// mutex held for its whole length.
package mem

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
	"doelab/api/internal/service"
)

// state is every table.
type state struct {
	feeders  map[uuid.UUID]domain.Feeder
	nodes    map[uuid.UUID]domain.FeederNode
	lines    map[uuid.UUID]domain.FeederLine
	sites    map[uuid.UUID]domain.Site
	devices  map[uuid.UUID]domain.Device
	configs  map[uuid.UUID]domain.EnvelopeConfig
	profiles map[uuid.UUID][]domain.SiteProfile
}

func newState() *state {
	return &state{
		feeders:  map[uuid.UUID]domain.Feeder{},
		nodes:    map[uuid.UUID]domain.FeederNode{},
		lines:    map[uuid.UUID]domain.FeederLine{},
		sites:    map[uuid.UUID]domain.Site{},
		devices:  map[uuid.UUID]domain.Device{},
		configs:  map[uuid.UUID]domain.EnvelopeConfig{},
		profiles: map[uuid.UUID][]domain.SiteProfile{},
	}
}

// clone copies the tables, so a failed transaction can be undone. Rows are
// values and are never changed in place, so copying the maps is enough.
func (s *state) clone() *state {
	return &state{
		feeders:  maps.Clone(s.feeders),
		nodes:    maps.Clone(s.nodes),
		lines:    maps.Clone(s.lines),
		sites:    maps.Clone(s.sites),
		devices:  maps.Clone(s.devices),
		configs:  maps.Clone(s.configs),
		profiles: maps.Clone(s.profiles),
	}
}

// Store is the in-memory store. The zero value is not usable; call New.
type Store struct {
	repos
	mu sync.Mutex
	st *state
	// Now is the clock for created_at and updated_at. Tests may replace it.
	Now func() time.Time
}

// New returns an empty store.
func New() *Store {
	s := &Store{st: newState(), Now: time.Now}
	s.repos = repos{s: s}
	return s
}

var _ service.Store = (*Store)(nil)

// Tx runs fn with the store locked, and undoes its writes when it fails.
func (s *Store) Tx(ctx context.Context, fn func(context.Context, service.Repos) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	before := s.st.clone()
	if err := fn(ctx, &repos{s: s, inTx: true}); err != nil {
		s.st = before
		return err
	}
	return nil
}

// repos implements service.Repos. Outside a transaction every method takes
// the lock for its own length; inside one, Tx already holds it.
type repos struct {
	s    *Store
	inTx bool
}

// lock takes the store's lock unless a transaction holds it, and returns the
// function that releases it: `defer r.lock()()`.
func (r *repos) lock() func() {
	if r.inTx {
		return func() {}
	}
	r.s.mu.Lock()
	return r.s.mu.Unlock
}

func (r *repos) now() time.Time {
	return r.s.Now().UTC().Truncate(time.Microsecond)
}

func notFound(what string, id any) error {
	return fmt.Errorf("%s %v: %w", what, id, domain.ErrNotFound)
}

// sorted returns the values of a table that keep, in the order of less.
func sorted[T any](table map[uuid.UUID]T, keep func(T) bool, less func(a, b T) int) []T {
	var out []T
	for _, v := range table {
		if keep(v) {
			out = append(out, v)
		}
	}
	slices.SortFunc(out, less)
	return out
}

// limit keeps at most n rows.
func limit[T any](rows []T, n int32) []T {
	if int32(len(rows)) > n { //nolint:gosec // G115: a table never holds 2^31 rows
		return rows[:n]
	}
	return rows
}
