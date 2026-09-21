package controller

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	doelabv1 "doelab/api/gen/doelab/v1"
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

	calls := map[string]func() error{
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
