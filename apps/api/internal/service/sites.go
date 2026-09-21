package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
)

// Sites manages customer connection points.
type Sites struct {
	store Store
}

// NewSites builds the service.
func NewSites(store Store) *Sites {
	return &Sites{store: store}
}

// Get returns a site by id.
func (s *Sites) Get(ctx context.Context, id uuid.UUID) (domain.Site, error) {
	return s.store.GetSite(ctx, id)
}

// GetByNMI returns a site by its NMI.
func (s *Sites) GetByNMI(ctx context.Context, nmi string) (domain.Site, error) {
	return s.store.GetSiteByNMI(ctx, nmi)
}

// List returns a page of a feeder's sites in NMI order. A nil phase means
// every phase.
func (s *Sites) List(ctx context.Context, feederID uuid.UUID, phase *int16, p domain.Page) ([]domain.Site, string, error) {
	if _, err := s.store.GetFeeder(ctx, feederID); err != nil {
		return nil, "", fmt.Errorf("feeder %s: %w", feederID, err)
	}
	return s.store.ListSites(ctx, feederID, phase, page(p))
}

// checkSite applies the rules about a site's own content that the wire format
// cannot express.
func checkSite(site domain.Site) error {
	if !domain.ValidNMI(site.NMI) {
		return fmt.Errorf("%w: NMI %q fails its checksum", domain.ErrInvalid, site.NMI)
	}
	if site.HasBattery != (site.BatteryKWh != nil) {
		return fmt.Errorf("%w: battery_kwh must be set exactly when has_battery is true", domain.ErrInvalid)
	}
	return nil
}

// Create stores a new site. Its node must be a node of its feeder.
func (s *Sites) Create(ctx context.Context, site domain.Site) (domain.Site, error) {
	if err := checkSite(site); err != nil {
		return domain.Site{}, err
	}
	var out domain.Site
	err := s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		node, err := r.GetFeederNode(ctx, site.NodeID)
		if err != nil {
			return fmt.Errorf("%w: node %s does not exist", domain.ErrFailedPrecondition, site.NodeID)
		}
		if node.FeederID != site.FeederID {
			return fmt.Errorf("%w: node %s is not a node of feeder %s", domain.ErrFailedPrecondition, node.Name, site.FeederID)
		}
		out, err = r.CreateSite(ctx, site)
		return err
	})
	return out, err
}

// SitePatch names the fields of a site to change. A nil field is left as it
// is. BatteryKWh is nullable, so SetBatteryKWh says whether to change it.
type SitePatch struct {
	PVKW          *float64
	InverterKVA   *float64
	ExportCapW    *float64
	ImportCapW    *float64
	HasBattery    *bool
	SetBatteryKWh bool
	BatteryKWh    *float64
	HasEV         *bool
}

// Update applies a patch to a site.
func (s *Sites) Update(ctx context.Context, id uuid.UUID, patch SitePatch) (domain.Site, error) {
	var out domain.Site
	err := s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		site, err := r.GetSite(ctx, id)
		if err != nil {
			return err
		}
		if patch.PVKW != nil {
			site.PVKW = *patch.PVKW
		}
		if patch.InverterKVA != nil {
			site.InverterKVA = *patch.InverterKVA
		}
		if patch.ExportCapW != nil {
			site.ExportCapW = *patch.ExportCapW
		}
		if patch.ImportCapW != nil {
			site.ImportCapW = *patch.ImportCapW
		}
		if patch.HasBattery != nil {
			site.HasBattery = *patch.HasBattery
		}
		if patch.SetBatteryKWh {
			site.BatteryKWh = patch.BatteryKWh
		}
		if patch.HasEV != nil {
			site.HasEV = *patch.HasEV
		}
		if err := checkSite(site); err != nil {
			return err
		}
		out, err = r.UpdateSite(ctx, site)
		return err
	})
	return out, err
}

// Delete soft-deletes a site.
func (s *Sites) Delete(ctx context.Context, id uuid.UUID) error {
	return s.store.Tx(ctx, func(ctx context.Context, r Repos) error {
		return r.SoftDeleteSite(ctx, id)
	})
}

// ListProfiles returns a page of a site's half-hourly profile with from <= ts
// < to.
func (s *Sites) ListProfiles(ctx context.Context, siteID uuid.UUID, from, to time.Time, p domain.Page) ([]domain.SiteProfile, string, error) {
	if _, err := s.store.GetSite(ctx, siteID); err != nil {
		return nil, "", fmt.Errorf("site %s: %w", siteID, err)
	}
	return s.store.ListSiteProfiles(ctx, siteID, from, to, page(p))
}
