// Package bus implements service.EnvelopeBus: the signal that tells an open
// subscription that the envelope of its site may have changed.
//
// Local fans a signal out to the subscribers in this process. Postgres does
// the same across API instances with LISTEN and NOTIFY, and delivers through a
// Local on each one. A NATS adapter could replace Postgres behind the same
// interface without any service changing.
package bus

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"doelab/api/internal/service"
)

// Local is an in-process fan-out, keyed by site.
type Local struct {
	mu   sync.Mutex
	subs map[uuid.UUID]map[*subscriber]struct{}
}

// subscriber holds one pending signal at most. A notification that finds the
// slot full is dropped: the subscriber already knows it has to look, and it
// will read the current envelope when it does. So a slow subscriber never
// holds up a publisher and never builds a backlog.
type subscriber struct {
	signal chan struct{}
}

// NewLocal returns an empty bus.
func NewLocal() *Local {
	return &Local{subs: map[uuid.UUID]map[*subscriber]struct{}{}}
}

var _ service.EnvelopeBus = (*Local)(nil)

// Notify signals the subscribers of each site. It never blocks.
func (b *Local) Notify(_ context.Context, siteIDs []uuid.UUID) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, id := range siteIDs {
		for sub := range b.subs[id] {
			select {
			case sub.signal <- struct{}{}:
			default:
			}
		}
	}
	return nil
}

// Subscribe registers a subscriber of one site.
func (b *Local) Subscribe(siteID uuid.UUID) (<-chan struct{}, func()) {
	sub := &subscriber{signal: make(chan struct{}, 1)}
	b.mu.Lock()
	if b.subs[siteID] == nil {
		b.subs[siteID] = map[*subscriber]struct{}{}
	}
	b.subs[siteID][sub] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	return sub.signal, func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			delete(b.subs[siteID], sub)
			if len(b.subs[siteID]) == 0 {
				delete(b.subs, siteID)
			}
		})
	}
}

// Subscribers is the number of open subscriptions, for metrics and tests.
func (b *Local) Subscribers() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, subs := range b.subs {
		n += len(subs)
	}
	return n
}
