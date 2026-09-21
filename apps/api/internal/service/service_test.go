package service_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
	"doelab/api/internal/repo/mem"
	"doelab/api/internal/repo/repotest"
	"doelab/api/internal/service"
)

// The rules a service applies on its own, below the wire format. The
// end-to-end tests in internal/server reach most of the services; these are
// the branches that validation at the edge keeps a request from reaching.

func TestSwitchTakesNoRating(t *testing.T) {
	t.Parallel()
	store := mem.New()
	f := repotest.Seed(t, store, "LV10", 1)
	ctx := repotest.Ctx()

	node, err := store.CreateFeederNode(ctx, domain.FeederNode{FeederID: f.Feeder.ID, Name: "past-switch", ParentNodeID: &f.Mid.ID})
	if err != nil {
		t.Fatal(err)
	}
	sw, err := store.CreateFeederLine(ctx, domain.FeederLine{
		FeederID: f.Feeder.ID, Name: "Switch_1_CLOSED", FromNodeID: f.Mid.ID, ToNodeID: node.ID,
		Linecode: "switch", LengthM: 1, IsSwitch: true,
		ROhm: repotest.Matrix(0.001), XOhm: repotest.Matrix(0.001), BS: repotest.Matrix(0),
	})
	if err != nil {
		t.Fatal(err)
	}

	feeders := service.NewFeeders(store)
	_, err = feeders.SetLineAmpacity(ctx, sw.ID, repotest.Ptr(100.0))
	if !errors.Is(err, domain.ErrFailedPrecondition) {
		t.Errorf("rating a switch: %v, want ErrFailedPrecondition", err)
	}
	// Clearing a rating it never had is allowed, and changes nothing.
	got, err := feeders.SetLineAmpacity(ctx, sw.ID, nil)
	if err != nil || got.AmpacityA != nil {
		t.Errorf("clearing a switch's rating: %+v, %v", got, err)
	}
}

func TestEnvelopeConfigBandIsChecked(t *testing.T) {
	t.Parallel()
	store := mem.New()
	f := repotest.Seed(t, store, "LV10", 1)
	configs := service.NewEnvelopeConfigs(store)

	c := repotest.Config(f.Feeder.ID)
	c.VMinPU, c.VMaxPU = 1.05, 1.05
	if _, err := configs.Create(repotest.Ctx(), c); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("an empty band: %v, want ErrInvalid", err)
	}
}

// conflictingStore fails the first n creates of a config with the error that
// a lost race for a version number produces.
type conflictingStore struct {
	*mem.Store
	conflicts atomic.Int32
}

func (s *conflictingStore) Tx(ctx context.Context, fn func(context.Context, service.Repos) error) error {
	return s.Store.Tx(ctx, func(ctx context.Context, r service.Repos) error {
		return fn(ctx, conflictingRepos{Repos: r, s: s})
	})
}

type conflictingRepos struct {
	service.Repos
	s *conflictingStore
}

func (r conflictingRepos) CreateEnvelopeConfig(ctx context.Context, c domain.EnvelopeConfig) (domain.EnvelopeConfig, error) {
	if r.s.conflicts.Add(-1) >= 0 {
		return domain.EnvelopeConfig{}, domain.ErrAlreadyExists
	}
	return r.Repos.CreateEnvelopeConfig(ctx, c)
}

func TestEnvelopeConfigCreateRetriesALostRace(t *testing.T) {
	t.Parallel()
	base := mem.New()
	f := repotest.Seed(t, base, "LV10", 1)

	// Two lost races, then a win.
	store := &conflictingStore{Store: base}
	store.conflicts.Store(2)
	created, err := service.NewEnvelopeConfigs(store).Create(repotest.Ctx(), repotest.Config(f.Feeder.ID))
	if err != nil || created.Version != 1 || created.CreatedBy != "operator" {
		t.Errorf("after two conflicts: %+v, %v", created, err)
	}

	// It gives up rather than spin.
	store.conflicts.Store(100)
	_, err = service.NewEnvelopeConfigs(store).Create(repotest.Ctx(), repotest.Config(f.Feeder.ID))
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Errorf("after endless conflicts: %v, want ErrAlreadyExists", err)
	}
	if left := store.conflicts.Load(); left != 97 {
		t.Errorf("it tried %d times, want 3", 100-left)
	}
}

func TestAnonymousConfigAuthor(t *testing.T) {
	t.Parallel()
	store := mem.New()
	f := repotest.Seed(t, store, "LV10", 1)
	// A caller with no actor on the context, as the import command is.
	created, err := service.NewEnvelopeConfigs(store).Create(context.Background(), repotest.Config(f.Feeder.ID))
	if err != nil || created.CreatedBy != "anonymous" {
		t.Errorf("created = %+v, %v", created, err)
	}
}

func TestUpdatesOfMissingRows(t *testing.T) {
	t.Parallel()
	store := mem.New()
	ctx := repotest.Ctx()
	if _, err := service.NewFeeders(store).SetLineAmpacity(ctx, uuid.New(), nil); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("rating an unknown line: %v", err)
	}
	if _, err := service.NewDevices(store).SetRating(ctx, uuid.New(), 1); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("rating an unknown device: %v", err)
	}
}
