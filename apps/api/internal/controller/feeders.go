package controller

import (
	"context"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/gen/doelab/v1/doelabv1connect"
	"doelab/api/internal/domain"
	"doelab/api/internal/protomap"
	"doelab/api/internal/service"
)

// Feeders serves doelab.v1.FeederService.
type Feeders struct {
	svc *service.Feeders
}

// NewFeeders builds the handler.
func NewFeeders(svc *service.Feeders) *Feeders {
	return &Feeders{svc: svc}
}

var _ doelabv1connect.FeederServiceHandler = (*Feeders)(nil)

// GetFeeder returns a feeder by id or by code.
func (c *Feeders) GetFeeder(ctx context.Context, req *connect.Request[doelabv1.GetFeederRequest]) (*connect.Response[doelabv1.GetFeederResponse], error) {
	var feeder domain.Feeder
	var err error
	if code := req.Msg.GetCode(); code != "" {
		feeder, err = c.svc.GetByCode(ctx, code)
	} else {
		id, parseErr := parseID("id", req.Msg.GetId())
		if parseErr != nil {
			return nil, parseErr
		}
		feeder, err = c.svc.Get(ctx, id)
	}
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.GetFeederResponse{Feeder: protomap.Feeder(feeder)}), nil
}

// ListFeeders returns a page of feeders.
func (c *Feeders) ListFeeders(ctx context.Context, req *connect.Request[doelabv1.ListFeedersRequest]) (*connect.Response[doelabv1.ListFeedersResponse], error) {
	feeders, next, err := c.svc.List(ctx, page(req.Msg))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.ListFeedersResponse{
		Feeders: protomap.Slice(feeders, protomap.Feeder), NextPageToken: next,
	}), nil
}

// UpdateFeeder applies the masked fields of a feeder.
func (c *Feeders) UpdateFeeder(ctx context.Context, req *connect.Request[doelabv1.UpdateFeederRequest]) (*connect.Response[doelabv1.UpdateFeederResponse], error) {
	id, err := parseID("feeder.id", req.Msg.GetFeeder().GetId())
	if err != nil {
		return nil, err
	}
	paths := req.Msg.GetUpdateMask().GetPaths()
	feeder, err := c.svc.Update(ctx, id, service.FeederPatch{
		Name:  when(paths, "name", req.Msg.GetFeeder().GetName()),
		TapPU: when(paths, "tap_pu", req.Msg.GetFeeder().GetTapPu()),
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.UpdateFeederResponse{Feeder: protomap.Feeder(feeder)}), nil
}

// GetFeederNode returns a node by id.
func (c *Feeders) GetFeederNode(ctx context.Context, req *connect.Request[doelabv1.GetFeederNodeRequest]) (*connect.Response[doelabv1.GetFeederNodeResponse], error) {
	id, err := parseID("id", req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	node, err := c.svc.GetNode(ctx, id)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.GetFeederNodeResponse{FeederNode: protomap.FeederNode(node)}), nil
}

// ListFeederNodes returns a page of a feeder's nodes.
func (c *Feeders) ListFeederNodes(ctx context.Context, req *connect.Request[doelabv1.ListFeederNodesRequest]) (*connect.Response[doelabv1.ListFeederNodesResponse], error) {
	feederID, err := parseID("feeder_id", req.Msg.GetFeederId())
	if err != nil {
		return nil, err
	}
	nodes, next, err := c.svc.ListNodes(ctx, feederID, page(req.Msg))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.ListFeederNodesResponse{
		FeederNodes: protomap.Slice(nodes, protomap.FeederNode), NextPageToken: next,
	}), nil
}

// GetFeederLine returns a line by id.
func (c *Feeders) GetFeederLine(ctx context.Context, req *connect.Request[doelabv1.GetFeederLineRequest]) (*connect.Response[doelabv1.GetFeederLineResponse], error) {
	id, err := parseID("id", req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	line, err := c.svc.GetLine(ctx, id)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.GetFeederLineResponse{FeederLine: protomap.FeederLine(line)}), nil
}

// ListFeederLines returns a page of a feeder's lines.
func (c *Feeders) ListFeederLines(ctx context.Context, req *connect.Request[doelabv1.ListFeederLinesRequest]) (*connect.Response[doelabv1.ListFeederLinesResponse], error) {
	feederID, err := parseID("feeder_id", req.Msg.GetFeederId())
	if err != nil {
		return nil, err
	}
	lines, next, err := c.svc.ListLines(ctx, feederID, page(req.Msg))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.ListFeederLinesResponse{
		FeederLines: protomap.Slice(lines, protomap.FeederLine), NextPageToken: next,
	}), nil
}

// UpdateFeederLine sets or clears a line's rating.
func (c *Feeders) UpdateFeederLine(ctx context.Context, req *connect.Request[doelabv1.UpdateFeederLineRequest]) (*connect.Response[doelabv1.UpdateFeederLineResponse], error) {
	id, err := parseID("feeder_line.id", req.Msg.GetFeederLine().GetId())
	if err != nil {
		return nil, err
	}
	// The mask is exactly ampacity_a. An unset field clears the rating.
	line, err := c.svc.SetLineAmpacity(ctx, id, req.Msg.GetFeederLine().AmpacityA)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.UpdateFeederLineResponse{FeederLine: protomap.FeederLine(line)}), nil
}

// GetFeederForecast returns the load and PV of every site for a range.
func (c *Feeders) GetFeederForecast(ctx context.Context, req *connect.Request[doelabv1.GetFeederForecastRequest]) (*connect.Response[doelabv1.GetFeederForecastResponse], error) {
	feederID, err := parseID("feeder_id", req.Msg.GetFeederId())
	if err != nil {
		return nil, err
	}
	points, err := c.svc.Forecast(ctx, feederID, req.Msg.GetFrom().AsTime(), req.Msg.GetTo().AsTime())
	if err != nil {
		return nil, err
	}
	out := make([]*doelabv1.ForecastPoint, len(points))
	for i, p := range points {
		out[i] = &doelabv1.ForecastPoint{SiteId: p.SiteID.String(), Ts: timestamppb.New(p.TS), LoadW: p.LoadW, PvW: p.PVW}
	}
	return connect.NewResponse(&doelabv1.GetFeederForecastResponse{Points: out}), nil
}
