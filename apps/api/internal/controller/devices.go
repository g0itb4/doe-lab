package controller

import (
	"context"

	"connectrpc.com/connect"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/gen/doelab/v1/doelabv1connect"
	"doelab/api/internal/protomap"
	"doelab/api/internal/service"
)

// Devices serves doelab.v1.DeviceService.
type Devices struct {
	svc *service.Devices
}

// NewDevices builds the handler.
func NewDevices(svc *service.Devices) *Devices {
	return &Devices{svc: svc}
}

var _ doelabv1connect.DeviceServiceHandler = (*Devices)(nil)

// GetDevice returns a device by id.
func (c *Devices) GetDevice(ctx context.Context, req *connect.Request[doelabv1.GetDeviceRequest]) (*connect.Response[doelabv1.GetDeviceResponse], error) {
	id, err := parseID("id", req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	device, err := c.svc.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.GetDeviceResponse{Device: protomap.Device(device)}), nil
}

// ListDevices returns a page of devices.
func (c *Devices) ListDevices(ctx context.Context, req *connect.Request[doelabv1.ListDevicesRequest]) (*connect.Response[doelabv1.ListDevicesResponse], error) {
	var filter service.DeviceFilter
	if req.Msg.SiteId != nil {
		siteID, err := parseID("site_id", req.Msg.GetSiteId())
		if err != nil {
			return nil, err
		}
		filter.SiteID = &siteID
	}
	if req.Msg.DerType != nil {
		derType := protomap.DERTypeFromProto(req.Msg.GetDerType())
		filter.DERType = &derType
	}
	devices, next, err := c.svc.List(ctx, filter, page(req.Msg))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.ListDevicesResponse{
		Devices: protomap.Slice(devices, protomap.Device), NextPageToken: next,
	}), nil
}

// CreateDevice creates a device.
func (c *Devices) CreateDevice(ctx context.Context, req *connect.Request[doelabv1.CreateDeviceRequest]) (*connect.Response[doelabv1.CreateDeviceResponse], error) {
	device, err := c.svc.Create(ctx, protomap.DeviceFromProto(req.Msg.GetDevice()))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.CreateDeviceResponse{Device: protomap.Device(device)}), nil
}

// UpdateDevice changes a device's rating.
func (c *Devices) UpdateDevice(ctx context.Context, req *connect.Request[doelabv1.UpdateDeviceRequest]) (*connect.Response[doelabv1.UpdateDeviceResponse], error) {
	id, err := parseID("device.id", req.Msg.GetDevice().GetId())
	if err != nil {
		return nil, err
	}
	// The mask is exactly rated_w.
	device, err := c.svc.SetRating(ctx, id, req.Msg.GetDevice().GetRatedW())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.UpdateDeviceResponse{Device: protomap.Device(device)}), nil
}

// DeleteDevice soft-deletes a device.
func (c *Devices) DeleteDevice(ctx context.Context, req *connect.Request[doelabv1.DeleteDeviceRequest]) (*connect.Response[doelabv1.DeleteDeviceResponse], error) {
	id, err := parseID("id", req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if err := c.svc.Delete(ctx, id); err != nil {
		return nil, err
	}
	return connect.NewResponse(&doelabv1.DeleteDeviceResponse{}), nil
}
