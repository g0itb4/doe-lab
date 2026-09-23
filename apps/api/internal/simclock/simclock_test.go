package simclock

import (
	"strings"
	"testing"
	"time"
)

var anchor = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func TestWallClockAtSpeedOne(t *testing.T) {
	t.Parallel()
	wall := anchor.Add(37 * time.Hour)
	// The anchor does not matter at speed 1.
	c, err := NewAt(time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC), 1, func() time.Time { return wall })
	if err != nil {
		t.Fatal(err)
	}
	if !c.Now().Equal(wall) || !c.Wall(wall).Equal(wall) || c.Speed() != 1 {
		t.Errorf("Now = %v, want the wall clock %v", c.Now(), wall)
	}
	if got := c.Until(wall.Add(time.Minute)); got != time.Minute {
		t.Errorf("Until = %v", got)
	}
}

func TestAcceleratedClock(t *testing.T) {
	t.Parallel()
	wall := anchor.Add(10 * time.Second)
	c, err := NewAt(anchor, 60, func() time.Time { return wall })
	if err != nil {
		t.Fatal(err)
	}
	if !c.Anchor().Equal(anchor) || c.Speed() != 60 {
		t.Errorf("anchor %v, speed %v", c.Anchor(), c.Speed())
	}
	// Ten seconds on the wall are ten minutes on the feeder.
	if got := c.Now(); !got.Equal(anchor.Add(10 * time.Minute)) {
		t.Errorf("Now = %v, want ten minutes past the anchor", got)
	}
	// The next half hour of feeder time is twenty minutes of feeder time
	// away: twenty seconds.
	next := anchor.Add(30 * time.Minute)
	if got := c.Until(next); got != 20*time.Second {
		t.Errorf("Until = %v, want 20s", got)
	}
	if got := c.Wall(next); !got.Equal(anchor.Add(30 * time.Second)) {
		t.Errorf("Wall = %v", got)
	}
	// A time that has passed needs no wait.
	if got := c.Until(anchor); got != 0 {
		t.Errorf("Until of a past time = %v", got)
	}
	if got := c.Real(time.Minute); got != time.Second {
		t.Errorf("Real(1m) = %v, want 1s", got)
	}
	// At and Wall are inverses.
	if got := c.Wall(c.At(wall)); !got.Equal(wall) {
		t.Errorf("Wall(At(w)) = %v, want %v", got, wall)
	}
}

func TestNewUsesTheRealClock(t *testing.T) {
	t.Parallel()
	c, err := New(anchor.In(time.FixedZone("AEST", 10*3600)), 1)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(c.Now()); d < 0 || d > time.Minute {
		t.Errorf("Now is %v from the wall clock", d)
	}
	if c.Anchor().Location() != time.UTC {
		t.Errorf("anchor kept its zone: %v", c.Anchor())
	}
}

func TestAnchorTooFarForAFastClock(t *testing.T) {
	t.Parallel()
	wall := func() time.Time { return anchor.AddDate(2, 0, 0) }
	// Two years at speed 60 is 118 years of lead.
	if _, err := NewAt(anchor, 60, wall); err == nil || !strings.Contains(err.Error(), "choose a recent anchor") {
		t.Errorf("a fast clock two years from its anchor: %v", err)
	}
	// The same distance is fine at speed 1, and a short one at speed 60.
	if _, err := NewAt(anchor, 1, wall); err != nil {
		t.Errorf("speed 1: %v", err)
	}
	if _, err := NewAt(anchor, 60, func() time.Time { return anchor.AddDate(0, 3, 0) }); err != nil {
		t.Errorf("three months at speed 60: %v", err)
	}
}

func TestInvalidClocks(t *testing.T) {
	t.Parallel()
	for name, tt := range map[string]struct {
		anchor time.Time
		speed  float64
		want   string
	}{
		"stopped":   {anchor, 0, "outside 1 to 3600"},
		"slow":      {anchor, 0.5, "outside 1 to 3600"},
		"backwards": {anchor, -1, "outside 1 to 3600"},
		"too fast":  {anchor, 3601, "outside 1 to 3600"},
		"no anchor": {time.Time{}, 60, "no anchor"},
	} {
		if _, err := New(tt.anchor, tt.speed); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: error = %v", name, err)
		}
	}
}
