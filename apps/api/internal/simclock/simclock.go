// Package simclock is feeder time: the clock of the simulation.
//
// At speed 1, feeder time is wall-clock time. A demo runs faster, so that a
// visitor sees a day pass in minutes: at speed 60, feeder time gains a minute
// every second. The two clocks agree at one instant, the anchor, and feeder
// time runs ahead from there:
//
//	feeder = anchor + speed × (wall − anchor)
//
// Every time that the schema calls "feeder time" (the validity of an
// envelope, the time of a reading, when an alert opened) comes from this
// clock. Audit columns stay on the wall clock.
package simclock

import (
	"errors"
	"fmt"
	"time"
)

// MaxSpeed is the fastest a clock may run: an hour of feeder time a second.
const MaxSpeed = 3600

// maxLeadHours is how far feeder time may be from the wall clock when a
// clock is made: 50 years.
const maxLeadHours = 50 * 365 * 24

// Clock converts between wall-clock time and feeder time. It is immutable and
// safe for concurrent use.
type Clock struct {
	anchor time.Time
	speed  float64
	wall   func() time.Time
}

// New returns a clock that agrees with the wall clock at anchor and runs at
// speed from there. A speed of 1 is the wall clock, whatever the anchor.
func New(anchor time.Time, speed float64) (*Clock, error) {
	return NewAt(anchor, speed, time.Now)
}

// NewAt is New with the wall clock given, for tests.
func NewAt(anchor time.Time, speed float64, wall func() time.Time) (*Clock, error) {
	if speed < 1 || speed > MaxSpeed {
		return nil, fmt.Errorf("clock speed %v is outside 1 to %d", speed, MaxSpeed)
	}
	if anchor.IsZero() {
		return nil, errors.New("the clock has no anchor")
	}
	// A fast clock far from its anchor runs out of range: feeder time is
	// held as an offset from the anchor, and a time.Duration holds 292
	// years. A demo is re-anchored when it is deployed, long before that.
	if ahead := wall().Sub(anchor).Abs().Hours() * (speed - 1); ahead > maxLeadHours {
		return nil, fmt.Errorf("at speed %v the clock is %.0f years from its anchor %s; choose a recent anchor",
			speed, ahead/(24*365), anchor.UTC().Format(time.RFC3339))
	}
	return &Clock{anchor: anchor.UTC(), speed: speed, wall: wall}, nil
}

// Anchor is the instant at which feeder time and wall-clock time are equal.
func (c *Clock) Anchor() time.Time { return c.anchor }

// Speed is the number of seconds of feeder time per second of wall time.
func (c *Clock) Speed() float64 { return c.speed }

// Now is feeder time now.
func (c *Clock) Now() time.Time {
	return c.At(c.wall())
}

// At is feeder time at a wall-clock instant.
func (c *Clock) At(wall time.Time) time.Time {
	return c.anchor.Add(time.Duration(float64(wall.Sub(c.anchor)) * c.speed))
}

// Wall is the wall-clock instant at which feeder time reaches t.
func (c *Clock) Wall(t time.Time) time.Time {
	return c.anchor.Add(time.Duration(float64(t.Sub(c.anchor)) / c.speed))
}

// Until is how long to wait, on the wall clock, for feeder time to reach t.
// It is zero when t has passed.
func (c *Clock) Until(t time.Time) time.Duration {
	return max(0, c.Wall(t).Sub(c.wall()))
}

// Real converts a duration of feeder time to the wall-clock time it takes: a
// minute of feeder time is a second at speed 60.
func (c *Clock) Real(feeder time.Duration) time.Duration {
	return time.Duration(float64(feeder) / c.speed)
}
