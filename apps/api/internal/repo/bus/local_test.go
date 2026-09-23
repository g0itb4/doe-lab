package bus

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func pending(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func TestLocalFanOut(t *testing.T) {
	t.Parallel()
	b := NewLocal()
	ctx := context.Background()
	siteA, siteB := uuid.New(), uuid.New()

	a1, cancelA1 := b.Subscribe(siteA)
	a2, cancelA2 := b.Subscribe(siteA)
	b1, cancelB1 := b.Subscribe(siteB)
	if b.Subscribers() != 3 {
		t.Fatalf("%d subscribers, want 3", b.Subscribers())
	}

	if err := b.Notify(ctx, []uuid.UUID{siteA}); err != nil {
		t.Fatal(err)
	}
	if !pending(a1) || !pending(a2) {
		t.Error("a subscriber of the site was not signalled")
	}
	if pending(b1) {
		t.Error("a subscriber of another site was signalled")
	}

	// Several sites in one call; a site with no subscriber is fine.
	if err := b.Notify(ctx, []uuid.UUID{siteA, siteB, uuid.New()}); err != nil {
		t.Fatal(err)
	}
	if !pending(a1) || !pending(b1) {
		t.Error("a batch did not reach both sites")
	}

	// After cancel, no more signals, and cancelling twice is safe.
	cancelA1()
	cancelA1()
	_ = b.Notify(ctx, []uuid.UUID{siteA})
	if pending(a1) || !pending(a2) {
		t.Error("cancel did not end exactly one subscription")
	}
	cancelA2()
	cancelB1()
	if b.Subscribers() != 0 {
		t.Errorf("%d subscribers left after every cancel", b.Subscribers())
	}
}

// A subscriber that does not read holds up nobody, and when it does read it
// finds one signal, not a backlog.
func TestSlowSubscriberDoesNotBlock(t *testing.T) {
	t.Parallel()
	b := NewLocal()
	ctx := context.Background()
	site := uuid.New()
	slow, cancelSlow := b.Subscribe(site)
	defer cancelSlow()
	fast, cancelFast := b.Subscribe(site)
	defer cancelFast()

	// A thousand notifications while the slow subscriber reads nothing. If
	// Notify blocked on it, this would never return.
	received := 0
	for range 1000 {
		if err := b.Notify(ctx, []uuid.UUID{site}); err != nil {
			t.Fatal(err)
		}
		if pending(fast) {
			received++
		}
	}
	if received != 1000 {
		t.Errorf("the fast subscriber got %d of 1000 signals", received)
	}
	if !pending(slow) {
		t.Error("the slow subscriber has no signal waiting")
	}
	if pending(slow) {
		t.Error("the slow subscriber has a backlog; signals should merge into one")
	}
}

func TestConcurrentUse(t *testing.T) {
	t.Parallel()
	b := NewLocal()
	site := uuid.New()
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				ch, cancel := b.Subscribe(site)
				_ = b.Notify(context.Background(), []uuid.UUID{site})
				<-ch
				cancel()
			}
		}()
	}
	wg.Wait()
	if b.Subscribers() != 0 {
		t.Errorf("%d subscribers left", b.Subscribers())
	}
}
