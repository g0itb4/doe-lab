package dss

import (
	"bytes"
	"encoding/json"
	"flag"
	"math"
	"os"
	"strings"
	"testing"

	"doelab/api/internal/engine"
)

var update = flag.Bool("update", false, "rewrite the fixtures derived from the raw CSIRO data")

func TestNetwork(t *testing.T) {
	t.Parallel()

	c, err := Read(demo, "Master.dss")
	if err != nil {
		t.Fatal(err)
	}
	net, err := c.Network()
	if err != nil {
		t.Fatal(err)
	}
	if err := net.Validate(); err != nil {
		t.Fatal(err)
	}

	// 11 kV across the delta winding, 400/√3 V across each wye winding.
	approx(t, "source voltage", net.Source.VoltageV, 400/math.Sqrt(3))
	// Source at -30 degrees, Dy11 leads by 30.
	approx(t, "source angle", net.Source.AngleDeg, 0)
	// Base impedance 0.4² kV² / 0.5 MVA = 0.32 ohm; 1 % R in total, 4 % X.
	approx(t, "source R", net.Source.ROhm, 0.0032)
	approx(t, "source X", net.Source.XOhm, 0.0128)
	approx(t, "source kVA", net.Source.KVA, 500)

	// Breadth-first from the transformer's low-voltage bus.
	var names []string
	var parents []int
	for _, b := range net.Buses {
		names = append(names, b.Name)
		parents = append(parents, b.Parent)
	}
	if got := strings.Join(names, " "); got != "B1 B2 B3 B4" {
		t.Errorf("bus order = %s", got)
	}
	if parents[0] != -1 || parents[1] != 0 || parents[2] != 1 || parents[3] != 2 {
		t.Errorf("parents = %v", parents)
	}

	main, service, sw := net.Buses[1].Line, net.Buses[2].Line, net.Buses[3].Line
	if main.Name != "L1" || service.Name != "L2" || sw.Name != "S1" || !sw.Switch {
		t.Fatalf("lines = %s %s %s", main.Name, service.Name, sw.Name)
	}
	// 0.5 km of a 1 + j2 ohm/km cable with 10 nF/km.
	approx(t, "main R11", main.ROhm[0][0], 0.5)
	approx(t, "main R12", main.ROhm[0][1], 0.05)
	approx(t, "main X44", main.XOhm[3][3], 1)
	approx(t, "main B11", main.BS[0][0], 2*math.Pi*50*10e-9*0.5)
	// 50 m of a 2 + j1 ohm/km cable.
	approx(t, "service R22", service.ROhm[1][1], 0.1)
	approx(t, "service X22", service.XOhm[1][1], 0.05)
	// A switch is 1 + j1 milliohm per conductor, uncoupled, whatever its
	// linecode and length say.
	approx(t, "switch R11", sw.ROhm[0][0], 0.001)
	approx(t, "switch X33", sw.XOhm[2][2], 0.001)
	if sw.ROhm[0][1] != 0 || sw.BS[0][0] != 0 {
		t.Errorf("switch has coupling or capacitance: %+v", sw)
	}

	if g := net.Buses[0].Ground; g == nil || g.ROhm != 0.3 {
		t.Errorf("transformer bus ground = %+v", g)
	}
	if g := net.Buses[2].Ground; g == nil || g.ROhm != 10 {
		t.Errorf("B3 ground = %+v", g)
	}
	if net.Buses[1].Ground != nil {
		t.Errorf("B2 has a ground")
	}

	want := []engine.Site{{Name: "Ld1", Bus: 2, Phase: 1}, {Name: "Ld2", Bus: 3, Phase: 3}}
	if len(net.Sites) != 2 || net.Sites[0] != want[0] || net.Sites[1] != want[1] {
		t.Errorf("sites = %+v", net.Sites)
	}
}

func TestNetworkLagAndTap(t *testing.T) {
	t.Parallel()

	c, err := Read(demo, "Master.dss")
	if err != nil {
		t.Fatal(err)
	}
	c.Transformers[0].Lead = false
	c.Transformers[0].Windings[1].Tap = 1.05
	c.Source.PU = 0.98
	net, err := c.Network()
	if err != nil {
		t.Fatal(err)
	}
	approx(t, "source angle (Dy1 lags)", net.Source.AngleDeg, -60)
	approx(t, "source voltage", net.Source.VoltageV, 0.98*1.05*400/math.Sqrt(3))
}

func TestNetworkErrors(t *testing.T) {
	t.Parallel()

	all := []int{1, 2, 3, 4}
	tests := []struct {
		name   string
		change func(c *Circuit)
		want   string
	}{
		{name: "no transformer", change: func(c *Circuit) { c.Transformers = nil }, want: "want exactly one two-winding transformer"},
		{name: "three windings", change: func(c *Circuit) {
			c.Transformers[0].Windings = append(c.Transformers[0].Windings, Winding{})
		}, want: "want exactly one two-winding transformer"},
		{name: "wye primary", change: func(c *Circuit) { c.Transformers[0].Windings[0].Delta = false }, want: "want a delta winding on the source bus"},
		{name: "delta secondary", change: func(c *Circuit) { c.Transformers[0].Windings[1].Delta = true }, want: "an earthed wye winding"},
		{name: "primary not on the source bus", change: func(c *Circuit) { c.Source.Bus = "elsewhere" }, want: "want a delta winding on the source bus"},
		{name: "unearthed star point", change: func(c *Circuit) { c.Transformers[0].Windings[1].Bus.Nodes = all }, want: "an earthed wye winding"},
		{name: "zero kv", change: func(c *Circuit) { c.Transformers[0].Windings[1].KV = 0 }, want: "kv, kva and tap must be positive"},
		{name: "zero kva", change: func(c *Circuit) { c.Transformers[0].Windings[1].KVA = 0 }, want: "kv, kva and tap must be positive"},
		{name: "zero tap", change: func(c *Circuit) { c.Transformers[0].Windings[0].Tap = 0 }, want: "kv, kva and tap must be positive"},
		{name: "zero primary kv", change: func(c *Circuit) { c.Transformers[0].Windings[0].KV = 0 }, want: "kv, kva and tap must be positive"},
		{name: "three-wire line", change: func(c *Circuit) { c.Lines[1].Conductors = 3 }, want: "line L2: want a 4-conductor line"},
		{name: "line on other nodes", change: func(c *Circuit) { c.Lines[1].Bus1.Nodes = []int{1, 2, 3, 0} }, want: "line L2: want a 4-conductor line on nodes 1.2.3.4"},
		{name: "line end on other nodes", change: func(c *Circuit) { c.Lines[1].Bus2.Nodes = []int{4, 3, 2, 1} }, want: "line L2: want a 4-conductor line on nodes 1.2.3.4"},
		{name: "unknown linecode", change: func(c *Circuit) { c.Lines[1].Linecode = "gone" }, want: `line L2: no 4-conductor linecode "gone"`},
		{name: "three-wire linecode", change: func(c *Circuit) {
			lc := c.Linecodes["service"]
			lc.Conductors = 3
			c.Linecodes["service"] = lc
		}, want: `line L2: no 4-conductor linecode "service"`},
		{name: "island", change: func(c *Circuit) {
			c.Lines = append(c.Lines, Line{
				Name: "far", Switch: true, Conductors: 4,
				Bus1: BusRef{Bus: "X1", Nodes: all}, Bus2: BusRef{Bus: "X2", Nodes: all},
			})
		}, want: "2 of 6 buses are not connected to B1"},
		{name: "root with no lines", change: func(c *Circuit) { c.Transformers[0].Windings[1].Bus.Bus = "alone" }, want: "4 of 5 buses are not connected to alone"},
		{name: "loop", change: func(c *Circuit) {
			c.Lines = append(c.Lines, Line{
				Name: "tie", Switch: true, Conductors: 4,
				Bus1: BusRef{Bus: "B4", Nodes: all}, Bus2: BusRef{Bus: "B1", Nodes: all},
			})
		}, want: "4 lines join 4 buses, so the feeder has a loop"},
		{name: "reactor on an unknown bus", change: func(c *Circuit) { c.Reactors[0].Bus1.Bus = "nowhere" }, want: "reactor g1: want one neutral-to-earth reactor"},
		{name: "reactor between buses", change: func(c *Circuit) { c.Reactors[0].Bus2.Bus = "B2" }, want: "reactor g1"},
		{name: "reactor on a phase", change: func(c *Circuit) { c.Reactors[0].Bus1.Nodes = []int{1} }, want: "reactor g1"},
		{name: "reactor not to earth", change: func(c *Circuit) { c.Reactors[0].Bus2.Nodes = []int{4} }, want: "reactor g1"},
		{name: "second reactor on a bus", change: func(c *Circuit) { c.Reactors = append(c.Reactors, c.Reactors[1]) }, want: "reactor g3"},
		{name: "three-phase load", change: func(c *Circuit) { c.Loads[0].Phases = 3 }, want: "load Ld1: want a single-phase load"},
		{name: "load on an unknown bus", change: func(c *Circuit) { c.Loads[0].Bus.Bus = "nowhere" }, want: "load Ld1"},
		{name: "phase-to-phase load", change: func(c *Circuit) { c.Loads[0].Bus.Nodes = []int{1, 2} }, want: "load Ld1"},
		{name: "load on the neutral", change: func(c *Circuit) { c.Loads[0].Bus.Nodes = []int{4, 4} }, want: "load Ld1"},
		{name: "load on node 0", change: func(c *Circuit) { c.Loads[0].Bus.Nodes = []int{0, 4} }, want: "load Ld1"},
		{name: "load with one node", change: func(c *Circuit) { c.Loads[0].Bus.Nodes = []int{1} }, want: "load Ld1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, err := Read(demo, "Master.dss")
			if err != nil {
				t.Fatal(err)
			}
			tt.change(c)
			_, err = c.Network()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

// lv10Fixture is the network that the solver's tests load. It is derived from
// the CSIRO feeder (CC BY-NC-SA 4.0, see data/derived/LICENSE) and is
// rewritten by `go test ./internal/engine/dss -run TestLV10Network -update`.
const lv10Fixture = "../testdata/lv10_network.json"

func TestLV10Network(t *testing.T) {
	t.Parallel()

	net, err := readLV10(t).Network()
	if err != nil {
		t.Fatal(err)
	}
	if err := net.Validate(); err != nil {
		t.Fatal(err)
	}

	if len(net.Buses) != 223 {
		t.Errorf("%d buses, want 223", len(net.Buses))
	}
	if net.Buses[0].Name != "B1862" {
		t.Errorf("root = %s, want the transformer's low-voltage bus B1862", net.Buses[0].Name)
	}
	// Every bus but the root hangs off an earlier bus by exactly one line, so
	// the feeder is a connected tree with no loop. The open switch
	// Switch_1343 is commented out in the source and must not appear.
	switches := 0
	for _, b := range net.Buses[1:] {
		if b.Line.Switch {
			switches++
		}
		if strings.Contains(b.Line.Name, "1343") {
			t.Errorf("open switch %s is in the network", b.Line.Name)
		}
	}
	if switches != 10 {
		t.Errorf("%d closed switches, want 10", switches)
	}

	approx(t, "transformer kVA", net.Source.KVA, 500)
	approx(t, "source voltage", net.Source.VoltageV, 433/math.Sqrt(3))
	approx(t, "source angle", net.Source.AngleDeg, 0)

	perPhase := map[int]int{}
	for _, s := range net.Sites {
		perPhase[s.Phase]++
	}
	if len(net.Sites) != 94 || perPhase[1] != 32 || perPhase[2] != 31 || perPhase[3] != 31 {
		t.Errorf("%d sites split %v, want 94 split 32/31/31", len(net.Sites), perPhase)
	}

	grounds := 0
	for _, b := range net.Buses {
		if b.Ground != nil {
			grounds++
		}
	}
	if grounds != 95 {
		t.Errorf("%d earthed buses, want 95 (the transformer and 94 customers)", grounds)
	}

	got, err := json.Marshal(net)
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if *update {
		if err := os.WriteFile(lv10Fixture, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(lv10Fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s is out of date with the raw data and the parser; rerun with -update", lv10Fixture)
	}
}
