package dersim

import (
	"context"
	"time"

	"connectrpc.com/connect"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/gen/doelab/v1/doelabv1connect"
)

// API is the part of the API that the devices use.
type API struct {
	Feeders   doelabv1connect.FeederServiceClient
	Sites     doelabv1connect.SiteServiceClient
	Devices   doelabv1connect.DeviceServiceClient
	Configs   doelabv1connect.EnvelopeConfigServiceClient
	Envelopes doelabv1connect.EnvelopeServiceClient
	Telemetry doelabv1connect.TelemetryServiceClient
	Clock     doelabv1connect.ClockServiceClient
}

// NewAPI builds the clients. They carry no token: a device's token is its
// own, and the fleet sets it call by call.
func NewAPI(client connect.HTTPClient, baseURL string, opts ...connect.ClientOption) *API {
	return &API{
		Feeders:   doelabv1connect.NewFeederServiceClient(client, baseURL, opts...),
		Sites:     doelabv1connect.NewSiteServiceClient(client, baseURL, opts...),
		Devices:   doelabv1connect.NewDeviceServiceClient(client, baseURL, opts...),
		Configs:   doelabv1connect.NewEnvelopeConfigServiceClient(client, baseURL, opts...),
		Envelopes: doelabv1connect.NewEnvelopeServiceClient(client, baseURL, opts...),
		Telemetry: doelabv1connect.NewTelemetryServiceClient(client, baseURL, opts...),
		Clock:     doelabv1connect.NewClockServiceClient(client, baseURL, opts...),
	}
}

// ClockSettings asks the API how feeder time runs: the instant at which it
// equals wall-clock time, and its speed from there.
func (a *API) ClockSettings(ctx context.Context) (anchor time.Time, speed float64, err error) {
	res, err := a.Clock.GetClock(ctx, connect.NewRequest(&doelabv1.GetClockRequest{}))
	if err != nil {
		return time.Time{}, 0, err
	}
	return res.Msg.GetAnchor().AsTime(), res.Msg.GetSpeed(), nil
}

// pageSize is how many rows a list call asks for.
const pageSize = 500
