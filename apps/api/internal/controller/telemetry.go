package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/gen/doelab/v1/doelabv1connect"
	"doelab/api/internal/protomap"
	"doelab/api/internal/service"
)

// Telemetry serves doelab.v1.TelemetryService.
type Telemetry struct {
	svc *service.Telemetry
}

// NewTelemetry builds the handler.
func NewTelemetry(svc *service.Telemetry) *Telemetry {
	return &Telemetry{svc: svc}
}

var _ doelabv1connect.TelemetryServiceHandler = (*Telemetry)(nil)

// ListReadings returns a page of a device's readings.
func (c *Telemetry) ListReadings(ctx context.Context, req *connect.Request[doelabv1.ListReadingsRequest]) (*connect.Response[doelabv1.ListReadingsResponse], error) {
	deviceID, err := parseID("device_id", req.Msg.GetDeviceId())
	if err != nil {
		return nil, err
	}
	readings, next, err := c.svc.ListReadings(ctx, deviceID, req.Msg.GetFrom().AsTime(), req.Msg.GetTo().AsTime(), page(req.Msg))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.ListReadingsResponse{
		Readings: protomap.Slice(readings, protomap.Reading), NextPageToken: next,
	}), nil
}

// IngestReadings takes a stream of batches from the devices of one site, and
// answers when the client closes it, or when the stream's context ends: the
// server is shutting down. Either way the answer counts what was stored.
func (c *Telemetry) IngestReadings(ctx context.Context, stream *connect.ClientStream[doelabv1.IngestReadingsRequest]) (*connect.Response[doelabv1.IngestReadingsResponse], error) {
	// Receive blocks until the device sends, and a device may be silent for a
	// long time. The wait is in a goroutine, so that the handler can still
	// leave when its context ends; the goroutine leaves when the request is
	// closed behind the handler.
	batches := make(chan *doelabv1.IngestReadingsRequest)
	go func() {
		defer close(batches)
		for stream.Receive() {
			select {
			case batches <- stream.Msg():
			case <-ctx.Done():
				return
			}
		}
	}()

	var out doelabv1.IngestReadingsResponse
	nmi := ""
	for {
		var msg *doelabv1.IngestReadingsRequest
		select {
		case <-ctx.Done():
			return connect.NewResponse(&out), nil
		case received, open := <-batches:
			if !open {
				// The goroutine is done with the stream, so its error is
				// safe to read.
				if err := stream.Err(); err != nil && !errors.Is(err, io.EOF) && ctx.Err() == nil {
					return nil, err
				}
				return connect.NewResponse(&out), nil
			}
			msg = received
		}
		// A stream is for one site: the token is checked against the NMI of
		// every batch, and the NMI may not change on the way.
		if nmi == "" {
			nmi = msg.GetNmi()
		} else if msg.GetNmi() != nmi {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("the stream is for site %s; a batch names %s", nmi, msg.GetNmi()))
		}
		stored, err := c.svc.Ingest(ctx, nmi, protomap.Slice(msg.GetReadings(), protomap.ReadingFromProto))
		if err != nil {
			return nil, err
		}
		out.Accepted += int32(stored)                            //nolint:gosec // G115: a batch is at most 1000
		out.Duplicates += int32(len(msg.GetReadings()) - stored) //nolint:gosec // G115: as above
	}
}

func fleetSummary(s service.FleetSummary) *doelabv1.FleetSummary {
	out := &doelabv1.FleetSummary{
		FeederId: s.FeederID.String(), At: timestamppb.New(s.At),
		EnrolledSites: int32(s.EnrolledSites), ReportingSites: int32(s.ReportingSites), //nolint:gosec // G115: counts of a feeder's sites
		Devices: int32(s.Devices), DevicesOnline: int32(s.DevicesOnline), //nolint:gosec // G115: as above
		ExportW: s.ExportW, ImportW: s.ImportW, ExportLimitW: s.ExportLimitW, ControlledExportW: s.ControlledExportW,
		SitesOverLimit: int32(s.SitesOverLimit), OpenAlerts: int32(s.OpenAlerts), //nolint:gosec // G115: as above
	}
	if s.BackstopEventID != nil {
		id := s.BackstopEventID.String()
		out.BackstopEventId = &id
	}
	if s.LatestRun != nil {
		id, status := s.LatestRun.ID.String(), protomap.RunStatusToProto(s.LatestRun.Status)
		out.LatestRunId, out.LatestRunAt, out.LatestRunStatus = &id, timestamppb.New(s.LatestRun.StartedAt), &status
	}
	return out
}

// GetFleetSummary returns the fleet of a feeder now.
func (c *Telemetry) GetFleetSummary(ctx context.Context, req *connect.Request[doelabv1.GetFleetSummaryRequest]) (*connect.Response[doelabv1.GetFleetSummaryResponse], error) {
	feederID, err := parseID("feeder_id", req.Msg.GetFeederId())
	if err != nil {
		return nil, err
	}
	summary, err := c.svc.Summary(ctx, feederID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.GetFleetSummaryResponse{Summary: fleetSummary(summary)}), nil
}

// GetFleetState returns every feeder now, and each site that has a location.
func (c *Telemetry) GetFleetState(ctx context.Context, _ *connect.Request[doelabv1.GetFleetStateRequest]) (*connect.Response[doelabv1.GetFleetStateResponse], error) {
	state, err := c.svc.FleetState(ctx)
	if err != nil {
		return nil, err
	}
	out := &doelabv1.GetFleetStateResponse{
		At:      timestamppb.New(state.At),
		Feeders: protomap.Slice(state.Feeders, fleetSummary),
		Sites:   make([]*doelabv1.SiteState, len(state.Sites)),
	}
	for i, site := range state.Sites {
		message := &doelabv1.SiteState{
			SiteId: site.SiteID.String(), Envelope: protomap.OptionalEnvelope(site.Envelope),
			Reporting: site.Reporting, OverLimit: site.OverLimit,
		}
		if site.Reporting {
			message.NetExportW = &site.NetExportW
		}
		if site.OpenAlert != nil {
			message.OpenAlert = protomap.Alert(*site.OpenAlert)
		}
		out.Sites[i] = message
	}
	return connect.NewResponse(out), nil
}

// GetFeederState returns one feeder at an instant, bus by bus and line by
// line.
func (c *Telemetry) GetFeederState(ctx context.Context, req *connect.Request[doelabv1.GetFeederStateRequest]) (*connect.Response[doelabv1.GetFeederStateResponse], error) {
	feederID, err := parseID("feeder_id", req.Msg.GetFeederId())
	if err != nil {
		return nil, err
	}
	var at *time.Time
	if req.Msg.At != nil {
		t := req.Msg.GetAt().AsTime()
		at = &t
	}
	state, err := c.svc.FeederState(ctx, feederID, at)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.GetFeederStateResponse{
		At:     timestamppb.New(state.At),
		Nodes:  protomap.Slice(state.Nodes, protomap.FeederNodeState),
		Lines:  protomap.Slice(state.Lines, protomap.FeederLineState),
		VMinPu: state.VMinPU, VMaxPu: state.VMaxPU,
		LineLimitPct: state.LineLimitPct, TransformerLimitPct: state.TransformerLimitPct,
	}), nil
}

// WatchFleet streams the fleet summary until the client leaves.
func (c *Telemetry) WatchFleet(ctx context.Context, req *connect.Request[doelabv1.WatchFleetRequest], stream *connect.ServerStream[doelabv1.WatchFleetResponse]) error {
	feederID, err := parseID("feeder_id", req.Msg.GetFeederId())
	if err != nil {
		return err
	}
	return c.svc.Watch(ctx, feederID, func(summary service.FleetSummary) error {
		return stream.Send(&doelabv1.WatchFleetResponse{Summary: fleetSummary(summary)})
	})
}

// GetFeederSeries returns a feeder through time.
func (c *Telemetry) GetFeederSeries(ctx context.Context, req *connect.Request[doelabv1.GetFeederSeriesRequest]) (*connect.Response[doelabv1.GetFeederSeriesResponse], error) {
	feederID, err := parseID("feeder_id", req.Msg.GetFeederId())
	if err != nil {
		return nil, err
	}
	series, err := c.svc.Series(ctx, feederID, req.Msg.GetFrom().AsTime(), req.Msg.GetTo().AsTime())
	if err != nil {
		return nil, err
	}
	out := &doelabv1.GetFeederSeriesResponse{
		VMinPu: series.VMinPU, VMaxPu: series.VMaxPU, TransformerKva: series.TransformerKVA, StaticLimitW: series.StaticLimitW,
		Points: make([]*doelabv1.FeederPoint, len(series.Points)),
	}
	for i, p := range series.Points {
		out.Points[i] = &doelabv1.FeederPoint{
			ValidFrom: timestamppb.New(p.ValidFrom), ValidTo: timestamppb.New(p.ValidTo),
			ForecastNetLoadW: p.ForecastNetLoadW, ForecastLoadingPct: p.ForecastLoadingPct,
			ForecastVMinPu: p.ForecastVMinPU, ForecastVMaxPu: p.ForecastVMaxPU,
			ExportLimitTotalW: p.ExportLimitTotalW, ImportLimitTotalW: p.ImportLimitTotalW,
			StaticLimitTotalW: p.StaticLimitTotalW, StaticVMaxPu: p.StaticVMaxPU,
			StaticBinding: protomap.BindingConstraintToProto(p.StaticBinding), StaticBindingElement: p.StaticBindingElement,
			MeasuredExportW: p.MeasuredExportW, MeasuredImportW: p.MeasuredImportW,
			EnvelopeVMaxPu: p.EnvelopeVMaxPU,
		}
	}
	return connect.NewResponse(out), nil
}

// GetSiteSeries returns one site through time.
func (c *Telemetry) GetSiteSeries(ctx context.Context, req *connect.Request[doelabv1.GetSiteSeriesRequest]) (*connect.Response[doelabv1.GetSiteSeriesResponse], error) {
	siteID, err := parseID("site_id", req.Msg.GetSiteId())
	if err != nil {
		return nil, err
	}
	series, err := c.svc.SiteSeries(ctx, siteID, req.Msg.GetFrom().AsTime(), req.Msg.GetTo().AsTime())
	if err != nil {
		return nil, err
	}
	out := &doelabv1.GetSiteSeriesResponse{
		Power:    make([]*doelabv1.SitePowerPoint, len(series.Power)),
		Forecast: make([]*doelabv1.SiteForecastPoint, len(series.Forecast)),
	}
	for i, p := range series.Power {
		out.Power[i] = &doelabv1.SitePowerPoint{
			Bucket: timestamppb.New(p.Bucket), AvgNetExportW: p.AvgNetExportW, MaxNetExportW: p.MaxNetExportW,
			AvgSocPct: p.AvgSOCPct, AvgVoltageV: p.AvgVoltageV,
		}
	}
	for i, f := range series.Forecast {
		out.Forecast[i] = &doelabv1.SiteForecastPoint{Ts: timestamppb.New(f.TS), LoadW: f.LoadW, PvW: f.PVW}
	}
	return connect.NewResponse(out), nil
}

// GetDailyReport returns a day of a feeder in numbers.
func (c *Telemetry) GetDailyReport(ctx context.Context, req *connect.Request[doelabv1.GetDailyReportRequest]) (*connect.Response[doelabv1.GetDailyReportResponse], error) {
	feederID, err := parseID("feeder_id", req.Msg.GetFeederId())
	if err != nil {
		return nil, err
	}
	r, err := c.svc.Report(ctx, feederID, req.Msg.GetDay().AsTime())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.GetDailyReportResponse{
		From: timestamppb.New(r.From), To: timestamppb.New(r.To),
		EnrolledSites: int32(r.EnrolledSites), Intervals: int32(r.Intervals), //nolint:gosec // G115: counts of a day
		PotentialExportKwh: r.PotentialExportKWh,
		EnvelopeExportKwh:  r.EnvelopeExportKWh, EnvelopeCurtailedKwh: r.EnvelopeCurtailedKWh,
		StaticExportKwh: r.StaticExportKWh, StaticCurtailedKwh: r.StaticCurtailedKWh,
		StaticViolationIntervals: int32(r.StaticViolationIntervals), StaticLimitW: r.StaticLimitW, //nolint:gosec // G115: as above
		ConstraintBreaches: int32(r.ConstraintBreaches), DeviceOfflineAlerts: int32(r.DeviceOfflineAlerts), //nolint:gosec // G115: as above
		BackstopEvents: int32(r.BackstopEvents), //nolint:gosec // G115: as above
	}), nil
}
