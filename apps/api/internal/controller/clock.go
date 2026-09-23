package controller

import (
	"context"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/gen/doelab/v1/doelabv1connect"
	"doelab/api/internal/simclock"
)

// Clock serves doelab.v1.ClockService. It has no service behind it: the
// clock is the whole of the answer.
type Clock struct {
	clock *simclock.Clock
}

// NewClock builds the handler.
func NewClock(clock *simclock.Clock) *Clock {
	return &Clock{clock: clock}
}

var _ doelabv1connect.ClockServiceHandler = (*Clock)(nil)

// GetClock returns feeder time, and how it relates to the wall clock.
func (c *Clock) GetClock(context.Context, *connect.Request[doelabv1.GetClockRequest]) (*connect.Response[doelabv1.GetClockResponse], error) {
	now := c.clock.Now()
	return connect.NewResponse(&doelabv1.GetClockResponse{
		Now: timestamppb.New(now), Anchor: timestamppb.New(c.clock.Anchor()),
		Speed: c.clock.Speed(), WallNow: timestamppb.New(c.clock.Wall(now)),
	}), nil
}
