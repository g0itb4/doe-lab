package controller

import (
	"context"

	"connectrpc.com/connect"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/gen/doelab/v1/doelabv1connect"
	"doelab/api/internal/protomap"
	"doelab/api/internal/service"
)

// EnvelopeRuns serves doelab.v1.EnvelopeRunService.
type EnvelopeRuns struct {
	svc *service.EnvelopeRuns
}

// NewEnvelopeRuns builds the handler.
func NewEnvelopeRuns(svc *service.EnvelopeRuns) *EnvelopeRuns {
	return &EnvelopeRuns{svc: svc}
}

var _ doelabv1connect.EnvelopeRunServiceHandler = (*EnvelopeRuns)(nil)

// GetEnvelopeRun returns a run by id.
func (c *EnvelopeRuns) GetEnvelopeRun(ctx context.Context, req *connect.Request[doelabv1.GetEnvelopeRunRequest]) (*connect.Response[doelabv1.GetEnvelopeRunResponse], error) {
	id, err := parseID("id", req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	run, err := c.svc.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.GetEnvelopeRunResponse{EnvelopeRun: protomap.EnvelopeRun(run)}), nil
}

// ListEnvelopeRuns returns a page of a feeder's runs, newest first.
func (c *EnvelopeRuns) ListEnvelopeRuns(ctx context.Context, req *connect.Request[doelabv1.ListEnvelopeRunsRequest]) (*connect.Response[doelabv1.ListEnvelopeRunsResponse], error) {
	feederID, err := parseID("feeder_id", req.Msg.GetFeederId())
	if err != nil {
		return nil, err
	}
	runs, next, err := c.svc.List(ctx, feederID, optionalStatus(req.Msg.Status), page(req.Msg))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.ListEnvelopeRunsResponse{
		EnvelopeRuns: protomap.Slice(runs, protomap.EnvelopeRun), NextPageToken: next,
	}), nil
}

// CreateEnvelopeRun starts a run.
func (c *EnvelopeRuns) CreateEnvelopeRun(ctx context.Context, req *connect.Request[doelabv1.CreateEnvelopeRunRequest]) (*connect.Response[doelabv1.CreateEnvelopeRunResponse], error) {
	run, err := c.svc.Create(ctx, protomap.EnvelopeRunFromProto(req.Msg.GetEnvelopeRun()))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.CreateEnvelopeRunResponse{EnvelopeRun: protomap.EnvelopeRun(run)}), nil
}

// CompleteEnvelopeRun finishes a run.
func (c *EnvelopeRuns) CompleteEnvelopeRun(ctx context.Context, req *connect.Request[doelabv1.CompleteEnvelopeRunRequest]) (*connect.Response[doelabv1.CompleteEnvelopeRunResponse], error) {
	id, err := parseID("id", req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	run, err := c.svc.Complete(ctx, id, service.RunResult{
		Status:     protomap.RunStatusFromProto(req.Msg.GetStatus()),
		DurationMS: req.Msg.GetDurationMs(), SiteCount: req.Msg.GetSiteCount(), IntervalCount: req.Msg.GetIntervalCount(),
		Error: req.Msg.Error,
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.CompleteEnvelopeRunResponse{EnvelopeRun: protomap.EnvelopeRun(run)}), nil
}

// CreateEnvelopeRunIntervals records the feeder's forecast state for
// intervals of a run.
func (c *EnvelopeRuns) CreateEnvelopeRunIntervals(ctx context.Context, req *connect.Request[doelabv1.CreateEnvelopeRunIntervalsRequest]) (*connect.Response[doelabv1.CreateEnvelopeRunIntervalsResponse], error) {
	runID, err := parseID("envelope_run_id", req.Msg.GetEnvelopeRunId())
	if err != nil {
		return nil, err
	}
	created, err := c.svc.CreateIntervals(ctx, runID, protomap.Slice(req.Msg.GetIntervals(), protomap.EnvelopeRunIntervalFromProto))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.CreateEnvelopeRunIntervalsResponse{Created: int32(created)}), nil //nolint:gosec // G115: at most 288
}

// ListEnvelopeRunIntervals returns the intervals of a run.
func (c *EnvelopeRuns) ListEnvelopeRunIntervals(ctx context.Context, req *connect.Request[doelabv1.ListEnvelopeRunIntervalsRequest]) (*connect.Response[doelabv1.ListEnvelopeRunIntervalsResponse], error) {
	runID, err := parseID("envelope_run_id", req.Msg.GetEnvelopeRunId())
	if err != nil {
		return nil, err
	}
	intervals, err := c.svc.ListIntervals(ctx, runID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.ListEnvelopeRunIntervalsResponse{
		Intervals: protomap.Slice(intervals, protomap.EnvelopeRunInterval),
	}), nil
}
