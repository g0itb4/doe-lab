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

// Substations serves doelab.v1.SubstationService.
type Substations struct {
	svc *service.Substations
}

// NewSubstations builds the handler.
func NewSubstations(svc *service.Substations) *Substations {
	return &Substations{svc: svc}
}

var _ doelabv1connect.SubstationServiceHandler = (*Substations)(nil)

// GetSubstation returns a substation by id or by code.
func (c *Substations) GetSubstation(ctx context.Context, req *connect.Request[doelabv1.GetSubstationRequest]) (*connect.Response[doelabv1.GetSubstationResponse], error) {
	var substation domain.Substation
	var err error
	if code := req.Msg.GetCode(); code != "" {
		substation, err = c.svc.GetByCode(ctx, code)
	} else {
		id, parseErr := parseID("id", req.Msg.GetId())
		if parseErr != nil {
			return nil, parseErr
		}
		substation, err = c.svc.Get(ctx, id)
	}
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.GetSubstationResponse{Substation: protomap.Substation(substation)}), nil
}

// ListSubstations returns a page of substations.
func (c *Substations) ListSubstations(ctx context.Context, req *connect.Request[doelabv1.ListSubstationsRequest]) (*connect.Response[doelabv1.ListSubstationsResponse], error) {
	substations, next, err := c.svc.List(ctx, page(req.Msg))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.ListSubstationsResponse{
		Substations: protomap.Slice(substations, protomap.Substation), NextPageToken: next,
	}), nil
}
