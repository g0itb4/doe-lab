package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/auth"
	"doelab/api/internal/domain"
	"doelab/api/internal/repo/bus"
	"doelab/api/internal/repo/mem"
	"doelab/api/internal/repo/repotest"
	"doelab/api/internal/service"
)

// faulty is the in-memory store with one repository call made to fail, inside
// a transaction and outside one. It stands for the database going away in the
// middle of a request: the service must pass the error on, and leave nothing
// half done.
type faulty struct {
	*mem.Store
	fail string
}

var errDown = errors.New("the database is gone")

func (f *faulty) Tx(ctx context.Context, fn func(context.Context, service.Repos) error) error {
	return f.Store.Tx(ctx, func(ctx context.Context, r service.Repos) error {
		return fn(ctx, faultyRepos{Repos: r, fail: f.fail})
	})
}

func (f *faulty) GetCurrentEnvelope(ctx context.Context, siteID uuid.UUID, at time.Time) (domain.Envelope, error) {
	if f.fail == "GetCurrentEnvelope" {
		return domain.Envelope{}, errDown
	}
	return f.Store.GetCurrentEnvelope(ctx, siteID, at)
}

func (f *faulty) ListAllSites(ctx context.Context, feederID uuid.UUID) ([]domain.Site, error) {
	if f.fail == "ListAllSites" {
		return nil, errDown
	}
	return f.Store.ListAllSites(ctx, feederID)
}

func (f *faulty) ListFeederProfiles(ctx context.Context, feederID uuid.UUID, from, to time.Time) ([]domain.SiteProfile, error) {
	if f.fail == "ListFeederProfiles" {
		return nil, errDown
	}
	return f.Store.ListFeederProfiles(ctx, feederID, from, to)
}

type faultyRepos struct {
	service.Repos
	fail string
}

func (r faultyRepos) CreateEnvelopeRun(ctx context.Context, run domain.EnvelopeRun) (domain.EnvelopeRun, bool, error) {
	if r.fail == "CreateEnvelopeRun" {
		return domain.EnvelopeRun{}, false, errDown
	}
	return r.Repos.CreateEnvelopeRun(ctx, run)
}

func (r faultyRepos) ClaimIdempotencyKey(ctx context.Context, key domain.IdempotencyKey) (domain.IdempotencyKey, bool, error) {
	if r.fail == "ClaimIdempotencyKey" {
		return domain.IdempotencyKey{}, false, errDown
	}
	return r.Repos.ClaimIdempotencyKey(ctx, key)
}

func (r faultyRepos) GetEnvelopeConfig(ctx context.Context, id uuid.UUID) (domain.EnvelopeConfig, error) {
	if r.fail == "GetEnvelopeConfig" {
		return domain.EnvelopeConfig{}, errDown
	}
	return r.Repos.GetEnvelopeConfig(ctx, id)
}

func (r faultyRepos) ListAllSites(ctx context.Context, feederID uuid.UUID) ([]domain.Site, error) {
	if r.fail == "ListAllSites" {
		return nil, errDown
	}
	return r.Repos.ListAllSites(ctx, feederID)
}

func (r faultyRepos) ReplaceEnvelopes(ctx context.Context, envelopes []domain.Envelope) ([]domain.Envelope, int, error) {
	if r.fail == "ReplaceEnvelopes" {
		return nil, 0, errDown
	}
	return r.Repos.ReplaceEnvelopes(ctx, envelopes)
}

func (r faultyRepos) AddEnvelopeRunCount(ctx context.Context, id uuid.UUID, added int32) error {
	if r.fail == "AddEnvelopeRunCount" {
		return errDown
	}
	return r.Repos.AddEnvelopeRunCount(ctx, id, added)
}

// fixedClock is feeder time standing still.
type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time                { return c.now }
func (c fixedClock) Until(time.Time) time.Duration { return time.Hour }

// scene is a store with a feeder, a config and a running run.
type scene struct {
	store *faulty
	f     repotest.Fixture
	run   domain.EnvelopeRun
	svc   *service.Envelopes
	bus   *bus.Local
}

func newScene(t *testing.T) *scene {
	t.Helper()
	store := &faulty{Store: mem.New()}
	f := repotest.Seed(t, store.Store, "LV10", 1)
	ctx := repotest.Ctx()
	config, err := store.CreateEnvelopeConfig(ctx, repotest.Config(f.Feeder.ID))
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := store.CreateEnvelopeRun(ctx, repotest.NewRun(f.Feeder.ID, config.ID, "run-0001"))
	if err != nil {
		t.Fatal(err)
	}
	b := bus.NewLocal()
	return &scene{store: store, f: f, run: run, bus: b, svc: service.NewEnvelopes(store, b, fixedClock{now: repotest.Day})}
}

// A publish that fails part of the way leaves nothing behind: no envelope, no
// claimed key, no count.
func TestPublishFailuresLeaveNothingBehind(t *testing.T) {
	t.Parallel()
	for _, step := range []string{"ClaimIdempotencyKey", "GetEnvelopeConfig", "ListAllSites", "ReplaceEnvelopes", "AddEnvelopeRunCount"} {
		t.Run(step, func(t *testing.T) {
			t.Parallel()
			s := newScene(t)
			ctx := repotest.Ctx()
			signal, cancel := s.bus.Subscribe(s.f.SiteA.ID)
			defer cancel()
			batch := []domain.Envelope{repotest.Envelope(s.f.SiteA.ID, s.run.ID, 0, 1000)}

			s.store.fail = step
			if _, err := s.svc.Publish(ctx, s.run.ID, "batch-0001", batch); !errors.Is(err, errDown) {
				t.Fatalf("error = %v, want the store's", err)
			}
			s.store.fail = ""

			if e, err := s.svc.Current(ctx, s.f.SiteA.ID, repotest.Day); err != nil || e != nil {
				t.Errorf("an envelope was left behind: %+v, %v", e, err)
			}
			if run, _ := s.store.GetEnvelopeRun(ctx, s.run.ID); run.EnvelopeCount != 0 {
				t.Errorf("the run counts %d envelopes", run.EnvelopeCount)
			}
			select {
			case <-signal:
				t.Error("subscribers were told about a publish that failed")
			default:
			}
			// The key is free: the same batch goes through once the store is back.
			result, err := s.svc.Publish(ctx, s.run.ID, "batch-0001", batch)
			if err != nil || result.Published != 1 || result.Replayed {
				t.Errorf("the retry: %+v, %v", result, err)
			}
			select {
			case <-signal:
			default:
				t.Error("subscribers were not told about the publish that worked")
			}
		})
	}
}

func TestEnvelopeServiceStoreFailures(t *testing.T) {
	t.Parallel()
	ctx := repotest.Ctx()

	s := newScene(t)
	s.store.fail = "GetCurrentEnvelope"
	if _, err := s.svc.Current(ctx, s.f.SiteA.ID, repotest.Day); !errors.Is(err, errDown) {
		t.Errorf("Current: %v", err)
	}
	// A subscription whose read fails ends with the error.
	device := auth.NewContext(context.Background(), auth.Actor{Scope: auth.ScopeDevice, NMI: s.f.SiteA.NMI})
	err := s.svc.Follow(device, s.f.SiteA.NMI, func(*domain.Envelope, time.Time, bool) error { return nil })
	if !errors.Is(err, errDown) {
		t.Errorf("Follow with a failing read: %v", err)
	}
	s.store.fail = ""
	// A subscription whose send fails ends with that error: the client has gone.
	gone := errors.New("broken pipe")
	err = s.svc.Follow(device, s.f.SiteA.NMI, func(*domain.Envelope, time.Time, bool) error { return gone })
	if !errors.Is(err, gone) {
		t.Errorf("Follow with a failing send: %v", err)
	}
	if n := s.bus.Subscribers(); n != 0 {
		t.Errorf("%d subscriptions left open", n)
	}

	runs := service.NewEnvelopeRuns(s.store)
	s.store.fail = "CreateEnvelopeRun"
	if _, err := runs.Create(ctx, repotest.NewRun(s.f.Feeder.ID, s.run.EnvelopeConfigID, "run-0002")); !errors.Is(err, errDown) {
		t.Errorf("Create: %v", err)
	}
	s.store.fail = ""

	// The two halves of a result must agree, whoever calls.
	reason := "x"
	if _, err := runs.Complete(ctx, s.run.ID, service.RunResult{Status: domain.RunFailed}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("failed with no error: %v", err)
	}
	if _, err := runs.Complete(ctx, s.run.ID, service.RunResult{Status: domain.RunCompleted, Error: &reason}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("completed with an error: %v", err)
	}
}

func TestForecastFailures(t *testing.T) {
	t.Parallel()
	ctx := repotest.Ctx()
	s := newScene(t)
	feeders := service.NewFeeders(s.store)
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	if _, err := feeders.Forecast(ctx, s.f.Feeder.ID, day, day); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("an empty range: %v", err)
	}
	if _, err := feeders.Forecast(ctx, s.f.Feeder.ID, day, day.Add(8*24*time.Hour)); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("eight days: %v", err)
	}
	for _, step := range []string{"ListAllSites", "ListFeederProfiles"} {
		s.store.fail = step
		if _, err := feeders.Forecast(ctx, s.f.Feeder.ID, day, day.Add(time.Hour)); !errors.Is(err, errDown) {
			t.Errorf("with %s failing: %v", step, err)
		}
	}
	s.store.fail = ""

	// A feeder whose zone is not a zone.
	odd := repotest.NewFeeder("ODD")
	odd.Timezone = "Mars/Olympus_Mons"
	created, err := s.store.CreateFeeder(ctx, odd)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := feeders.Forecast(ctx, created.ID, day, day.Add(time.Hour)); err == nil || !strings.Contains(err.Error(), "Mars/Olympus_Mons") {
		t.Errorf("an unknown zone: %v", err)
	}
}
