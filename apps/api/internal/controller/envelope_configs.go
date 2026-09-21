package controller

import (
	"context"

	"connectrpc.com/connect"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/gen/doelab/v1/doelabv1connect"
	"doelab/api/internal/protomap"
	"doelab/api/internal/service"
)

// EnvelopeConfigs serves doelab.v1.EnvelopeConfigService.
type EnvelopeConfigs struct {
	svc *service.EnvelopeConfigs
}

// NewEnvelopeConfigs builds the handler.
func NewEnvelopeConfigs(svc *service.EnvelopeConfigs) *EnvelopeConfigs {
	return &EnvelopeConfigs{svc: svc}
}

var _ doelabv1connect.EnvelopeConfigServiceHandler = (*EnvelopeConfigs)(nil)

// GetEnvelopeConfig returns a config version by id.
func (c *EnvelopeConfigs) GetEnvelopeConfig(ctx context.Context, req *connect.Request[doelabv1.GetEnvelopeConfigRequest]) (*connect.Response[doelabv1.GetEnvelopeConfigResponse], error) {
	id, err := parseID("id", req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	config, err := c.svc.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.GetEnvelopeConfigResponse{EnvelopeConfig: protomap.EnvelopeConfig(config)}), nil
}

// GetActiveEnvelopeConfig returns the config in force for a feeder.
func (c *EnvelopeConfigs) GetActiveEnvelopeConfig(ctx context.Context, req *connect.Request[doelabv1.GetActiveEnvelopeConfigRequest]) (*connect.Response[doelabv1.GetActiveEnvelopeConfigResponse], error) {
	feederID, err := parseID("feeder_id", req.Msg.GetFeederId())
	if err != nil {
		return nil, err
	}
	config, err := c.svc.GetActive(ctx, feederID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.GetActiveEnvelopeConfigResponse{EnvelopeConfig: protomap.EnvelopeConfig(config)}), nil
}

// ListEnvelopeConfigs returns a page of a feeder's config versions.
func (c *EnvelopeConfigs) ListEnvelopeConfigs(ctx context.Context, req *connect.Request[doelabv1.ListEnvelopeConfigsRequest]) (*connect.Response[doelabv1.ListEnvelopeConfigsResponse], error) {
	feederID, err := parseID("feeder_id", req.Msg.GetFeederId())
	if err != nil {
		return nil, err
	}
	configs, next, err := c.svc.List(ctx, feederID, page(req.Msg))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.ListEnvelopeConfigsResponse{
		EnvelopeConfigs: protomap.Slice(configs, protomap.EnvelopeConfig), NextPageToken: next,
	}), nil
}

// CreateEnvelopeConfig creates the next config version of a feeder.
func (c *EnvelopeConfigs) CreateEnvelopeConfig(ctx context.Context, req *connect.Request[doelabv1.CreateEnvelopeConfigRequest]) (*connect.Response[doelabv1.CreateEnvelopeConfigResponse], error) {
	config, err := c.svc.Create(ctx, protomap.EnvelopeConfigFromProto(req.Msg.GetEnvelopeConfig()))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.CreateEnvelopeConfigResponse{EnvelopeConfig: protomap.EnvelopeConfig(config)}), nil
}
