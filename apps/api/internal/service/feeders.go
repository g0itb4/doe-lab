package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
)

// Feeders reads the network model and applies the two changes an operator may
// make to it.
type Feeders struct {
	store Store
}

// NewFeeders builds the service.
func NewFeeders(store Store) *Feeders {
	return &Feeders{store: store}
}

// Get returns a feeder by id.
func (s *Feeders) Get(ctx context.Context, id uuid.UUID) (domain.Feeder, error) {
	return s.store.GetFeeder(ctx, id)
}

// GetByCode returns a feeder by its code.
func (s *Feeders) GetByCode(ctx context.Context, code string) (domain.Feeder, error) {
	return s.store.GetFeederByCode(ctx, code)
}

// List returns a page of feeders in code order.
func (s *Feeders) List(ctx context.Context, p domain.Page) ([]domain.Feeder, string, error) {
	return s.store.ListFeeders(ctx, page(p))
}

// FeederPatch names the fields of a feeder to change. A nil field is left as
// it is.
type FeederPatch struct {
	Name  *string
	TapPU *float64
}

// Update applies a patch to a feeder.
func (s *Feeders) Update(ctx context.Context, id uuid.UUID, patch FeederPatch) (domain.Feeder, error) {
	var out domain.Feeder
	err := s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		f, err := r.GetFeeder(ctx, id)
		if err != nil {
			return err
		}
		if patch.Name != nil {
			f.Name = *patch.Name
		}
		if patch.TapPU != nil {
			f.TapPU = *patch.TapPU
		}
		out, err = r.UpdateFeeder(ctx, f)
		return err
	})
	return out, err
}

// GetNode returns a node by id.
func (s *Feeders) GetNode(ctx context.Context, id uuid.UUID) (domain.FeederNode, error) {
	return s.store.GetFeederNode(ctx, id)
}

// ListNodes returns a page of a feeder's nodes in name order.
func (s *Feeders) ListNodes(ctx context.Context, feederID uuid.UUID, p domain.Page) ([]domain.FeederNode, string, error) {
	if _, err := s.store.GetFeeder(ctx, feederID); err != nil {
		return nil, "", fmt.Errorf("feeder %s: %w", feederID, err)
	}
	return s.store.ListFeederNodes(ctx, feederID, page(p))
}

// GetLine returns a line by id.
func (s *Feeders) GetLine(ctx context.Context, id uuid.UUID) (domain.FeederLine, error) {
	return s.store.GetFeederLine(ctx, id)
}

// ListLines returns a page of a feeder's lines in name order.
func (s *Feeders) ListLines(ctx context.Context, feederID uuid.UUID, p domain.Page) ([]domain.FeederLine, string, error) {
	if _, err := s.store.GetFeeder(ctx, feederID); err != nil {
		return nil, "", fmt.Errorf("feeder %s: %w", feederID, err)
	}
	return s.store.ListFeederLines(ctx, feederID, page(p))
}

// SetLineAmpacity sets a line's current rating, which becomes the operator's
// rating. A nil rating makes the line unrated. A switch has no rating to set.
func (s *Feeders) SetLineAmpacity(ctx context.Context, id uuid.UUID, ampacityA *float64) (domain.FeederLine, error) {
	var out domain.FeederLine
	err := s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		line, err := r.GetFeederLine(ctx, id)
		if err != nil {
			return err
		}
		if line.IsSwitch && ampacityA != nil {
			return fmt.Errorf("%w: %s is a switch and takes no rating", domain.ErrFailedPrecondition, line.Name)
		}
		out, err = r.UpdateFeederLineAmpacity(ctx, id, ampacityA)
		return err
	})
	return out, err
}
