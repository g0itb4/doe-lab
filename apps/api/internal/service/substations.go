package service

import (
	"context"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
)

// Substations reads the zone substations: where on the map the feeders are.
type Substations struct {
	store Store
}

// NewSubstations builds the service.
func NewSubstations(store Store) *Substations {
	return &Substations{store: store}
}

// Get returns a substation by id.
func (s *Substations) Get(ctx context.Context, id uuid.UUID) (domain.Substation, error) {
	return s.store.GetSubstation(ctx, id)
}

// GetByCode returns a substation by its code.
func (s *Substations) GetByCode(ctx context.Context, code string) (domain.Substation, error) {
	return s.store.GetSubstationByCode(ctx, code)
}

// List returns a page of substations in code order.
func (s *Substations) List(ctx context.Context, p domain.Page) ([]domain.Substation, string, error) {
	return s.store.ListSubstations(ctx, page(p))
}
