package engine

import (
	"errors"
	"fmt"
)

// Conductors is the number of conductors at every bus: three phases and the
// neutral. Earth is the reference and is not a conductor.
const Conductors = 4

// Neutral is the index of the neutral conductor.
const Neutral = 3

// NominalVoltage is the Australian phase-to-neutral nominal voltage in volts
// (AS 60038). Per-unit voltages are expressed against it.
const NominalVoltage = 230.0

// Matrix is a conductor-by-conductor quantity of one line.
type Matrix [Conductors][Conductors]float64

// Network is a radial low-voltage feeder below one transformer.
type Network struct {
	Name   string `json:"name"`
	Source Source `json:"source"`
	// Buses is in tree order: the transformer's low-voltage bus first, and
	// every other bus after its parent.
	Buses []Bus  `json:"buses"`
	Sites []Site `json:"sites"`
}

// Source is the transformer seen from its low-voltage terminals: per phase, a
// voltage source to earth behind a series impedance. This is exact for a
// delta-wye transformer on a stiff supply, where each low-voltage phase
// winding is driven by one fixed line-to-line voltage.
type Source struct {
	KVA float64 `json:"kva"`
	// VoltageV is the open-circuit phase-to-earth voltage.
	VoltageV float64 `json:"voltage_v"`
	// AngleDeg is the angle of phase 1. Phases 2 and 3 lag it by 120 and 240
	// degrees.
	AngleDeg float64 `json:"angle_deg"`
	ROhm     float64 `json:"r_ohm"`
	XOhm     float64 `json:"x_ohm"`
}

// Bus is a node of the feeder tree.
type Bus struct {
	Name string `json:"name"`
	// Parent is the index of the bus towards the transformer, or -1 for the
	// first bus.
	Parent int `json:"parent"`
	// Line joins the bus to its parent. The first bus has none.
	Line *Line `json:"line,omitempty"`
	// Ground is the impedance from the neutral to earth, when the neutral is
	// earthed here.
	Ground *Impedance `json:"ground,omitempty"`
}

// Impedance is a series resistance and reactance.
type Impedance struct {
	ROhm float64 `json:"r_ohm"`
	XOhm float64 `json:"x_ohm"`
}

// Line is a four-conductor line or a closed switch.
type Line struct {
	Name     string  `json:"name"`
	Linecode string  `json:"linecode"`
	LengthKm float64 `json:"length_km"`
	Switch   bool    `json:"switch,omitempty"`
	// ROhm and XOhm are the series impedance of the whole length, with earth
	// return (Carson).
	ROhm Matrix `json:"r_ohm"`
	XOhm Matrix `json:"x_ohm"`
	// BS is the shunt susceptance of the whole length in siemens. Half of it
	// sits at each end.
	BS Matrix `json:"b_s"`
}

// Site is a single-phase customer connection, between one phase and the
// neutral.
type Site struct {
	Name string `json:"name"`
	Bus  int    `json:"bus"`
	// Phase is 1, 2 or 3.
	Phase int `json:"phase"`
}

// ErrInvalidNetwork is wrapped by every error from Validate.
var ErrInvalidNetwork = errors.New("invalid network")

// Validate checks the invariants that the solver relies on.
func (n *Network) Validate() error {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalidNetwork, fmt.Sprintf(format, args...))
	}
	if n.Source.VoltageV <= 0 || n.Source.KVA <= 0 {
		return invalid("source voltage and rating must be positive")
	}
	if n.Source.ROhm <= 0 && n.Source.XOhm <= 0 {
		return invalid("source impedance must not be zero")
	}
	if len(n.Buses) == 0 {
		return invalid("no buses")
	}
	for i, b := range n.Buses {
		switch {
		case i == 0 && (b.Parent != -1 || b.Line != nil):
			return invalid("bus 0 (%s) must be the root, with parent -1 and no line", b.Name)
		case i > 0 && (b.Parent < 0 || b.Parent >= i):
			return invalid("bus %d (%s) has parent %d, want an earlier bus", i, b.Name, b.Parent)
		case i > 0 && b.Line == nil:
			return invalid("bus %d (%s) has no line to its parent", i, b.Name)
		}
	}
	for _, s := range n.Sites {
		if s.Bus < 0 || s.Bus >= len(n.Buses) {
			return invalid("site %s is on bus %d, which does not exist", s.Name, s.Bus)
		}
		if s.Phase < 1 || s.Phase > 3 {
			return invalid("site %s is on phase %d, want 1 to 3", s.Name, s.Phase)
		}
	}
	return nil
}
