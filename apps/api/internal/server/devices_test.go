package server_test

import (
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/internal/repo/repotest"
)

func TestDeviceCRUD(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	siteA, siteB := a.fixture.SiteA.ID.String(), a.fixture.SiteB.ID.String()
	create := func(token, siteID string, derType doelabv1.DerType, ratedW float64) (*doelabv1.Device, error) {
		res, err := a.devices(token).CreateDevice(ctx, req(&doelabv1.CreateDeviceRequest{
			Device: &doelabv1.Device{SiteId: siteID, DerType: derType, RatedW: ratedW},
		}))
		if err != nil {
			return nil, err
		}
		return res.Msg.GetDevice(), nil
	}

	// Create.
	solar, err := create(operatorToken, siteA, doelabv1.DerType_DER_TYPE_SOLAR, 5000)
	noErr(t, "create solar", err)
	battery, err := create(operatorToken, siteA, doelabv1.DerType_DER_TYPE_BATTERY, 5000)
	noErr(t, "create battery", err)
	_, err = create(operatorToken, siteB, doelabv1.DerType_DER_TYPE_EV, 7000)
	noErr(t, "create EV", err)
	if solar.GetId() == "" || solar.GetCreatedAt() == nil || solar.GetDerType() != doelabv1.DerType_DER_TYPE_SOLAR || solar.GetSiteId() != siteA {
		t.Errorf("solar = %v", solar)
	}
	_, err = create(operatorToken, siteA, doelabv1.DerType_DER_TYPE_SOLAR, 3000)
	wantCode(t, "a second solar device on a site", err, connect.CodeAlreadyExists)
	_, err = create(operatorToken, unknownID, doelabv1.DerType_DER_TYPE_SOLAR, 3000)
	wantCode(t, "an unknown site", err, connect.CodeFailedPrecondition)
	_, err = create(operatorToken, siteB, doelabv1.DerType_DER_TYPE_UNSPECIFIED, 3000)
	wantViolation(t, "no type", err, "required")
	_, err = create(operatorToken, siteB, doelabv1.DerType(99), 3000)
	wantViolation(t, "an undefined type", err, "der_type")
	_, err = create(operatorToken, siteB, doelabv1.DerType_DER_TYPE_SOLAR, 0)
	wantViolation(t, "no rating", err, "required")
	_, err = create(operatorToken, "", doelabv1.DerType_DER_TYPE_SOLAR, 1)
	wantViolation(t, "no site", err, "required")
	_, err = a.devices(operatorToken).CreateDevice(ctx, req(&doelabv1.CreateDeviceRequest{
		Device: &doelabv1.Device{Id: unknownID, SiteId: siteB, DerType: doelabv1.DerType_DER_TYPE_SOLAR, RatedW: 1},
	}))
	wantViolation(t, "a client-chosen id", err, "id is set by the server")
	_, err = create("", siteB, doelabv1.DerType_DER_TYPE_SOLAR, 3000)
	wantCode(t, "anonymous", err, connect.CodeUnauthenticated)

	// Get.
	got, err := a.devices("").GetDevice(ctx, req(&doelabv1.GetDeviceRequest{Id: battery.GetId()}))
	noErr(t, "get", err)
	if got.Msg.GetDevice().GetDerType() != doelabv1.DerType_DER_TYPE_BATTERY {
		t.Errorf("get = %v", got.Msg.GetDevice())
	}
	_, err = a.devices("").GetDevice(ctx, req(&doelabv1.GetDeviceRequest{Id: unknownID}))
	wantCode(t, "get an unknown device", err, connect.CodeNotFound)
	_, err = a.devices("").GetDevice(ctx, req(&doelabv1.GetDeviceRequest{}))
	wantViolation(t, "get with no id", err, "id")

	// List: two pages, then each filter.
	list := func(r *doelabv1.ListDevicesRequest) *doelabv1.ListDevicesResponse {
		res, err := a.devices("").ListDevices(ctx, req(r))
		noErr(t, "list", err)
		return res.Msg
	}
	page1 := list(&doelabv1.ListDevicesRequest{PageSize: 2})
	page2 := list(&doelabv1.ListDevicesRequest{PageSize: 2, PageToken: page1.GetNextPageToken()})
	if len(page1.GetDevices()) != 2 || len(page2.GetDevices()) != 1 || page2.GetNextPageToken() != "" {
		t.Errorf("pages = %d and %d devices", len(page1.GetDevices()), len(page2.GetDevices()))
	}
	if n := len(list(&doelabv1.ListDevicesRequest{SiteId: &siteA}).GetDevices()); n != 2 {
		t.Errorf("site filter = %d devices, want 2", n)
	}
	if n := len(list(&doelabv1.ListDevicesRequest{DerType: repotest.Ptr(doelabv1.DerType_DER_TYPE_EV)}).GetDevices()); n != 1 {
		t.Errorf("type filter = %d devices, want 1", n)
	}
	if n := len(list(&doelabv1.ListDevicesRequest{SiteId: &siteB, DerType: repotest.Ptr(doelabv1.DerType_DER_TYPE_SOLAR)}).GetDevices()); n != 0 {
		t.Errorf("both filters = %d devices, want 0", n)
	}
	_, err = a.devices("").ListDevices(ctx, req(&doelabv1.ListDevicesRequest{DerType: repotest.Ptr(doelabv1.DerType_DER_TYPE_UNSPECIFIED)}))
	wantViolation(t, "filter on the unspecified type", err, "der_type")
	_, err = a.devices("").ListDevices(ctx, req(&doelabv1.ListDevicesRequest{SiteId: repotest.Ptr("x")}))
	wantViolation(t, "malformed site filter", err, "site_id")

	// Update.
	update := func(token string, d *doelabv1.Device, paths ...string) (*doelabv1.Device, error) {
		res, err := a.devices(token).UpdateDevice(ctx, req(&doelabv1.UpdateDeviceRequest{
			Device: d, UpdateMask: &fieldmaskpb.FieldMask{Paths: paths},
		}))
		if err != nil {
			return nil, err
		}
		return res.Msg.GetDevice(), nil
	}
	updated, err := update(operatorToken, &doelabv1.Device{Id: solar.GetId(), RatedW: 6600, DerType: doelabv1.DerType_DER_TYPE_EV}, "rated_w")
	noErr(t, "update", err)
	if updated.GetRatedW() != 6600 || updated.GetDerType() != doelabv1.DerType_DER_TYPE_SOLAR {
		t.Errorf("updated = %v", updated)
	}
	_, err = update(operatorToken, &doelabv1.Device{Id: solar.GetId(), DerType: doelabv1.DerType_DER_TYPE_EV, RatedW: 1}, "der_type")
	wantViolation(t, "an immutable field in the mask", err, "update_mask")
	_, err = update(operatorToken, &doelabv1.Device{Id: solar.GetId(), RatedW: 0}, "rated_w")
	wantViolation(t, "a zero rating", err, "rated_w")
	_, err = update(operatorToken, &doelabv1.Device{RatedW: 1}, "rated_w")
	wantViolation(t, "no id", err, "device.id")
	_, err = update(operatorToken, &doelabv1.Device{Id: unknownID, RatedW: 1}, "rated_w")
	wantCode(t, "an unknown device", err, connect.CodeNotFound)
	_, err = update("", &doelabv1.Device{Id: solar.GetId(), RatedW: 1}, "rated_w")
	wantCode(t, "anonymous", err, connect.CodeUnauthenticated)

	// Delete, and the place it frees.
	_, err = a.devices("").DeleteDevice(ctx, req(&doelabv1.DeleteDeviceRequest{Id: solar.GetId()}))
	wantCode(t, "anonymous delete", err, connect.CodeUnauthenticated)
	_, err = a.devices(operatorToken).DeleteDevice(ctx, req(&doelabv1.DeleteDeviceRequest{Id: solar.GetId()}))
	noErr(t, "delete", err)
	_, err = a.devices("").GetDevice(ctx, req(&doelabv1.GetDeviceRequest{Id: solar.GetId()}))
	wantCode(t, "get a deleted device", err, connect.CodeNotFound)
	_, err = a.devices(operatorToken).DeleteDevice(ctx, req(&doelabv1.DeleteDeviceRequest{Id: solar.GetId()}))
	wantCode(t, "delete it again", err, connect.CodeNotFound)
	_, err = a.devices(operatorToken).DeleteDevice(ctx, req(&doelabv1.DeleteDeviceRequest{Id: "x"}))
	wantViolation(t, "malformed id", err, "id")
	_, err = create(operatorToken, siteA, doelabv1.DerType_DER_TYPE_SOLAR, 8000)
	noErr(t, "replace the deleted device", err)
}
