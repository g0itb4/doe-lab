package service

import (
	"context"
	"time"
)

// Retention removes the history that has grown old. The API runs it on a
// timer; what it keeps is counted in feeder time, so a demo that runs the
// clock fast keeps the same number of feeder days as one that does not.
type Retention struct {
	store Store
	clock Clock
	keep  Keep
}

// Keep is how much history stays.
type Keep struct {
	// Readings is how long a reading is kept, and with it the telemetry
	// charts reach back.
	Readings time.Duration
	// Envelopes is how long an envelope and the run that published it are
	// kept.
	Envelopes time.Duration
	// Alerts is how long a resolved alert and a cleared backstop are kept.
	Alerts time.Duration
}

// NewRetention builds the service.
func NewRetention(store Store, clock Clock, keep Keep) *Retention {
	return &Retention{store: store, clock: clock, keep: keep}
}

// Sweep removes what is older than the service keeps, and returns the number
// of runs, alerts and backstops it removed. Readings and envelopes go in
// whole chunks and are not counted.
func (r *Retention) Sweep(ctx context.Context) (int, error) {
	now := r.clock.Now()
	return r.store.PurgeBefore(ctx, now.Add(-r.keep.Readings), now.Add(-r.keep.Envelopes), now.Add(-r.keep.Alerts))
}
