package topology

import (
	"encoding/json"
	"errors"
	"math"
	"math/cmplx"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
	"doelab/api/internal/engine"
)

func lv10(t *testing.T) *engine.Network {
	t.Helper()
	data, err := os.ReadFile("../engine/testdata/lv10_network.json")
	if err != nil {
		t.Fatal(err)
	}
	var net engine.Network
	if err := json.Unmarshal(data, &net); err != nil {
		t.Fatal(err)
	}
	return &net
}

// store gives the rows ids and wires them together, as the import does when it
// writes them: a node's parent, a line's two ends, a site's node.
func store(t *testing.T, rows Rows) (domain.Feeder, []domain.FeederNode, []domain.FeederLine, []domain.Site) {
	t.Helper()
	feeder := rows.Feeder
	feeder.ID = uuid.New()
	nodes := append([]domain.FeederNode(nil), rows.Nodes...)
	for i := range nodes {
		nodes[i].ID, nodes[i].FeederID = uuid.New(), feeder.ID
	}
	lines := append([]domain.FeederLine(nil), rows.Lines...)
	for i := range nodes {
		if rows.Parent[i] < 0 {
			continue
		}
		parent := nodes[rows.Parent[i]].ID
		nodes[i].ParentNodeID = &parent
		lines[i-1].ID, lines[i-1].FeederID = uuid.New(), feeder.ID
		lines[i-1].FromNodeID, lines[i-1].ToNodeID = parent, nodes[i].ID
	}
	sites := append([]domain.Site(nil), rows.Sites...)
	for i := range sites {
		nmi, err := domain.SyntheticNMI(i + 1)
		if err != nil {
			t.Fatal(err)
		}
		sites[i].ID, sites[i].FeederID, sites[i].NMI = uuid.New(), feeder.ID, nmi
		sites[i].NodeID = nodes[rows.SiteNode[i]].ID
	}
	return feeder, nodes, lines, sites
}

func TestFromNetwork(t *testing.T) {
	t.Parallel()
	net := lv10(t)
	rows, err := FromNetwork(net, "LV10", "CSIRO, CC BY-NC-SA 4.0")
	if err != nil {
		t.Fatal(err)
	}

	f := rows.Feeder
	if f.Code != "LV10" || f.Name != net.Name || f.TransformerKVA != 500 || f.NominalVoltageV != 230 ||
		f.TapPU != 1 || f.Timezone != "Australia/Sydney" || f.Attribution == "" ||
		math.Abs(f.SourceVoltageV-433/math.Sqrt(3)) > 1e-9 || f.SourceROhm != net.Source.ROhm {
		t.Errorf("feeder = %+v", f)
	}
	if len(rows.Nodes) != 223 || len(rows.Lines) != 222 || len(rows.Sites) != 94 || len(rows.Parent) != 223 || len(rows.SiteNode) != 94 {
		t.Fatalf("%d nodes, %d lines, %d sites", len(rows.Nodes), len(rows.Lines), len(rows.Sites))
	}
	if rows.Parent[0] != -1 || rows.Nodes[0].Name != "B1862" || rows.Nodes[0].GroundROhm == nil || *rows.Nodes[0].GroundROhm != 0.3 {
		t.Errorf("root = %+v, parent %d", rows.Nodes[0], rows.Parent[0])
	}

	// Lines[i] is the line into Nodes[i+1].
	for i, line := range rows.Lines {
		bus := net.Buses[i+1]
		if line.Name != bus.Line.Name || len(line.ROhm) != 16 || line.ROhm[5] != bus.Line.ROhm[1][1] ||
			line.XOhm[14] != bus.Line.XOhm[3][2] || math.Abs(line.LengthM-bus.Line.LengthKm*1000) > 1e-9 {
			t.Fatalf("line %d = %+v", i, line)
		}
		rated := line.AmpacityA != nil && line.AmpacitySource != nil && *line.AmpacitySource == domain.AmpacityAssumed
		if line.IsSwitch == rated {
			t.Errorf("line %s: switch=%v, rated=%v; a switch is unrated and a cable is rated", line.Name, line.IsSwitch, rated)
		}
	}
	phases := map[int16]int{}
	for _, s := range rows.Sites {
		phases[s.Phase]++
	}
	if phases[1] != 32 || phases[2] != 31 || phases[3] != 31 {
		t.Errorf("sites per phase = %v", phases)
	}

	bad := *net
	bad.Buses = nil
	if _, err := FromNetwork(&bad, "X", "x"); !errors.Is(err, engine.ErrInvalidNetwork) {
		t.Errorf("an invalid network: %v", err)
	}
}

// A network written to the schema and read back solves to the same voltages,
// bus for bus, although the buses come back in another order.
func TestRoundTripSolvesTheSame(t *testing.T) {
	t.Parallel()
	original := lv10(t)
	rows, err := FromNetwork(original, "LV10", "test")
	if err != nil {
		t.Fatal(err)
	}
	feeder, nodes, lines, sites := store(t, rows)
	// Hand the rows over in reverse: the order they arrive in must not matter.
	reverse(nodes)
	reverse(lines)
	reverse(sites)

	rebuilt, order, err := ToNetwork(feeder, nodes, lines, sites)
	if err != nil {
		t.Fatal(err)
	}
	ids := order.Sites
	if len(rebuilt.Buses) != 223 || len(rebuilt.Sites) != 94 || len(ids) != 94 || rebuilt.Buses[0].Name != "B1862" {
		t.Fatalf("rebuilt: %d buses, %d sites, root %s", len(rebuilt.Buses), len(rebuilt.Sites), rebuilt.Buses[0].Name)
	}
	// Each bus comes back with the id of its node, and of the line into it:
	// none for the root.
	nodeName, lineName := map[uuid.UUID]string{}, map[uuid.UUID]string{}
	for _, n := range nodes {
		nodeName[n.ID] = n.Name
	}
	for _, l := range lines {
		lineName[l.ID] = l.Name
	}
	if len(order.Nodes) != 223 || len(order.Lines) != 223 || order.Lines[0] != uuid.Nil {
		t.Fatalf("order: %d nodes, %d lines, the root's line %s", len(order.Nodes), len(order.Lines), order.Lines[0])
	}
	for i, bus := range rebuilt.Buses {
		if nodeName[order.Nodes[i]] != bus.Name || (i > 0 && lineName[order.Lines[i]] != bus.Line.Name) {
			t.Fatalf("bus %d is %s, with the ids of node %s and line %s", i, bus.Name, nodeName[order.Nodes[i]], lineName[order.Lines[i]])
		}
	}
	// Sites come back in NMI order, with the id of each.
	byID := map[uuid.UUID]domain.Site{}
	for _, s := range sites {
		byID[s.ID] = s
	}
	for i, s := range rebuilt.Sites {
		if byID[ids[i]].NMI != s.Name || (i > 0 && rebuilt.Sites[i-1].Name >= s.Name) {
			t.Fatalf("site %d = %s, id of %s", i, s.Name, byID[ids[i]].NMI)
		}
	}

	// Each customer draws 2 kW plus 100 W for every letter of its bus name,
	// so the loads differ and are tied to a place, not to an index.
	loadAt := func(bus string) complex128 { return complex(2000+100*float64(len(bus)), 300) }
	solve := func(net *engine.Network) map[string][engine.Conductors]complex128 {
		pf, err := engine.NewPowerFlow(net, engine.Tree)
		if err != nil {
			t.Fatal(err)
		}
		load := make([]complex128, len(net.Sites))
		for i, s := range net.Sites {
			load[i] = loadAt(net.Buses[s.Bus].Name)
		}
		var sol engine.Solution
		if err := pf.Solve(load, &sol); err != nil {
			t.Fatal(err)
		}
		out := map[string][engine.Conductors]complex128{}
		for i, b := range net.Buses {
			out[b.Name] = sol.V[i]
		}
		return out
	}
	want, got := solve(original), solve(rebuilt)
	worst := 0.0
	for name, v := range want {
		for c := range engine.Conductors {
			worst = math.Max(worst, cmplx.Abs(got[name][c]-v[c]))
		}
	}
	if worst > 1e-8 {
		t.Errorf("the round trip moves a voltage by %.2e V", worst)
	}
}

func reverse[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

func TestToNetworkAppliesTheTap(t *testing.T) {
	t.Parallel()
	rows, err := FromNetwork(lv10(t), "LV10", "test")
	if err != nil {
		t.Fatal(err)
	}
	feeder, nodes, lines, sites := store(t, rows)
	feeder.TapPU = 0.975
	net, _, err := ToNetwork(feeder, nodes, lines, sites)
	if err != nil {
		t.Fatal(err)
	}
	if want := 0.975 * 433 / math.Sqrt(3); math.Abs(net.Source.VoltageV-want) > 1e-9 {
		t.Errorf("source voltage = %v, want %v", net.Source.VoltageV, want)
	}
}

func TestToNetworkErrors(t *testing.T) {
	t.Parallel()
	rows, err := FromNetwork(lv10(t), "LV10", "test")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		change func(f *domain.Feeder, nodes *[]domain.FeederNode, lines *[]domain.FeederLine, sites *[]domain.Site)
		want   string
	}{
		{"no root", func(_ *domain.Feeder, n *[]domain.FeederNode, _ *[]domain.FeederLine, _ *[]domain.Site) {
			(*n)[0].ParentNodeID = &(*n)[1].ID
		}, "has 0 root nodes, want 1"},
		{"two roots", func(_ *domain.Feeder, n *[]domain.FeederNode, _ *[]domain.FeederLine, _ *[]domain.Site) {
			(*n)[5].ParentNodeID = nil
		}, "has 2 root nodes, want 1"},
		{"a node with no line", func(_ *domain.Feeder, _ *[]domain.FeederNode, l *[]domain.FeederLine, _ *[]domain.Site) {
			*l = (*l)[1:]
		}, "has no line from its parent"},
		{"an island", func(_ *domain.Feeder, n *[]domain.FeederNode, _ *[]domain.FeederLine, _ *[]domain.Site) {
			stranger := uuid.New()
			(*n)[7].ParentNodeID = &stranger
		}, "nodes are not reachable from the root"},
		{"a short resistance matrix", func(_ *domain.Feeder, _ *[]domain.FeederNode, l *[]domain.FeederLine, _ *[]domain.Site) {
			(*l)[0].ROhm = []float64{1, 2, 3}
		}, ".r_ohm has 3 values, want 16"},
		{"a short reactance matrix", func(_ *domain.Feeder, _ *[]domain.FeederNode, l *[]domain.FeederLine, _ *[]domain.Site) {
			(*l)[0].XOhm = nil
		}, ".x_ohm has 0 values, want 16"},
		{"a short susceptance matrix", func(_ *domain.Feeder, _ *[]domain.FeederNode, l *[]domain.FeederLine, _ *[]domain.Site) {
			(*l)[0].BS = make([]float64, 17)
		}, ".b_s has 17 values, want 16"},
		{"a site on an unknown node", func(_ *domain.Feeder, _ *[]domain.FeederNode, _ *[]domain.FeederLine, s *[]domain.Site) {
			(*s)[3].NodeID = uuid.New()
		}, "is on a node that is not in the feeder"},
		{"a site on phase 4", func(_ *domain.Feeder, _ *[]domain.FeederNode, _ *[]domain.FeederLine, s *[]domain.Site) {
			(*s)[3].Phase = 4
		}, "want 1 to 3"},
		{"an ideal source", func(f *domain.Feeder, _ *[]domain.FeederNode, _ *[]domain.FeederLine, _ *[]domain.Site) {
			f.SourceROhm, f.SourceXOhm = 0, 0
		}, "source impedance must not be zero"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			feeder, nodes, lines, sites := store(t, rows)
			tt.change(&feeder, &nodes, &lines, &sites)
			_, _, err := ToNetwork(feeder, nodes, lines, sites)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one containing %q", err, tt.want)
			}
		})
	}
}
