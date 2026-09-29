package controller

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/internal/repo/bus"
	"doelab/api/internal/repo/mem"
	"doelab/api/internal/service"
)

// A handler is normally reached only after the validation interceptor has
// checked every id. These tests call the handlers directly, with ids the
// interceptor would have refused: the handler must still answer
// invalid_argument and never reach the service.
func TestHandlersRefuseMalformedIDs(t *testing.T) {
	t.Parallel()
	store := mem.New()
	feeders := NewFeeders(service.NewFeeders(store))
	sites := NewSites(service.NewSites(store))
	devices := NewDevices(service.NewDevices(store))
	configs := NewEnvelopeConfigs(service.NewEnvelopeConfigs(store))
	ctx := context.Background()
	const bad = "not-a-uuid"

	runs := NewEnvelopeRuns(service.NewEnvelopeRuns(store))
	envelopes := NewEnvelopes(service.NewEnvelopes(store, bus.NewLocal(), nil))

	compliance := service.NewCompliance(store, nil)
	telemetry := NewTelemetry(service.NewTelemetry(store, nil, compliance))
	alerts := NewAlerts(service.NewAlerts(store))
	backstops := NewBackstops(service.NewBackstops(store, bus.NewLocal(), nil))
	const good = "0199a0a0-0000-7000-8000-000000000001"

	calls := map[string]func() error{
		"CreateEnvelopeRunIntervals": func() error {
			_, err := runs.CreateEnvelopeRunIntervals(ctx, connect.NewRequest(&doelabv1.CreateEnvelopeRunIntervalsRequest{EnvelopeRunId: bad}))
			return err
		},
		"ListEnvelopeRunIntervals": func() error {
			_, err := runs.ListEnvelopeRunIntervals(ctx, connect.NewRequest(&doelabv1.ListEnvelopeRunIntervalsRequest{EnvelopeRunId: bad}))
			return err
		},
		"ListReadings": func() error {
			_, err := telemetry.ListReadings(ctx, connect.NewRequest(&doelabv1.ListReadingsRequest{DeviceId: bad}))
			return err
		},
		"GetFleetSummary": func() error {
			_, err := telemetry.GetFleetSummary(ctx, connect.NewRequest(&doelabv1.GetFleetSummaryRequest{FeederId: bad}))
			return err
		},
		"WatchFleet": func() error {
			return telemetry.WatchFleet(ctx, connect.NewRequest(&doelabv1.WatchFleetRequest{FeederId: bad}), nil)
		},
		"GetFeederSeries": func() error {
			_, err := telemetry.GetFeederSeries(ctx, connect.NewRequest(&doelabv1.GetFeederSeriesRequest{FeederId: bad}))
			return err
		},
		"GetSiteSeries": func() error {
			_, err := telemetry.GetSiteSeries(ctx, connect.NewRequest(&doelabv1.GetSiteSeriesRequest{SiteId: bad}))
			return err
		},
		"GetDailyReport": func() error {
			_, err := telemetry.GetDailyReport(ctx, connect.NewRequest(&doelabv1.GetDailyReportRequest{FeederId: bad}))
			return err
		},
		"GetAlert": func() error {
			_, err := alerts.GetAlert(ctx, connect.NewRequest(&doelabv1.GetAlertRequest{Id: bad}))
			return err
		},
		"ListAlerts": func() error {
			_, err := alerts.ListAlerts(ctx, connect.NewRequest(&doelabv1.ListAlertsRequest{FeederId: bad}))
			return err
		},
		"ListAlerts of a site": func() error {
			id := bad
			_, err := alerts.ListAlerts(ctx, connect.NewRequest(&doelabv1.ListAlertsRequest{FeederId: good, SiteId: &id}))
			return err
		},
		"AcknowledgeAlert": func() error {
			_, err := alerts.AcknowledgeAlert(ctx, connect.NewRequest(&doelabv1.AcknowledgeAlertRequest{Id: bad}))
			return err
		},
		"GetBackstopEvent": func() error {
			_, err := backstops.GetBackstopEvent(ctx, connect.NewRequest(&doelabv1.GetBackstopEventRequest{Id: bad}))
			return err
		},
		"ListBackstopEvents": func() error {
			_, err := backstops.ListBackstopEvents(ctx, connect.NewRequest(&doelabv1.ListBackstopEventsRequest{FeederId: bad}))
			return err
		},
		"ClearBackstop": func() error {
			_, err := backstops.ClearBackstop(ctx, connect.NewRequest(&doelabv1.ClearBackstopRequest{Id: bad}))
			return err
		},
		"GetFeederForecast": func() error {
			_, err := feeders.GetFeederForecast(ctx, connect.NewRequest(&doelabv1.GetFeederForecastRequest{FeederId: bad}))
			return err
		},
		"GetEnvelopeRun": func() error {
			_, err := runs.GetEnvelopeRun(ctx, connect.NewRequest(&doelabv1.GetEnvelopeRunRequest{Id: bad}))
			return err
		},
		"ListEnvelopeRuns": func() error {
			_, err := runs.ListEnvelopeRuns(ctx, connect.NewRequest(&doelabv1.ListEnvelopeRunsRequest{FeederId: bad}))
			return err
		},
		"ExportEnvelopeRun": func() error {
			_, err := runs.ExportEnvelopeRun(ctx, connect.NewRequest(&doelabv1.ExportEnvelopeRunRequest{Id: bad}))
			return err
		},
		"CompleteEnvelopeRun": func() error {
			_, err := runs.CompleteEnvelopeRun(ctx, connect.NewRequest(&doelabv1.CompleteEnvelopeRunRequest{Id: bad}))
			return err
		},
		"GetCurrentEnvelope": func() error {
			_, err := envelopes.GetCurrentEnvelope(ctx, connect.NewRequest(&doelabv1.GetCurrentEnvelopeRequest{
				Site: &doelabv1.GetCurrentEnvelopeRequest_SiteId{SiteId: bad},
			}))
			return err
		},
		"ListEnvelopes": func() error {
			_, err := envelopes.ListEnvelopes(ctx, connect.NewRequest(&doelabv1.ListEnvelopesRequest{SiteId: bad}))
			return err
		},
		"PublishEnvelopes": func() error {
			_, err := envelopes.PublishEnvelopes(ctx, connect.NewRequest(&doelabv1.PublishEnvelopesRequest{EnvelopeRunId: bad}))
			return err
		},
		"GetFeeder": func() error {
			_, err := feeders.GetFeeder(ctx, connect.NewRequest(&doelabv1.GetFeederRequest{Key: &doelabv1.GetFeederRequest_Id{Id: bad}}))
			return err
		},
		"UpdateFeeder": func() error {
			_, err := feeders.UpdateFeeder(ctx, connect.NewRequest(&doelabv1.UpdateFeederRequest{Feeder: &doelabv1.Feeder{Id: bad}}))
			return err
		},
		"GetFeederNode": func() error {
			_, err := feeders.GetFeederNode(ctx, connect.NewRequest(&doelabv1.GetFeederNodeRequest{Id: bad}))
			return err
		},
		"ListFeederNodes": func() error {
			_, err := feeders.ListFeederNodes(ctx, connect.NewRequest(&doelabv1.ListFeederNodesRequest{FeederId: bad}))
			return err
		},
		"GetFeederLine": func() error {
			_, err := feeders.GetFeederLine(ctx, connect.NewRequest(&doelabv1.GetFeederLineRequest{Id: bad}))
			return err
		},
		"ListFeederLines": func() error {
			_, err := feeders.ListFeederLines(ctx, connect.NewRequest(&doelabv1.ListFeederLinesRequest{FeederId: bad}))
			return err
		},
		"UpdateFeederLine": func() error {
			_, err := feeders.UpdateFeederLine(ctx, connect.NewRequest(&doelabv1.UpdateFeederLineRequest{FeederLine: &doelabv1.FeederLine{Id: bad}}))
			return err
		},
		"GetSite": func() error {
			_, err := sites.GetSite(ctx, connect.NewRequest(&doelabv1.GetSiteRequest{Key: &doelabv1.GetSiteRequest_Id{Id: bad}}))
			return err
		},
		"ListSites": func() error {
			_, err := sites.ListSites(ctx, connect.NewRequest(&doelabv1.ListSitesRequest{FeederId: bad}))
			return err
		},
		"UpdateSite": func() error {
			_, err := sites.UpdateSite(ctx, connect.NewRequest(&doelabv1.UpdateSiteRequest{Site: &doelabv1.Site{Id: bad}}))
			return err
		},
		"DeleteSite": func() error {
			_, err := sites.DeleteSite(ctx, connect.NewRequest(&doelabv1.DeleteSiteRequest{Id: bad}))
			return err
		},
		"ListSiteProfiles": func() error {
			_, err := sites.ListSiteProfiles(ctx, connect.NewRequest(&doelabv1.ListSiteProfilesRequest{SiteId: bad}))
			return err
		},
		"GetDevice": func() error {
			_, err := devices.GetDevice(ctx, connect.NewRequest(&doelabv1.GetDeviceRequest{Id: bad}))
			return err
		},
		"ListDevices": func() error {
			id := bad
			_, err := devices.ListDevices(ctx, connect.NewRequest(&doelabv1.ListDevicesRequest{SiteId: &id}))
			return err
		},
		"UpdateDevice": func() error {
			_, err := devices.UpdateDevice(ctx, connect.NewRequest(&doelabv1.UpdateDeviceRequest{Device: &doelabv1.Device{Id: bad}}))
			return err
		},
		"DeleteDevice": func() error {
			_, err := devices.DeleteDevice(ctx, connect.NewRequest(&doelabv1.DeleteDeviceRequest{Id: bad}))
			return err
		},
		"GetEnvelopeConfig": func() error {
			_, err := configs.GetEnvelopeConfig(ctx, connect.NewRequest(&doelabv1.GetEnvelopeConfigRequest{Id: bad}))
			return err
		},
		"GetActiveEnvelopeConfig": func() error {
			_, err := configs.GetActiveEnvelopeConfig(ctx, connect.NewRequest(&doelabv1.GetActiveEnvelopeConfigRequest{FeederId: bad}))
			return err
		},
		"ListEnvelopeConfigs": func() error {
			_, err := configs.ListEnvelopeConfigs(ctx, connect.NewRequest(&doelabv1.ListEnvelopeConfigsRequest{FeederId: bad}))
			return err
		},
	}
	for name, call := range calls {
		if err := call(); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s with a malformed id: %v, want invalid_argument", name, err)
		}
	}
}

func TestMaskHelpers(t *testing.T) {
	t.Parallel()
	paths := []string{"name", "tap_pu"}
	if !masked(paths, "name") || masked(paths, "code") || masked(nil, "name") {
		t.Error("masked")
	}
	if got := when(paths, "tap_pu", 0.975); got == nil || *got != 0.975 {
		t.Errorf("when for a masked path = %v", got)
	}
	if got := when(paths, "code", "x"); got != nil {
		t.Errorf("when for an unmasked path = %v", *got)
	}
}
