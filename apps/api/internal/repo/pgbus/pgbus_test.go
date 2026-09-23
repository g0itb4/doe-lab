package pgbus_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"doelab/api/internal/repo/pgbus"
	"doelab/api/internal/testutil"
)

func TestMain(m *testing.M) { testutil.TestMain(m) }

func signalled(ch <-chan struct{}, within time.Duration) bool {
	select {
	case <-ch:
		return true
	case <-time.After(within):
		return false
	}
}

// Two API instances on one database: a publish through one reaches the
// subscribers of both.
func TestTwoInstancesShareSignals(t *testing.T) {
	t.Parallel()
	dsn := testutil.PostgresURL(t)
	ctx, cancel := context.WithCancel(context.Background())
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	start := func() (*pgbus.Bus, chan error) {
		pool, err := pgxpool.New(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		b := pgbus.New(pool, log)
		done := make(chan error, 1)
		go func() { done <- b.Run(ctx) }()
		select {
		case <-b.Ready():
		case <-time.After(10 * time.Second):
			t.Fatal("the bus did not start listening")
		}
		return b, done
	}
	one, oneDone := start()
	two, twoDone := start()

	site, other := uuid.New(), uuid.New()
	onOne, cancelOne := one.Subscribe(site)
	defer cancelOne()
	onTwo, cancelTwo := two.Subscribe(site)
	defer cancelTwo()
	elsewhere, cancelElsewhere := two.Subscribe(other)
	defer cancelElsewhere()

	if err := one.Notify(ctx, []uuid.UUID{site}); err != nil {
		t.Fatal(err)
	}
	if !signalled(onTwo, 5*time.Second) {
		t.Error("the other instance's subscriber was not signalled")
	}
	if !signalled(onOne, 5*time.Second) {
		t.Error("the publishing instance's own subscriber was not signalled")
	}
	if signalled(elsewhere, 100*time.Millisecond) {
		t.Error("a subscriber of another site was signalled")
	}

	// More sites than fit one notification: every one is delivered.
	many := make([]uuid.UUID, 450)
	for i := range many {
		many[i] = uuid.New()
	}
	many[449] = other
	if err := two.Notify(ctx, many); err != nil {
		t.Fatal(err)
	}
	if !signalled(elsewhere, 5*time.Second) {
		t.Error("the last site of a large batch was not signalled")
	}

	// Run returns when its context ends.
	cancel()
	for _, done := range []chan error{oneDone, twoDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run returned %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Run did not return after cancel")
		}
	}
}
