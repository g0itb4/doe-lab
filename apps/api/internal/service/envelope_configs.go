package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"doelab/api/internal/auth"
	"doelab/api/internal/domain"
)

// EnvelopeConfigs manages the envelope policy of a feeder, as immutable
// versions.
type EnvelopeConfigs struct {
	store Store
}

// NewEnvelopeConfigs builds the service.
func NewEnvelopeConfigs(store Store) *EnvelopeConfigs {
	return &EnvelopeConfigs{store: store}
}

// Get returns a config version by id.
func (s *EnvelopeConfigs) Get(ctx context.Context, id uuid.UUID) (domain.EnvelopeConfig, error) {
	return s.store.GetEnvelopeConfig(ctx, id)
}

// GetActive returns the config in force for a feeder: its highest version.
func (s *EnvelopeConfigs) GetActive(ctx context.Context, feederID uuid.UUID) (domain.EnvelopeConfig, error) {
	return s.store.GetActiveEnvelopeConfig(ctx, feederID)
}

// List returns a page of a feeder's config versions, newest first.
func (s *EnvelopeConfigs) List(ctx context.Context, feederID uuid.UUID, p domain.Page) ([]domain.EnvelopeConfig, string, error) {
	if _, err := s.store.GetFeeder(ctx, feederID); err != nil {
		return nil, "", fmt.Errorf("feeder %s: %w", feederID, err)
	}
	return s.store.ListEnvelopeConfigs(ctx, feederID, page(p))
}

// createAttempts bounds the retries of Create. Two creates at once can pick
// the same version number; the schema lets one through and the other tries
// again with the next number.
const createAttempts = 3

// Create stores c as the next version of its feeder and returns it. The
// version, the author and the time are set here, whatever c holds.
func (s *EnvelopeConfigs) Create(ctx context.Context, c domain.EnvelopeConfig) (domain.EnvelopeConfig, error) {
	if c.VMinPU >= c.VMaxPU {
		return domain.EnvelopeConfig{}, fmt.Errorf("%w: v_min_pu must be below v_max_pu", domain.ErrInvalid)
	}
	c.CreatedBy = auth.ActorName(ctx)

	var out domain.EnvelopeConfig
	var err error
	for range createAttempts {
		err = s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
			if _, err := r.GetFeeder(ctx, c.FeederID); err != nil {
				return fmt.Errorf("%w: feeder %s does not exist", domain.ErrFailedPrecondition, c.FeederID)
			}
			var err error
			out, err = r.CreateEnvelopeConfig(ctx, c)
			return err
		})
		if !errors.Is(err, domain.ErrAlreadyExists) {
			break
		}
	}
	return out, err
}
