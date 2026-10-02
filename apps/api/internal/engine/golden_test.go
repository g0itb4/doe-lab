package engine

import (
	"encoding/json"
	"math"
	"math/cmplx"
	"os"
	"strings"
	"sync"
	"testing"
)

// snapshot is one OpenDSS reference solution written by
// tools/opendss_snapshots.py.
type snapshot struct {
	Name  string `json:"name"`
	Sites map[string]struct {
		PW   float64 `json:"p_w"`
		QVar float64 `json:"q_var"`
	} `json:"sites"`
	TransformerW   float64                  `json:"transformer_w"`
	TransformerVar float64                  `json:"transformer_var"`
	Voltages       map[string][4][2]float64 `json:"voltages"`
	LineCurrentsA  map[string][4]float64    `json:"line_currents_a"`
}

var snapshotNames = []string{"zero", "light", "peak", "pv_export", "phase_a_export"}

func readJSON(t testing.TB, path string, into any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

var lv10 = sync.OnceValue(func() *Network {
	var net Network
	data, err := os.ReadFile("testdata/lv10_network.json")
	if err != nil {
		panic(err)
	}
	if err := json.Unmarshal(data, &net); err != nil {
		panic(err)
	}
	return &net
})

// loadsFor returns the snapshot's power per site, in the network's site order.
// OpenDSS lower-cases names.
func loadsFor(t testing.TB, net *Network, snap *snapshot) []complex128 {
	t.Helper()
	load := make([]complex128, len(net.Sites))
	for i, s := range net.Sites {
		p, ok := snap.Sites[strings.ToLower(s.Name)]
		if !ok {
			t.Fatalf("snapshot %s has no site %s", snap.Name, s.Name)
		}
		load[i] = complex(p.PW, p.QVar)
	}
	return load
}

// Tolerances of the Go solver against OpenDSS on LV10, fixed from the first
// comparison (2026-09-19). The largest gaps measured, over the five snapshots
// and both methods:
//
//	phase-to-neutral magnitude  3.4e-4 V  (1.5e-6 pu)
//	complex node voltage        9.9e-4 V
//	transformer active power    0.03 W in 373 kW
//	transformer reactive power  0.43 var
//
// The target was 0.1 % of voltage magnitude (1e-3 pu); the engine is about
// 700 times closer. What remains is OpenDSS's own modelling of things the
// engine leaves out: the source's small internal impedance behind the
// "ideal" 11 kV bus, and the 1 ppm shunt that OpenDSS adds to every
// transformer winding to keep its matrix from floating. The reactive power gap
// is that shunt.
const (
	goldenPhaseNeutralPU = 5e-6
	goldenNodeVolts      = 5e-3
	goldenPowerVA        = 1.0
	goldenCurrentA       = 5e-3
)

// lv10Dense is shared: factorising the 892×892 matrix takes about 10 s under
// the race detector.
var lv10Dense = sync.OnceValues(func() (*PowerFlow, error) {
	return NewPowerFlow(lv10(), Dense)
})

// skipSlowDense skips a test of the dense method on LV10 in -short mode, which
// is what the pre-commit hook runs. The tree method is always checked against
// OpenDSS, and the dense method is always checked on the hand-solved case;
// `just test` and CI run without -short.
func skipSlowDense(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("dense factorisation of LV10 is slow under the race detector")
	}
}

func TestGoldenOpenDSS(t *testing.T) {
	t.Parallel()
	net := lv10()

	for _, method := range []Method{Tree, Dense} {
		var pf *PowerFlow
		var err error
		if method == Dense {
			if testing.Short() {
				continue
			}
			pf, err = lv10Dense()
		} else {
			pf, err = NewPowerFlow(net, method)
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range snapshotNames {
			var snap snapshot
			readJSON(t, "testdata/lv10_"+name+".json", &snap)
			var sol Solution
			if err := pf.Solve(loadsFor(t, net, &snap), &sol); err != nil {
				t.Fatalf("%s: %v", name, err)
			}

			worstNode, worstPN, worstCurrent := 0.0, 0.0, 0.0
			for i, b := range net.Buses {
				ref, ok := snap.Voltages[strings.ToLower(b.Name)]
				if !ok {
					t.Fatalf("snapshot %s has no bus %s", name, b.Name)
				}
				refNeutral := complex(ref[Neutral][0], ref[Neutral][1])
				for c := range Conductors {
					r := complex(ref[c][0], ref[c][1])
					worstNode = math.Max(worstNode, cmplx.Abs(sol.V[i][c]-r))
					if c != Neutral {
						got := cmplx.Abs(sol.V[i][c] - sol.V[i][Neutral])
						worstPN = math.Max(worstPN, math.Abs(got-cmplx.Abs(r-refNeutral)))
					}
				}
				if b.Line == nil {
					continue
				}
				// OpenDSS reports the current at the line's first terminal,
				// which may be either end; the two differ by the charging
				// current, far below the tolerance.
				refCurrent, ok := snap.LineCurrentsA[strings.ToLower(b.Line.Name)]
				if !ok {
					t.Fatalf("snapshot %s has no line %s", name, b.Line.Name)
				}
				got := pf.LineCurrent(&sol, i)
				for c := range Conductors {
					worstCurrent = math.Max(worstCurrent, math.Abs(got[c]-refCurrent[c]))
				}
			}
			power := pf.SourcePower(&sol)
			t.Logf("method %d %-15s %2d iterations: node %.1e V, phase-neutral %.1e pu, current %.1e A, power %.2f W %.2f var",
				method, name, sol.Iterations, worstNode, worstPN/NominalVoltage, worstCurrent,
				real(power)-snap.TransformerW, imag(power)-snap.TransformerVar)

			if worstPN/NominalVoltage > goldenPhaseNeutralPU {
				t.Errorf("%s: phase-to-neutral voltage is %.2e pu from OpenDSS, tolerance %.0e", name, worstPN/NominalVoltage, goldenPhaseNeutralPU)
			}
			if worstNode > goldenNodeVolts {
				t.Errorf("%s: node voltage is %.2e V from OpenDSS, tolerance %.0e", name, worstNode, goldenNodeVolts)
			}
			if worstCurrent > goldenCurrentA {
				t.Errorf("%s: line current is %.2e A from OpenDSS, tolerance %.0e", name, worstCurrent, goldenCurrentA)
			}
			if d := cmplx.Abs(power - complex(snap.TransformerW, snap.TransformerVar)); d > goldenPowerVA {
				t.Errorf("%s: transformer power is %.2f VA from OpenDSS, tolerance %.0f", name, d, goldenPowerVA)
			}
		}
	}
}

// The two factorisations solve the same matrix. They must agree far more
// closely than either agrees with OpenDSS.
func TestTreeMatchesDense(t *testing.T) {
	t.Parallel()
	skipSlowDense(t)
	net := lv10()

	treePF, err := NewPowerFlow(net, Tree)
	if err != nil {
		t.Fatal(err)
	}
	densePF, err := lv10Dense()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range snapshotNames {
		var snap snapshot
		readJSON(t, "testdata/lv10_"+name+".json", &snap)
		load := loadsFor(t, net, &snap)
		var a, b Solution
		if err := treePF.Solve(load, &a); err != nil {
			t.Fatal(err)
		}
		if err := densePF.Solve(load, &b); err != nil {
			t.Fatal(err)
		}
		worst := 0.0
		for i := range a.V {
			for c := range Conductors {
				worst = math.Max(worst, cmplx.Abs(a.V[i][c]-b.V[i][c]))
			}
		}
		t.Logf("%-15s tree and dense differ by %.1e pu", name, worst/NominalVoltage)
		if worst/NominalVoltage > 1e-9 {
			t.Errorf("%s: tree and dense differ by %.2e pu, want at most 1e-9", name, worst/NominalVoltage)
		}
	}
}

// templateNetworks are the smaller feeders that the fleet import builds its
// other feeders from. The parser and the solver were written against LV10;
// these show that they read five other networks as OpenDSS does.
var templateNetworks = []string{"lv2", "lv3", "lv13", "lv22", "lv32"}

func TestGoldenTemplates(t *testing.T) {
	t.Parallel()

	for _, prefix := range templateNetworks {
		t.Run(prefix, func(t *testing.T) {
			t.Parallel()
			var net Network
			readJSON(t, "testdata/"+prefix+"_network.json", &net)
			for _, method := range []Method{Tree, Dense} {
				pf, err := NewPowerFlow(&net, method)
				if err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"peak", "pv_export"} {
					var snap snapshot
					readJSON(t, "testdata/"+prefix+"_"+name+".json", &snap)
					var sol Solution
					if err := pf.Solve(loadsFor(t, &net, &snap), &sol); err != nil {
						t.Fatalf("%s: %v", name, err)
					}

					worstPN, worstCurrent := 0.0, 0.0
					for i, b := range net.Buses {
						ref := snap.Voltages[strings.ToLower(b.Name)]
						refNeutral := complex(ref[Neutral][0], ref[Neutral][1])
						for c := range Neutral {
							got := cmplx.Abs(sol.V[i][c] - sol.V[i][Neutral])
							want := cmplx.Abs(complex(ref[c][0], ref[c][1]) - refNeutral)
							worstPN = math.Max(worstPN, math.Abs(got-want))
						}
						if b.Line == nil {
							continue
						}
						refCurrent := snap.LineCurrentsA[strings.ToLower(b.Line.Name)]
						got := pf.LineCurrent(&sol, i)
						for c := range Conductors {
							worstCurrent = math.Max(worstCurrent, math.Abs(got[c]-refCurrent[c]))
						}
					}
					power := pf.SourcePower(&sol)
					gap := cmplx.Abs(power - complex(snap.TransformerW, snap.TransformerVar))
					t.Logf("method %d %-10s phase-neutral %.1e pu, current %.1e A, power %.2f VA",
						method, name, worstPN/NominalVoltage, worstCurrent, gap)

					if worstPN/NominalVoltage > goldenPhaseNeutralPU {
						t.Errorf("%s: phase-to-neutral voltage is %.2e pu from OpenDSS, tolerance %.0e", name, worstPN/NominalVoltage, goldenPhaseNeutralPU)
					}
					if worstCurrent > goldenCurrentA {
						t.Errorf("%s: line current is %.2e A from OpenDSS, tolerance %.0e", name, worstCurrent, goldenCurrentA)
					}
					if gap > goldenPowerVA {
						t.Errorf("%s: transformer power is %.2f VA from OpenDSS, tolerance %.0f", name, gap, goldenPowerVA)
					}
				}
			}
		})
	}
}
