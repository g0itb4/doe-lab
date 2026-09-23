package controller

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/gen/doelab/v1/doelabv1connect"
	"doelab/api/internal/domain"
	"doelab/api/internal/protomap"
	"doelab/api/internal/service"
)

// Envelopes serves doelab.v1.EnvelopeService.
type Envelopes struct {
	svc *service.Envelopes
}

// NewEnvelopes builds the handler.
func NewEnvelopes(svc *service.Envelopes) *Envelopes {
	return &Envelopes{svc: svc}
}

var _ doelabv1connect.EnvelopeServiceHandler = (*Envelopes)(nil)

// GetCurrentEnvelope returns the envelope in force for a site.
func (c *Envelopes) GetCurrentEnvelope(ctx context.Context, req *connect.Request[doelabv1.GetCurrentEnvelopeRequest]) (*connect.Response[doelabv1.GetCurrentEnvelopeResponse], error) {
	var at time.Time
	if req.Msg.At != nil {
		at = req.Msg.GetAt().AsTime()
	}
	var envelope *domain.Envelope
	var err error
	if nmi := req.Msg.GetNmi(); nmi != "" {
		envelope, err = c.svc.CurrentByNMI(ctx, nmi, at)
	} else {
		siteID, parseErr := parseID("site_id", req.Msg.GetSiteId())
		if parseErr != nil {
			return nil, parseErr
		}
		envelope, err = c.svc.Current(ctx, siteID, at)
	}
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.GetCurrentEnvelopeResponse{Envelope: protomap.OptionalEnvelope(envelope)}), nil
}

// ListEnvelopes returns a page of a site's envelopes.
func (c *Envelopes) ListEnvelopes(ctx context.Context, req *connect.Request[doelabv1.ListEnvelopesRequest]) (*connect.Response[doelabv1.ListEnvelopesResponse], error) {
	siteID, err := parseID("site_id", req.Msg.GetSiteId())
	if err != nil {
		return nil, err
	}
	envelopes, next, err := c.svc.List(ctx, siteID, req.Msg.GetFrom().AsTime(), req.Msg.GetTo().AsTime(),
		req.Msg.GetIncludeSuperseded(), page(req.Msg))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.ListEnvelopesResponse{
		Envelopes: protomap.Slice(envelopes, protomap.Envelope), NextPageToken: next,
	}), nil
}

// PublishEnvelopes stores a batch of envelopes from a run.
func (c *Envelopes) PublishEnvelopes(ctx context.Context, req *connect.Request[doelabv1.PublishEnvelopesRequest]) (*connect.Response[doelabv1.PublishEnvelopesResponse], error) {
	runID, err := parseID("envelope_run_id", req.Msg.GetEnvelopeRunId())
	if err != nil {
		return nil, err
	}
	result, err := c.svc.Publish(ctx, runID, req.Msg.GetIdempotencyKey(),
		protomap.Slice(req.Msg.GetEnvelopes(), protomap.EnvelopeFromProto))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.PublishEnvelopesResponse{
		Published: int32(result.Published), Superseded: int32(result.Superseded), Replayed: result.Replayed, //nolint:gosec // G115: a batch is at most 5000
	}), nil
}

// SubscribeEnvelopes streams the envelope of one site to its device. The
// stream lives until the client leaves: the service decides WHEN there is
// something to say, and this handler says it.
func (c *Envelopes) SubscribeEnvelopes(ctx context.Context, req *connect.Request[doelabv1.SubscribeEnvelopesRequest], stream *connect.ServerStream[doelabv1.SubscribeEnvelopesResponse]) error {
	return c.svc.Follow(ctx, req.Msg.GetNmi(), func(envelope *domain.Envelope, now time.Time, keepalive bool) error {
		return stream.Send(&doelabv1.SubscribeEnvelopesResponse{
			Envelope: protomap.OptionalEnvelope(envelope), ServerTime: timestamppb.New(now), Keepalive: keepalive,
		})
	})
}
