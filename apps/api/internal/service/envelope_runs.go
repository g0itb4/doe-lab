package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
)

// EnvelopeRuns records the runs of the engine.
type EnvelopeRuns struct {
	store Store
	// Metrics is told how each run ended.
	Metrics Recorder
	// Objects is where an export is written. Nil means that this API cannot
	// export.
	Objects ObjectStore
	// now is the wall clock, replaceable in tests.
	now func() time.Time
}

// NewEnvelopeRuns builds the service.
func NewEnvelopeRuns(store Store) *EnvelopeRuns {
	return &EnvelopeRuns{store: store, Metrics: NoRecorder{}, now: time.Now}
}

// Get returns a run by id.
func (s *EnvelopeRuns) Get(ctx context.Context, id uuid.UUID) (domain.EnvelopeRun, error) {
	return s.store.GetEnvelopeRun(ctx, id)
}

// List returns a page of a feeder's runs, newest first. A nil status means
// every status.
func (s *EnvelopeRuns) List(ctx context.Context, feederID uuid.UUID, status *domain.RunStatus, p domain.Page) ([]domain.EnvelopeRun, string, error) {
	if _, err := s.store.GetFeeder(ctx, feederID); err != nil {
		return nil, "", fmt.Errorf("feeder %s: %w", feederID, err)
	}
	return s.store.ListEnvelopeRuns(ctx, feederID, status, page(p))
}

// Create starts a run. With the idempotency key of an existing run it starts
// nothing and returns that run, provided the request is the same one: the
// same key for another feeder, config or horizon is a mistake, and is refused.
func (s *EnvelopeRuns) Create(ctx context.Context, run domain.EnvelopeRun) (domain.EnvelopeRun, error) {
	var out domain.EnvelopeRun
	err := s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		config, err := r.GetEnvelopeConfig(ctx, run.EnvelopeConfigID)
		if err != nil {
			return fmt.Errorf("%w: envelope config %s does not exist", domain.ErrFailedPrecondition, run.EnvelopeConfigID)
		}
		if config.FeederID != run.FeederID {
			return fmt.Errorf("%w: envelope config %s belongs to another feeder", domain.ErrFailedPrecondition, config.ID)
		}
		stored, created, err := r.CreateEnvelopeRun(ctx, run)
		if err != nil {
			return err
		}
		if !created && (stored.FeederID != run.FeederID || stored.EnvelopeConfigID != run.EnvelopeConfigID ||
			!stored.HorizonFrom.Equal(run.HorizonFrom) || !stored.HorizonTo.Equal(run.HorizonTo)) {
			return fmt.Errorf("%w: idempotency key %q was used for a different run", domain.ErrAlreadyExists, run.IdempotencyKey)
		}
		out = stored
		return nil
	})
	return out, err
}

// Complete finishes a run. Completing a finished run with the same outcome
// changes nothing and returns it; with another outcome it is refused.
func (s *EnvelopeRuns) Complete(ctx context.Context, id uuid.UUID, result RunResult) (domain.EnvelopeRun, error) {
	if (result.Status == domain.RunFailed) != (result.Error != nil) {
		return domain.EnvelopeRun{}, fmt.Errorf("%w: a failed run needs an error, and only a failed run", domain.ErrInvalid)
	}
	var out domain.EnvelopeRun
	ended := false
	err := s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		run, err := r.GetEnvelopeRun(ctx, id)
		if err != nil {
			return err
		}
		if run.Status == result.Status {
			out = run
			return nil
		}
		out, err = r.CompleteEnvelopeRun(ctx, id, result)
		ended = err == nil
		return err
	})
	// Once: a completion that is sent again changes nothing, and is not
	// counted again.
	if err == nil && ended {
		s.Metrics.RunCompleted(ctx, result.Status, time.Duration(result.DurationMS)*time.Millisecond)
	}
	return out, err
}

// CreateIntervals records the forecast state of the feeder for intervals of a
// running run. The feeder of each row is the run's, whatever the row says.
func (s *EnvelopeRuns) CreateIntervals(ctx context.Context, runID uuid.UUID, rows []domain.EnvelopeRunInterval) (int, error) {
	err := s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		run, err := r.GetEnvelopeRun(ctx, runID)
		if err != nil {
			return fmt.Errorf("%w: envelope run %s does not exist", domain.ErrFailedPrecondition, runID)
		}
		if run.Status != domain.RunRunning {
			return fmt.Errorf("%w: envelope run %s is %s, not running", domain.ErrFailedPrecondition, runID, run.Status)
		}
		for i := range rows {
			if rows[i].ValidFrom.Before(run.HorizonFrom) || rows[i].ValidTo.After(run.HorizonTo) {
				return fmt.Errorf("%w: interval %s is outside the run's horizon",
					domain.ErrInvalid, rows[i].ValidFrom.UTC().Format("2006-01-02T15:04:05Z"))
			}
			rows[i].EnvelopeRunID, rows[i].FeederID = runID, run.FeederID
		}
		return r.CreateEnvelopeRunIntervals(ctx, rows)
	})
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

// ListIntervals returns the intervals of a run, in time order.
func (s *EnvelopeRuns) ListIntervals(ctx context.Context, runID uuid.UUID) ([]domain.EnvelopeRunInterval, error) {
	if _, err := s.store.GetEnvelopeRun(ctx, runID); err != nil {
		return nil, err
	}
	return s.store.ListEnvelopeRunIntervals(ctx, runID)
}
