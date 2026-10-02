package engine

import (
	"errors"
	"math"
	"math/cmplx"
	"strings"
	"testing"
)

// handCase is a feeder small enough to solve on paper. Everything is
// resistive and the conductors are uncoupled, so the one customer's current
// flows round a single loop and the power flow reduces to a quadratic.
//
//	source   240 V per phase behind 0.02 ohm
//	main     tx -> mid     0.04 ohm per conductor
//	service  mid -> house  0.15 ohm per conductor
//	earth    the neutral is earthed at the transformer only, through 0.6 ohm
//	load     2300 W at unity power factor, phase 1 to neutral, at the house
//
// The loop, from the source round to the star point:
//
//	phase conductor   0.02 + 0.04 + 0.15         = 0.21 ohm
//	neutral conductor 0.15 + 0.04                = 0.19 ohm
//	earth electrode                                0.60 ohm
//	total R                                        1.00 ohm
//
// A constant-power load P across the loop gives E = I·R + P/I, so
//
//	R·I² - E·I + P = 0
//	I = (240 - sqrt(240² - 4·1·2300)) / 2 = (240 - 220) / 2 = 10 A
//
// (the other root, 230 A, is the collapsed low-voltage solution). Then, to
// earth:
//
//	           phase 1                    neutral
//	tx      240 - 10·0.02 = 239.8      10·0.60        = 6.0
//	mid     239.8 - 10·0.04 = 239.4    6.0 + 10·0.04  = 6.4
//	house   239.4 - 10·0.15 = 237.9    6.4 + 10·0.15  = 7.9
//
// The customer sees 237.9 - 7.9 = 230 V, and 230 V · 10 A = 2300 W. Phases 2
// and 3 carry no current and stay at 240 V everywhere.
func handCase() *Network {
	line := func(name string, r float64) *Line {
		l := &Line{Name: name, Linecode: "hand", LengthKm: 0.1}
		for i := range Conductors {
			l.ROhm[i][i] = r
		}
		return l
	}
	return &Network{
		Name:   "hand-case",
		Source: Source{KVA: 100, VoltageV: 240, ROhm: 0.02},
		Buses: []Bus{
			{Name: "tx", Parent: -1, Ground: &Impedance{ROhm: 0.6}},
			{Name: "mid", Parent: 0, Line: line("main", 0.04)},
			{Name: "house", Parent: 1, Line: line("service", 0.15)},
		},
		Sites: []Site{{Name: "house-a", Bus: 2, Phase: 1}},
	}
}

func TestHandCase(t *testing.T) {
	t.Parallel()

	for name, method := range map[string]Method{"tree": Tree, "dense": Dense} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			pf, err := NewPowerFlow(handCase(), method)
			if err != nil {
				t.Fatal(err)
			}
			var sol Solution
			if err := pf.Solve([]complex128{2300}, &sol); err != nil {
				t.Fatal(err)
			}

			want := [][2]float64{{239.8, 6.0}, {239.4, 6.4}, {237.9, 7.9}}
			for i, w := range want {
				near(t, "phase 1 at bus", real(sol.V[i][0]), w[0], 1e-6)
				near(t, "neutral at bus", real(sol.V[i][Neutral]), w[1], 1e-6)
				near(t, "phase 1 angle", imag(sol.V[i][0]), 0, 1e-6)
				// The unloaded phases sit at the source voltage, 120 degrees
				// apart.
				near(t, "phase 2 magnitude", cmplx.Abs(sol.V[i][1]), 240, 1e-6)
				near(t, "phase 2 angle", cmplx.Phase(sol.V[i][1])*180/math.Pi, -120, 1e-6)
				near(t, "phase 3 angle", cmplx.Phase(sol.V[i][2])*180/math.Pi, 120, 1e-6)
			}
			near(t, "customer voltage", pf.PhaseVoltage(&sol, 0), 230, 1e-6)

			for _, bus := range []int{1, 2} {
				current := pf.LineCurrent(&sol, bus)
				near(t, "phase 1 current", current[0], 10, 1e-6)
				near(t, "neutral current", current[Neutral], 10, 1e-6)
				near(t, "phase 2 current", current[1], 0, 1e-6)
			}
			// Each line carries what the customer draws and what is lost
			// beyond its near end: 10² · 0.08 ohm in the main, out and back,
			// and 10² · 0.30 ohm in the service.
			near(t, "power into the main", real(pf.LinePower(&sol, 1)), 2338, 1e-5)
			near(t, "power into the service", real(pf.LinePower(&sol, 2)), 2330, 1e-5)
			near(t, "reactive power into the main", imag(pf.LinePower(&sol, 1)), 0, 1e-5)
			// Phase to neutral at the house: 237.9 − 7.9 V on the loaded
			// phase, and the idle phases shifted by the neutral.
			house := pf.BusVoltage(&sol, 2)
			near(t, "phase 1 to neutral at the house", house[0], 230, 1e-6)
			if house[1] <= 240 || house[2] <= 240 {
				t.Errorf("idle phases at the house = %v, want both above 240 V", house)
			}
			// 240 V · 10 A leaves the source: 2300 W to the customer and
			// 10² · 1.00 ohm = 100 W of loss.
			power := pf.SourcePower(&sol)
			near(t, "source P", real(power), 2400, 1e-5)
			near(t, "source Q", imag(power), 0, 1e-5)
		})
	}
}

func TestExportRaisesVoltage(t *testing.T) {
	t.Parallel()

	pf, err := NewPowerFlow(handCase(), Tree)
	if err != nil {
		t.Fatal(err)
	}
	// Exporting P reverses the current: -P = V·I with I = -(E - V)/R, so
	// V = (E + sqrt(E² + 4·R·P)) / 2 across the customer. For P = 2500 W:
	// sqrt(57600 + 10000) = 260, V = 250 V, and 10 A flows back.
	var sol Solution
	if err := pf.Solve([]complex128{-2500}, &sol); err != nil {
		t.Fatal(err)
	}
	near(t, "customer voltage", pf.PhaseVoltage(&sol, 0), 250, 1e-6)
	near(t, "source P", real(pf.SourcePower(&sol)), -2400, 1e-5)
	// The power flows back: 2500 W less the service's loss reaches the main,
	// and less the main's reaches the transformer's bus.
	near(t, "power into the service", real(pf.LinePower(&sol, 2)), -(2500 - 30), 1e-5)
	near(t, "power into the main", real(pf.LinePower(&sol, 1)), -(2500 - 38), 1e-5)
}

func TestWarmStart(t *testing.T) {
	t.Parallel()

	pf, err := NewPowerFlow(handCase(), Tree)
	if err != nil {
		t.Fatal(err)
	}
	var sol Solution
	if err := pf.Solve([]complex128{2300}, &sol); err != nil {
		t.Fatal(err)
	}
	cold := sol.Iterations
	if err := pf.Solve([]complex128{2300}, &sol); err != nil {
		t.Fatal(err)
	}
	if sol.Iterations != 1 || cold <= 1 {
		t.Errorf("cold start took %d iterations and the repeat %d, want more than 1 and exactly 1", cold, sol.Iterations)
	}

	// No load: the solve is the no-load profile itself.
	var idle Solution
	if err := pf.Solve([]complex128{0}, &idle); err != nil {
		t.Fatal(err)
	}
	if idle.Iterations != 1 {
		t.Errorf("no-load solve took %d iterations, want 1", idle.Iterations)
	}
	near(t, "no-load voltage", pf.PhaseVoltage(&idle, 0), 240, 1e-9)
}

func TestVoltageCollapse(t *testing.T) {
	t.Parallel()

	pf, err := NewPowerFlow(handCase(), Tree)
	if err != nil {
		t.Fatal(err)
	}
	// The loop can deliver at most E²/4R = 14.4 kW. Asking for 20 kW has no
	// solution.
	var sol Solution
	err = pf.Solve([]complex128{20000}, &sol)
	if !errors.Is(err, ErrNotConverged) {
		t.Fatalf("error = %v, want ErrNotConverged", err)
	}
	if sol.V != nil {
		t.Error("a failed solve left voltages behind as a starting point")
	}
	// The same Solution recovers on the next, feasible, solve.
	if err := pf.Solve([]complex128{2300}, &sol); err != nil {
		t.Fatal(err)
	}
	near(t, "customer voltage after recovery", pf.PhaseVoltage(&sol, 0), 230, 1e-6)
}

func TestSolveWrongLoadCount(t *testing.T) {
	t.Parallel()

	pf, err := NewPowerFlow(handCase(), Tree)
	if err != nil {
		t.Fatal(err)
	}
	err = pf.Solve([]complex128{1, 2}, &Solution{})
	if err == nil || !strings.Contains(err.Error(), "got 2 loads for 1 sites") {
		t.Errorf("error = %v", err)
	}
}

func TestNewPowerFlowErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(n *Network)
		want   string
	}{
		{name: "invalid network", change: func(n *Network) { n.Buses = nil }, want: "no buses"},
		{name: "floating neutral", change: func(n *Network) { n.Buses[0].Ground = nil }, want: "the admittance matrix is singular"},
		{name: "zero-impedance line", change: func(n *Network) { n.Buses[1].Line.ROhm = Matrix{} }, want: "line main has a singular impedance matrix"},
	}
	for _, tt := range tests {
		for name, method := range map[string]Method{"tree": Tree, "dense": Dense} {
			t.Run(tt.name+"/"+name, func(t *testing.T) {
				t.Parallel()
				n := handCase()
				tt.change(n)
				_, err := NewPowerFlow(n, method)
				if !errors.Is(err, ErrInvalidNetwork) || !strings.Contains(err.Error(), tt.want) {
					t.Errorf("error = %v, want ErrInvalidNetwork containing %q", err, tt.want)
				}
			})
		}
	}
}

func near(t *testing.T, what string, got, want, tolerance float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Errorf("%s = %.9g, want %.9g", what, got, want)
	}
}
