package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
)

// Devices manages the DER behind each site.
type Devices struct {
	store Store
}

// NewDevices builds the service.
func NewDevices(store Store) *Devices {
	return &Devices{store: store}
}

// Get returns a device by id.
func (s *Devices) Get(ctx context.Context, id uuid.UUID) (domain.Device, error) {
	return s.store.GetDevice(ctx, id)
}

// List returns a page of devices in id order.
func (s *Devices) List(ctx context.Context, filter DeviceFilter, p domain.Page) ([]domain.Device, string, error) {
	return s.store.ListDevices(ctx, filter, page(p))
}

// Create stores a new device on an existing site.
func (s *Devices) Create(ctx context.Context, d domain.Device) (domain.Device, error) {
	var out domain.Device
	err := s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		if _, err := r.GetSite(ctx, d.SiteID); err != nil {
			return fmt.Errorf("%w: site %s does not exist", domain.ErrFailedPrecondition, d.SiteID)
		}
		var err error
		out, err = r.CreateDevice(ctx, d)
		return err
	})
	return out, err
}

// SetRating changes a device's nameplate power.
func (s *Devices) SetRating(ctx context.Context, id uuid.UUID, ratedW float64) (domain.Device, error) {
	var out domain.Device
	err := s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		d, err := r.GetDevice(ctx, id)
		if err != nil {
			return err
		}
		d.RatedW = ratedW
		out, err = r.UpdateDevice(ctx, d)
		return err
	})
	return out, err
}

// Delete soft-deletes a device.
func (s *Devices) Delete(ctx context.Context, id uuid.UUID) error {
	return s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		return r.SoftDeleteDevice(ctx, id)
	})
}
