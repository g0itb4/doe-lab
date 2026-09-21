package controller

import (
	"context"

	"connectrpc.com/connect"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/gen/doelab/v1/doelabv1connect"
	"doelab/api/internal/domain"
	"doelab/api/internal/protomap"
	"doelab/api/internal/service"
)

// Sites serves doelab.v1.SiteService.
type Sites struct {
	svc *service.Sites
}

// NewSites builds the handler.
func NewSites(svc *service.Sites) *Sites {
	return &Sites{svc: svc}
}

var _ doelabv1connect.SiteServiceHandler = (*Sites)(nil)

// GetSite returns a site by id or by NMI.
func (c *Sites) GetSite(ctx context.Context, req *connect.Request[doelabv1.GetSiteRequest]) (*connect.Response[doelabv1.GetSiteResponse], error) {
	var site domain.Site
	var err error
	if nmi := req.Msg.GetNmi(); nmi != "" {
		site, err = c.svc.GetByNMI(ctx, nmi)
	} else {
		id, parseErr := parseID("id", req.Msg.GetId())
		if parseErr != nil {
			return nil, parseErr
		}
		site, err = c.svc.Get(ctx, id)
	}
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.GetSiteResponse{Site: protomap.Site(site)}), nil
}

// ListSites returns a page of a feeder's sites.
func (c *Sites) ListSites(ctx context.Context, req *connect.Request[doelabv1.ListSitesRequest]) (*connect.Response[doelabv1.ListSitesResponse], error) {
	feederID, err := parseID("feeder_id", req.Msg.GetFeederId())
	if err != nil {
		return nil, err
	}
	var phase *int16
	if req.Msg.Phase != nil {
		p := int16(req.Msg.GetPhase()) //nolint:gosec // G115: validated as 1 to 3
		phase = &p
	}
	sites, next, err := c.svc.List(ctx, feederID, phase, page(req.Msg))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.ListSitesResponse{
		Sites: protomap.Slice(sites, protomap.Site), NextPageToken: next,
	}), nil
}

// CreateSite creates a site.
func (c *Sites) CreateSite(ctx context.Context, req *connect.Request[doelabv1.CreateSiteRequest]) (*connect.Response[doelabv1.CreateSiteResponse], error) {
	site, err := c.svc.Create(ctx, protomap.SiteFromProto(req.Msg.GetSite()))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.CreateSiteResponse{Site: protomap.Site(site)}), nil
}

// UpdateSite applies the masked fields of a site.
func (c *Sites) UpdateSite(ctx context.Context, req *connect.Request[doelabv1.UpdateSiteRequest]) (*connect.Response[doelabv1.UpdateSiteResponse], error) {
	in := req.Msg.GetSite()
	id, err := parseID("site.id", in.GetId())
	if err != nil {
		return nil, err
	}
	paths := req.Msg.GetUpdateMask().GetPaths()
	site, err := c.svc.Update(ctx, id, service.SitePatch{
		PVKW:          when(paths, "pv_kw", in.GetPvKw()),
		InverterKVA:   when(paths, "inverter_kva", in.GetInverterKva()),
		ExportCapW:    when(paths, "export_cap_w", in.GetExportCapW()),
		ImportCapW:    when(paths, "import_cap_w", in.GetImportCapW()),
		HasBattery:    when(paths, "has_battery", in.GetHasBattery()),
		SetBatteryKWh: masked(paths, "battery_kwh"),
		BatteryKWh:    in.BatteryKwh,
		HasEV:         when(paths, "has_ev", in.GetHasEv()),
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.UpdateSiteResponse{Site: protomap.Site(site)}), nil
}

// DeleteSite soft-deletes a site.
func (c *Sites) DeleteSite(ctx context.Context, req *connect.Request[doelabv1.DeleteSiteRequest]) (*connect.Response[doelabv1.DeleteSiteResponse], error) {
	id, err := parseID("id", req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if err := c.svc.Delete(ctx, id); err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.DeleteSiteResponse{}), nil
}

// ListSiteProfiles returns a page of a site's half-hourly profile.
func (c *Sites) ListSiteProfiles(ctx context.Context, req *connect.Request[doelabv1.ListSiteProfilesRequest]) (*connect.Response[doelabv1.ListSiteProfilesResponse], error) {
	siteID, err := parseID("site_id", req.Msg.GetSiteId())
	if err != nil {
		return nil, err
	}
	rows, next, err := c.svc.ListProfiles(ctx, siteID, req.Msg.GetFrom().AsTime(), req.Msg.GetTo().AsTime(), page(req.Msg))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.ListSiteProfilesResponse{
		SiteProfiles: protomap.Slice(rows, protomap.SiteProfile), NextPageToken: next,
	}), nil
}
