package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"doelab/api/internal/auth"
	"doelab/api/internal/domain"
)

// Alerts shows the alerts of a feeder and lets an operator acknowledge one.
// Opening and resolving alerts is Compliance's work.
type Alerts struct {
	store Store
}

// NewAlerts builds the service.
func NewAlerts(store Store) *Alerts {
	return &Alerts{store: store}
}

// Get returns an alert by id.
func (s *Alerts) Get(ctx context.Context, id uuid.UUID) (domain.Alert, error) {
	return s.store.GetAlert(ctx, id)
}

// List returns a page of a feeder's alerts, newest first.
func (s *Alerts) List(ctx context.Context, feederID uuid.UUID, filter AlertFilter, p domain.Page) ([]domain.Alert, string, error) {
	if _, err := s.store.GetFeeder(ctx, feederID); err != nil {
		return nil, "", fmt.Errorf("feeder %s: %w", feederID, err)
	}
	return s.store.ListAlerts(ctx, feederID, filter, page(p))
}

// Acknowledge records that the caller has seen an alert. Acknowledging one
// that is already acknowledged changes nothing.
func (s *Alerts) Acknowledge(ctx context.Context, id uuid.UUID) (domain.Alert, error) {
	var out domain.Alert
	err := s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		var err error
		out, err = r.AcknowledgeAlert(ctx, id, auth.ActorName(ctx))
		return err
	})
	return out, err
}
