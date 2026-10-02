package mem

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
	"doelab/api/internal/repo/pagetoken"
	"doelab/api/internal/service"
)

func (r *repos) GetSite(_ context.Context, id uuid.UUID) (domain.Site, error) {
	defer r.lock()()
	s, ok := r.s.st.sites[id]
	if !ok || s.DeletedAt != nil {
		return domain.Site{}, notFound("site", id)
	}
	return s, nil
}

func (r *repos) GetSiteByNMI(_ context.Context, nmi string) (domain.Site, error) {
	defer r.lock()()
	for _, s := range r.s.st.sites {
		if s.NMI == nmi && s.DeletedAt == nil {
			return s, nil
		}
	}
	return domain.Site{}, notFound("site", nmi)
}

func (r *repos) feederSites(feederID uuid.UUID, phase *int16, after string) []domain.Site {
	return sorted(r.s.st.sites,
		func(s domain.Site) bool {
			return s.FeederID == feederID && s.DeletedAt == nil && s.NMI > after && (phase == nil || s.Phase == *phase)
		},
		func(a, b domain.Site) int { return cmp.Compare(a.NMI, b.NMI) })
}

func (r *repos) ListSites(_ context.Context, feederID uuid.UUID, phase *int16, page domain.Page) ([]domain.Site, string, error) {
	defer r.lock()()
	after, err := pagetoken.Decode(page.Token, 1)
	if err != nil {
		return nil, "", err
	}
	rows, next := pagetoken.Next(limit(r.feederSites(feederID, phase, after[0]), page.Size+1), page.Size,
		func(s domain.Site) []string { return []string{s.NMI} })
	return rows, next, nil
}

func (r *repos) ListAllSites(_ context.Context, feederID uuid.UUID) ([]domain.Site, error) {
	defer r.lock()()
	return r.feederSites(feederID, nil, ""), nil
}

func (r *repos) ListLocatedSites(_ context.Context, page domain.Page) ([]domain.Site, string, error) {
	defer r.lock()()
	after, err := pagetoken.Decode(page.Token, 1)
	if err != nil {
		return nil, "", err
	}
	rows := sorted(r.s.st.sites,
		func(s domain.Site) bool { return s.LatitudeDeg != nil && s.DeletedAt == nil && s.NMI > after[0] },
		func(a, b domain.Site) int { return cmp.Compare(a.NMI, b.NMI) })
	rows, next := pagetoken.Next(limit(rows, page.Size+1), page.Size,
		func(s domain.Site) []string { return []string{s.NMI} })
	return rows, next, nil
}

func (r *repos) CreateSite(_ context.Context, s domain.Site) (domain.Site, error) {
	defer r.lock()()
	if !domain.ValidNMI(s.NMI) {
		return domain.Site{}, fmt.Errorf("sites_nmi_checksum: %w", domain.ErrInvalid)
	}
	if (s.LatitudeDeg == nil) != (s.LongitudeDeg == nil) {
		return domain.Site{}, fmt.Errorf("sites_location_complete: %w", domain.ErrInvalid)
	}
	if s.LatitudeDeg != nil && !located(*s.LatitudeDeg, *s.LongitudeDeg) {
		return domain.Site{}, fmt.Errorf("sites_location_range: %w", domain.ErrInvalid)
	}
	if node, ok := r.s.st.nodes[s.NodeID]; !ok || node.FeederID != s.FeederID {
		return domain.Site{}, fmt.Errorf("sites_node_fkey: %w", domain.ErrFailedPrecondition)
	}
	for _, other := range r.s.st.sites {
		// The NMI is unique for good: a deleted site keeps it.
		if other.NMI == s.NMI {
			return domain.Site{}, fmt.Errorf("sites_nmi_key: %w", domain.ErrAlreadyExists)
		}
		if other.FeederID == s.FeederID && other.Name == s.Name {
			return domain.Site{}, fmt.Errorf("sites_name_key: %w", domain.ErrAlreadyExists)
		}
	}
	s.ID = uuid.Must(uuid.NewV7())
	s.CreatedAt = r.now()
	s.UpdatedAt = s.CreatedAt
	s.DeletedAt = nil
	r.s.st.sites[s.ID] = s
	return s, nil
}

func (r *repos) UpdateSite(_ context.Context, s domain.Site) (domain.Site, error) {
	defer r.lock()()
	current, ok := r.s.st.sites[s.ID]
	if !ok || current.DeletedAt != nil {
		return domain.Site{}, notFound("site", s.ID)
	}
	current.PVKW, current.InverterKVA = s.PVKW, s.InverterKVA
	current.ExportCapW, current.ImportCapW = s.ExportCapW, s.ImportCapW
	current.HasBattery, current.BatteryKWh, current.HasEV = s.HasBattery, s.BatteryKWh, s.HasEV
	current.UpdatedAt = r.now()
	r.s.st.sites[s.ID] = current
	return current, nil
}

func (r *repos) SoftDeleteSite(_ context.Context, id uuid.UUID) error {
	defer r.lock()()
	s, ok := r.s.st.sites[id]
	if !ok || s.DeletedAt != nil {
		return notFound("site", id)
	}
	now := r.now()
	s.DeletedAt, s.UpdatedAt = &now, now
	r.s.st.sites[id] = s
	// The schema cascades a soft delete to the site's devices.
	for deviceID, d := range r.s.st.devices {
		if d.SiteID == id && d.DeletedAt == nil {
			d.DeletedAt, d.UpdatedAt = &now, now
			r.s.st.devices[deviceID] = d
		}
	}
	return nil
}

func (r *repos) ListSiteProfiles(_ context.Context, siteID uuid.UUID, from, to time.Time, page domain.Page) ([]domain.SiteProfile, string, error) {
	defer r.lock()()
	after, err := pagetoken.Decode(page.Token, 1)
	if err != nil {
		return nil, "", err
	}
	start := from
	if after[0] != "" {
		last, err := time.Parse(time.RFC3339Nano, after[0])
		if err != nil {
			return nil, "", fmt.Errorf("%w: malformed page token", domain.ErrInvalid)
		}
		start = last.Add(time.Nanosecond)
	}
	var rows []domain.SiteProfile
	for _, p := range r.s.st.profiles[siteID] {
		if !p.TS.Before(start) && p.TS.Before(to) {
			rows = append(rows, p)
		}
	}
	rows, next := pagetoken.Next(limit(rows, page.Size+1), page.Size,
		func(p domain.SiteProfile) []string { return []string{p.TS.UTC().Format(time.RFC3339Nano)} })
	return rows, next, nil
}

func (r *repos) ListFeederProfiles(_ context.Context, feederID uuid.UUID, from, to time.Time) ([]domain.SiteProfile, error) {
	defer r.lock()()
	nmi := map[uuid.UUID]string{}
	var rows []domain.SiteProfile
	for _, s := range r.feederSites(feederID, nil, "") {
		nmi[s.ID] = s.NMI
		for _, p := range r.s.st.profiles[s.ID] {
			if !p.TS.Before(from) && p.TS.Before(to) {
				rows = append(rows, p)
			}
		}
	}
	slices.SortFunc(rows, func(a, b domain.SiteProfile) int {
		return cmp.Or(a.TS.Compare(b.TS), cmp.Compare(nmi[a.SiteID], nmi[b.SiteID]))
	})
	return rows, nil
}

func (r *repos) ReplaceSiteProfiles(_ context.Context, siteID uuid.UUID, rows []domain.SiteProfile) error {
	defer r.lock()()
	if _, ok := r.s.st.sites[siteID]; !ok {
		return fmt.Errorf("site_profiles_site_fkey: %w", domain.ErrFailedPrecondition)
	}
	kept := make([]domain.SiteProfile, len(rows))
	for i, p := range rows {
		p.SiteID = siteID
		p.TS = p.TS.UTC()
		kept[i] = p
	}
	slices.SortFunc(kept, func(a, b domain.SiteProfile) int { return a.TS.Compare(b.TS) })
	for i := 1; i < len(kept); i++ {
		if kept[i].TS.Equal(kept[i-1].TS) {
			return fmt.Errorf("site_profiles_pkey: %w", domain.ErrAlreadyExists)
		}
	}
	r.s.st.profiles[siteID] = kept
	return nil
}

func (r *repos) GetDevice(_ context.Context, id uuid.UUID) (domain.Device, error) {
	defer r.lock()()
	d, ok := r.s.st.devices[id]
	if !ok || d.DeletedAt != nil {
		return domain.Device{}, notFound("device", id)
	}
	return d, nil
}

func (r *repos) ListDevices(_ context.Context, filter service.DeviceFilter, page domain.Page) ([]domain.Device, string, error) {
	defer r.lock()()
	after, err := pagetoken.Decode(page.Token, 1)
	if err != nil {
		return nil, "", err
	}
	rows := sorted(r.s.st.devices,
		func(d domain.Device) bool {
			return d.DeletedAt == nil && d.ID.String() > after[0] &&
				(filter.SiteID == nil || d.SiteID == *filter.SiteID) &&
				(filter.DERType == nil || d.DERType == *filter.DERType)
		},
		func(a, b domain.Device) int { return cmp.Compare(a.ID.String(), b.ID.String()) })
	rows, next := pagetoken.Next(limit(rows, page.Size+1), page.Size,
		func(d domain.Device) []string { return []string{d.ID.String()} })
	return rows, next, nil
}

func (r *repos) CreateDevice(_ context.Context, d domain.Device) (domain.Device, error) {
	defer r.lock()()
	if site, ok := r.s.st.sites[d.SiteID]; !ok || site.DeletedAt != nil {
		return domain.Device{}, fmt.Errorf("devices_site_fkey: %w", domain.ErrFailedPrecondition)
	}
	for _, other := range r.s.st.devices {
		if other.SiteID == d.SiteID && other.DERType == d.DERType && other.DeletedAt == nil {
			return domain.Device{}, fmt.Errorf("devices_site_type_live_key: %w", domain.ErrAlreadyExists)
		}
	}
	d.ID = uuid.Must(uuid.NewV7())
	d.CreatedAt = r.now()
	d.UpdatedAt = d.CreatedAt
	d.DeletedAt = nil
	r.s.st.devices[d.ID] = d
	return d, nil
}

func (r *repos) UpdateDevice(_ context.Context, d domain.Device) (domain.Device, error) {
	defer r.lock()()
	current, ok := r.s.st.devices[d.ID]
	if !ok || current.DeletedAt != nil {
		return domain.Device{}, notFound("device", d.ID)
	}
	current.RatedW = d.RatedW
	current.UpdatedAt = r.now()
	r.s.st.devices[d.ID] = current
	return current, nil
}

func (r *repos) SoftDeleteDevice(_ context.Context, id uuid.UUID) error {
	defer r.lock()()
	d, ok := r.s.st.devices[id]
	if !ok || d.DeletedAt != nil {
		return notFound("device", id)
	}
	now := r.now()
	d.DeletedAt, d.UpdatedAt = &now, now
	r.s.st.devices[id] = d
	return nil
}
