package controller

import (
	"context"

	"connectrpc.com/connect"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/gen/doelab/v1/doelabv1connect"
	"doelab/api/internal/protomap"
	"doelab/api/internal/service"
)

// Alerts serves doelab.v1.AlertService.
type Alerts struct {
	svc *service.Alerts
}

// NewAlerts builds the handler.
func NewAlerts(svc *service.Alerts) *Alerts {
	return &Alerts{svc: svc}
}

var _ doelabv1connect.AlertServiceHandler = (*Alerts)(nil)

// GetAlert returns an alert by id.
func (c *Alerts) GetAlert(ctx context.Context, req *connect.Request[doelabv1.GetAlertRequest]) (*connect.Response[doelabv1.GetAlertResponse], error) {
	id, err := parseID("id", req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	alert, err := c.svc.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.GetAlertResponse{Alert: protomap.Alert(alert)}), nil
}

// ListAlerts returns a page of a feeder's alerts, newest first.
func (c *Alerts) ListAlerts(ctx context.Context, req *connect.Request[doelabv1.ListAlertsRequest]) (*connect.Response[doelabv1.ListAlertsResponse], error) {
	feederID, err := parseID("feeder_id", req.Msg.GetFeederId())
	if err != nil {
		return nil, err
	}
	filter := service.AlertFilter{OpenOnly: req.Msg.GetOpenOnly()}
	if req.Msg.SiteId != nil {
		siteID, err := parseID("site_id", req.Msg.GetSiteId())
		if err != nil {
			return nil, err
		}
		filter.SiteID = &siteID
	}
	if req.Msg.Kind != nil {
		kind := protomap.AlertKindFromProto(req.Msg.GetKind())
		filter.Kind = &kind
	}
	alerts, next, err := c.svc.List(ctx, feederID, filter, page(req.Msg))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.ListAlertsResponse{
		Alerts: protomap.Slice(alerts, protomap.Alert), NextPageToken: next,
	}), nil
}

// AcknowledgeAlert records that an operator has seen an alert.
func (c *Alerts) AcknowledgeAlert(ctx context.Context, req *connect.Request[doelabv1.AcknowledgeAlertRequest]) (*connect.Response[doelabv1.AcknowledgeAlertResponse], error) {
	id, err := parseID("id", req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	alert, err := c.svc.Acknowledge(ctx, id)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.AcknowledgeAlertResponse{Alert: protomap.Alert(alert)}), nil
}

// Backstops serves doelab.v1.BackstopService.
type Backstops struct {
	svc *service.Backstops
}

// NewBackstops builds the handler.
func NewBackstops(svc *service.Backstops) *Backstops {
	return &Backstops{svc: svc}
}

var _ doelabv1connect.BackstopServiceHandler = (*Backstops)(nil)

// GetBackstopEvent returns a backstop and the sites it covers.
func (c *Backstops) GetBackstopEvent(ctx context.Context, req *connect.Request[doelabv1.GetBackstopEventRequest]) (*connect.Response[doelabv1.GetBackstopEventResponse], error) {
	id, err := parseID("id", req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	event, sites, err := c.svc.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.GetBackstopEventResponse{
		BackstopEvent: protomap.BackstopEvent(event), SiteIds: protomap.IDs(sites),
	}), nil
}

// ListBackstopEvents returns a page of a feeder's backstops, newest first.
func (c *Backstops) ListBackstopEvents(ctx context.Context, req *connect.Request[doelabv1.ListBackstopEventsRequest]) (*connect.Response[doelabv1.ListBackstopEventsResponse], error) {
	feederID, err := parseID("feeder_id", req.Msg.GetFeederId())
	if err != nil {
		return nil, err
	}
	events, next, err := c.svc.List(ctx, feederID, page(req.Msg))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.ListBackstopEventsResponse{
		BackstopEvents: protomap.Slice(events, protomap.BackstopEvent), NextPageToken: next,
	}), nil
}

// CreateBackstopEvent triggers a backstop.
func (c *Backstops) CreateBackstopEvent(ctx context.Context, req *connect.Request[doelabv1.CreateBackstopEventRequest]) (*connect.Response[doelabv1.CreateBackstopEventResponse], error) {
	event, sites, err := c.svc.Trigger(ctx, protomap.BackstopEventFromProto(req.Msg.GetBackstopEvent()), protomap.ParseIDs(req.Msg.GetSiteIds()))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.CreateBackstopEventResponse{
		BackstopEvent: protomap.BackstopEvent(event), SiteIds: protomap.IDs(sites),
	}), nil
}

// ClearBackstop ends a backstop.
func (c *Backstops) ClearBackstop(ctx context.Context, req *connect.Request[doelabv1.ClearBackstopRequest]) (*connect.Response[doelabv1.ClearBackstopResponse], error) {
	id, err := parseID("id", req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	event, err := c.svc.Clear(ctx, id)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.ClearBackstopResponse{BackstopEvent: protomap.BackstopEvent(event)}), nil
}
