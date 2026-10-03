package service_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
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
	faultyRepos
	Store *mem.Store
}

func newFaulty() *faulty {
	store := mem.New()
	return &faulty{faultyRepos: faultyRepos{Repos: store, plan: &plan{}}, Store: store}
}

var errDown = errors.New("the database is gone")

func (f *faulty) Tx(ctx context.Context, fn func(context.Context, service.Repos) error) error {
	return f.Store.Tx(ctx, func(ctx context.Context, r service.Repos) error {
		return fn(ctx, faultyRepos{Repos: r, plan: f.plan})
	})
}

// failAt names the call that fails from now on: "Method" for every call of
// it, "Method#2" for its second call only, "" for none.
func (f *faulty) failAt(step string) {
	f.plan.mu.Lock()
	defer f.plan.mu.Unlock()
	f.plan.fail, f.plan.calls = step, 0
}

// plan is which call fails. A transaction's repositories and the store's own
// share one.
type plan struct {
	mu    sync.Mutex
	fail  string
	calls int
	// seen is how many times each call was made, failing or not.
	seen map[string]int
}

// count returns how many times a call was made since the store was built.
func (f *faulty) count(method string) int {
	f.plan.mu.Lock()
	defer f.plan.mu.Unlock()
	return f.plan.seen[method]
}

// down returns the failure for a call of method, if the plan has one.
func (p *plan) down(method string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.seen == nil {
		p.seen = map[string]int{}
	}
	p.seen[method]++
	name, nth, counted := strings.Cut(p.fail, "#")
	if method != name {
		return nil
	}
	p.calls++
	if counted && strconv.Itoa(p.calls) != nth {
		return nil
	}
	return errDown
}

type faultyRepos struct {
	service.Repos
	plan *plan
}

func (r faultyRepos) GetCurrentEnvelope(ctx context.Context, siteID uuid.UUID, at time.Time) (domain.Envelope, error) {
	if err := r.plan.down("GetCurrentEnvelope"); err != nil {
		return domain.Envelope{}, err
	}
	return r.Repos.GetCurrentEnvelope(ctx, siteID, at)
}

func (r faultyRepos) ListFeederNodeStates(ctx context.Context, feederID uuid.UUID, at time.Time) ([]domain.FeederNodeState, error) {
	if err := r.plan.down("ListFeederNodeStates"); err != nil {
		return nil, err
	}
	return r.Repos.ListFeederNodeStates(ctx, feederID, at)
}

func (r faultyRepos) ListFeederLineStates(ctx context.Context, feederID uuid.UUID, at time.Time) ([]domain.FeederLineState, error) {
	if err := r.plan.down("ListFeederLineStates"); err != nil {
		return nil, err
	}
	return r.Repos.ListFeederLineStates(ctx, feederID, at)
}

func (r faultyRepos) GetFeeder(ctx context.Context, id uuid.UUID) (domain.Feeder, error) {
	if err := r.plan.down("GetFeeder"); err != nil {
		return domain.Feeder{}, err
	}
	return r.Repos.GetFeeder(ctx, id)
}

func (r faultyRepos) ListFeeders(ctx context.Context, page domain.Page) ([]domain.Feeder, string, error) {
	if err := r.plan.down("ListFeeders"); err != nil {
		return nil, "", err
	}
	return r.Repos.ListFeeders(ctx, page)
}

func (r faultyRepos) ListAllSites(ctx context.Context, feederID uuid.UUID) ([]domain.Site, error) {
	if err := r.plan.down("ListAllSites"); err != nil {
		return nil, err
	}
	return r.Repos.ListAllSites(ctx, feederID)
}

func (r faultyRepos) ListFeederProfiles(ctx context.Context, feederID uuid.UUID, from, to time.Time) ([]domain.SiteProfile, error) {
	if err := r.plan.down("ListFeederProfiles"); err != nil {
		return nil, err
	}
	return r.Repos.ListFeederProfiles(ctx, feederID, from, to)
}

func (r faultyRepos) ListSiteProfiles(ctx context.Context, siteID uuid.UUID, from, to time.Time, page domain.Page) ([]domain.SiteProfile, string, error) {
	if err := r.plan.down("ListSiteProfiles"); err != nil {
		return nil, "", err
	}
	return r.Repos.ListSiteProfiles(ctx, siteID, from, to, page)
}

func (r faultyRepos) ListDevices(ctx context.Context, filter service.DeviceFilter, page domain.Page) ([]domain.Device, string, error) {
	if err := r.plan.down("ListDevices"); err != nil {
		return nil, "", err
	}
	return r.Repos.ListDevices(ctx, filter, page)
}

func (r faultyRepos) CreateEnvelopeRun(ctx context.Context, run domain.EnvelopeRun) (domain.EnvelopeRun, bool, error) {
	if err := r.plan.down("CreateEnvelopeRun"); err != nil {
		return domain.EnvelopeRun{}, false, err
	}
	return r.Repos.CreateEnvelopeRun(ctx, run)
}

func (r faultyRepos) ListEnvelopeRuns(ctx context.Context, feederID uuid.UUID, status *domain.RunStatus, page domain.Page) ([]domain.EnvelopeRun, string, error) {
	if err := r.plan.down("ListEnvelopeRuns"); err != nil {
		return nil, "", err
	}
	return r.Repos.ListEnvelopeRuns(ctx, feederID, status, page)
}

func (r faultyRepos) ClaimIdempotencyKey(ctx context.Context, key domain.IdempotencyKey) (domain.IdempotencyKey, bool, error) {
	if err := r.plan.down("ClaimIdempotencyKey"); err != nil {
		return domain.IdempotencyKey{}, false, err
	}
	return r.Repos.ClaimIdempotencyKey(ctx, key)
}

func (r faultyRepos) GetEnvelopeConfig(ctx context.Context, id uuid.UUID) (domain.EnvelopeConfig, error) {
	if err := r.plan.down("GetEnvelopeConfig"); err != nil {
		return domain.EnvelopeConfig{}, err
	}
	return r.Repos.GetEnvelopeConfig(ctx, id)
}

func (r faultyRepos) GetActiveEnvelopeConfig(ctx context.Context, feederID uuid.UUID) (domain.EnvelopeConfig, error) {
	if err := r.plan.down("GetActiveEnvelopeConfig"); err != nil {
		return domain.EnvelopeConfig{}, err
	}
	return r.Repos.GetActiveEnvelopeConfig(ctx, feederID)
}

func (r faultyRepos) ReplaceEnvelopes(ctx context.Context, envelopes []domain.Envelope) ([]domain.Envelope, int, error) {
	if err := r.plan.down("ReplaceEnvelopes"); err != nil {
		return nil, 0, err
	}
	return r.Repos.ReplaceEnvelopes(ctx, envelopes)
}

func (r faultyRepos) ListRunEnvelopes(ctx context.Context, runID uuid.UUID, page domain.Page) ([]domain.Envelope, string, error) {
	if err := r.plan.down("ListRunEnvelopes"); err != nil {
		return nil, "", err
	}
	return r.Repos.ListRunEnvelopes(ctx, runID, page)
}

func (r faultyRepos) ListFeederEnvelopes(ctx context.Context, feederID uuid.UUID, from, to time.Time) ([]domain.Envelope, error) {
	if err := r.plan.down("ListFeederEnvelopes"); err != nil {
		return nil, err
	}
	return r.Repos.ListFeederEnvelopes(ctx, feederID, from, to)
}

func (r faultyRepos) AddEnvelopeRunCount(ctx context.Context, id uuid.UUID, added int32) error {
	if err := r.plan.down("AddEnvelopeRunCount"); err != nil {
		return err
	}
	return r.Repos.AddEnvelopeRunCount(ctx, id, added)
}

func (r faultyRepos) ListFeederIntervals(ctx context.Context, feederID uuid.UUID, from, to time.Time) ([]domain.EnvelopeRunInterval, error) {
	if err := r.plan.down("ListFeederIntervals"); err != nil {
		return nil, err
	}
	return r.Repos.ListFeederIntervals(ctx, feederID, from, to)
}

func (r faultyRepos) InsertReadings(ctx context.Context, readings []domain.Reading) (int, error) {
	if err := r.plan.down("InsertReadings"); err != nil {
		return 0, err
	}
	return r.Repos.InsertReadings(ctx, readings)
}

func (r faultyRepos) ListSitePower(ctx context.Context, siteID uuid.UUID, from, to time.Time) ([]domain.SitePower, error) {
	if err := r.plan.down("ListSitePower"); err != nil {
		return nil, err
	}
	return r.Repos.ListSitePower(ctx, siteID, from, to)
}

func (r faultyRepos) ListFleetSeries(ctx context.Context, feederID uuid.UUID, from, to time.Time) ([]domain.FleetMinute, error) {
	if err := r.plan.down("ListFleetSeries"); err != nil {
		return nil, err
	}
	return r.Repos.ListFleetSeries(ctx, feederID, from, to)
}

func (r faultyRepos) ListDeviceStates(ctx context.Context, feederID uuid.UUID) ([]domain.DeviceState, error) {
	if err := r.plan.down("ListDeviceStates"); err != nil {
		return nil, err
	}
	return r.Repos.ListDeviceStates(ctx, feederID)
}

func (r faultyRepos) ListAlerts(ctx context.Context, feederID uuid.UUID, filter service.AlertFilter, page domain.Page) ([]domain.Alert, string, error) {
	if err := r.plan.down("ListAlerts"); err != nil {
		return nil, "", err
	}
	return r.Repos.ListAlerts(ctx, feederID, filter, page)
}

func (r faultyRepos) OpenAlert(ctx context.Context, alert domain.Alert) (domain.Alert, bool, error) {
	if err := r.plan.down("OpenAlert"); err != nil {
		return domain.Alert{}, false, err
	}
	return r.Repos.OpenAlert(ctx, alert)
}

func (r faultyRepos) ResolveAlert(ctx context.Context, siteID uuid.UUID, kind domain.AlertKind, at time.Time) (bool, error) {
	if err := r.plan.down("ResolveAlert"); err != nil {
		return false, err
	}
	return r.Repos.ResolveAlert(ctx, siteID, kind, at)
}

func (r faultyRepos) CountOpenAlerts(ctx context.Context, feederID uuid.UUID) (int, error) {
	if err := r.plan.down("CountOpenAlerts"); err != nil {
		return 0, err
	}
	return r.Repos.CountOpenAlerts(ctx, feederID)
}

func (r faultyRepos) GetActiveBackstopEvent(ctx context.Context, feederID uuid.UUID) (domain.BackstopEvent, error) {
	if err := r.plan.down("GetActiveBackstopEvent"); err != nil {
		return domain.BackstopEvent{}, err
	}
	return r.Repos.GetActiveBackstopEvent(ctx, feederID)
}

func (r faultyRepos) GetBackstopEvent(ctx context.Context, id uuid.UUID) (domain.BackstopEvent, error) {
	if err := r.plan.down("GetBackstopEvent"); err != nil {
		return domain.BackstopEvent{}, err
	}
	return r.Repos.GetBackstopEvent(ctx, id)
}

func (r faultyRepos) ListBackstopEvents(ctx context.Context, feederID uuid.UUID, page domain.Page) ([]domain.BackstopEvent, string, error) {
	if err := r.plan.down("ListBackstopEvents"); err != nil {
		return nil, "", err
	}
	return r.Repos.ListBackstopEvents(ctx, feederID, page)
}

func (r faultyRepos) CreateBackstopEvent(ctx context.Context, event domain.BackstopEvent, siteIDs []uuid.UUID) (domain.BackstopEvent, error) {
	if err := r.plan.down("CreateBackstopEvent"); err != nil {
		return domain.BackstopEvent{}, err
	}
	return r.Repos.CreateBackstopEvent(ctx, event, siteIDs)
}

func (r faultyRepos) ListBackstopEventSiteIDs(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error) {
	if err := r.plan.down("ListBackstopEventSiteIDs"); err != nil {
		return nil, err
	}
	return r.Repos.ListBackstopEventSiteIDs(ctx, id)
}

func (r faultyRepos) ClearBackstopEvent(ctx context.Context, id uuid.UUID, by string, at time.Time) (domain.BackstopEvent, error) {
	if err := r.plan.down("ClearBackstopEvent"); err != nil {
		return domain.BackstopEvent{}, err
	}
	return r.Repos.ClearBackstopEvent(ctx, id, by, at)
}

func (r faultyRepos) ListActiveEnvelopes(ctx context.Context, siteIDs []uuid.UUID, from time.Time) ([]domain.Envelope, error) {
	if err := r.plan.down("ListActiveEnvelopes"); err != nil {
		return nil, err
	}
	return r.Repos.ListActiveEnvelopes(ctx, siteIDs, from)
}

func (r faultyRepos) SupersedeBackstopEnvelopes(ctx context.Context, eventID uuid.UUID, from time.Time) (int, error) {
	if err := r.plan.down("SupersedeBackstopEnvelopes"); err != nil {
		return 0, err
	}
	return r.Repos.SupersedeBackstopEnvelopes(ctx, eventID, from)
}

func (r faultyRepos) ListLatestEngineEnvelopes(ctx context.Context, siteIDs []uuid.UUID, from time.Time) ([]domain.Envelope, error) {
	if err := r.plan.down("ListLatestEngineEnvelopes"); err != nil {
		return nil, err
	}
	return r.Repos.ListLatestEngineEnvelopes(ctx, siteIDs, from)
}

// fixedClock is feeder time standing still.
type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time                { return c.now }
func (c fixedClock) Until(time.Time) time.Duration { return time.Hour }

// movedClock is feeder time that moves when a test says so.
type movedClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *movedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *movedClock) Until(time.Time) time.Duration { return time.Hour }

func (c *movedClock) set(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
}

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
	store := newFaulty()
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

			s.store.failAt(step)
			if _, err := s.svc.Publish(ctx, s.run.ID, "batch-0001", batch); !errors.Is(err, errDown) {
				t.Fatalf("error = %v, want the store's", err)
			}
			s.store.failAt("")

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
	s.store.failAt("GetCurrentEnvelope")
	if _, err := s.svc.Current(ctx, s.f.SiteA.ID, repotest.Day); !errors.Is(err, errDown) {
		t.Errorf("Current: %v", err)
	}
	// A subscription whose read fails ends with the error.
	device := auth.NewContext(context.Background(), auth.Actor{Scope: auth.ScopeDevice, NMI: s.f.SiteA.NMI})
	err := s.svc.Follow(device, s.f.SiteA.NMI, func(*domain.Envelope, time.Time, bool) error { return nil })
	if !errors.Is(err, errDown) {
		t.Errorf("Follow with a failing read: %v", err)
	}
	s.store.failAt("")
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
	s.store.failAt("CreateEnvelopeRun")
	if _, err := runs.Create(ctx, repotest.NewRun(s.f.Feeder.ID, s.run.EnvelopeConfigID, "run-0002")); !errors.Is(err, errDown) {
		t.Errorf("Create: %v", err)
	}
	s.store.failAt("")

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
		s.store.failAt(step)
		if _, err := feeders.Forecast(ctx, s.f.Feeder.ID, day, day.Add(time.Hour)); !errors.Is(err, errDown) {
			t.Errorf("with %s failing: %v", step, err)
		}
	}
	s.store.failAt("")

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
