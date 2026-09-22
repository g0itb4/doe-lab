// Package topology converts a feeder between the engine's network model and
// the rows of the schema: feeders, feeder_nodes, feeder_lines and sites.
//
// The import writes a network into the database with FromNetwork; the engine
// command rebuilds the network from what the API returns with ToNetwork. The
// functions are pure, and a round trip gives a network that solves to the
// same voltages.
package topology

import (
	"fmt"
	"sort"

	"github.com/google/uuid"

	"doelab/api/internal/domain"
	"doelab/api/internal/engine"
)

// Rows is a feeder as the schema holds it. The slices of nodes and lines are
// in the engine's bus order: a node comes after its parent. ParentNodeID,
// FromNodeID and ToNodeID are not set by FromNetwork, because the ids do not
// exist until the rows are stored; Parent gives the index of each node's
// parent instead.
type Rows struct {
	Feeder domain.Feeder
	Nodes  []domain.FeederNode
	// Parent[i] is the index in Nodes of node i's parent, or -1 for the root.
	Parent []int
	// Lines[i] is the line into Nodes[i+1]: every node but the root has one.
	Lines []domain.FeederLine
	// Sites[i] is the site of the network's site i; SiteNode[i] is the index
	// in Nodes of its node.
	Sites    []domain.Site
	SiteNode []int
}

// flatten turns an engine matrix into the 16 values of a column.
func flatten(m engine.Matrix) []float64 {
	out := make([]float64, 0, engine.Conductors*engine.Conductors)
	for _, row := range m {
		out = append(out, row[:]...)
	}
	return out
}

// FromNetwork lays a network out as rows. code and attribution describe the
// feeder; the network's own name becomes the feeder's display name.
func FromNetwork(net *engine.Network, code, attribution string) (Rows, error) {
	if err := net.Validate(); err != nil {
		return Rows{}, err
	}
	rows := Rows{
		Feeder: domain.Feeder{
			Code: code, Name: net.Name,
			NominalVoltageV: engine.NominalVoltage, TransformerKVA: net.Source.KVA,
			SourceVoltageV: net.Source.VoltageV, SourceAngleDeg: net.Source.AngleDeg,
			SourceROhm: net.Source.ROhm, SourceXOhm: net.Source.XOhm,
			TapPU: 1, Timezone: "Australia/Sydney", Attribution: attribution,
		},
	}
	for _, bus := range net.Buses {
		node := domain.FeederNode{Name: bus.Name}
		if bus.Ground != nil {
			r, x := bus.Ground.ROhm, bus.Ground.XOhm
			node.GroundROhm, node.GroundXOhm = &r, &x
		}
		rows.Nodes = append(rows.Nodes, node)
		rows.Parent = append(rows.Parent, bus.Parent)
		if bus.Line == nil {
			continue
		}
		line := domain.FeederLine{
			Name: bus.Line.Name, Linecode: bus.Line.Linecode, LengthM: bus.Line.LengthKm * 1000,
			IsSwitch: bus.Line.Switch,
			ROhm:     flatten(bus.Line.ROhm), XOhm: flatten(bus.Line.XOhm), BS: flatten(bus.Line.BS),
		}
		if bus.Line.AmpacityA > 0 {
			ampacity, source := bus.Line.AmpacityA, domain.AmpacityAssumed
			line.AmpacityA, line.AmpacitySource = &ampacity, &source
		}
		rows.Lines = append(rows.Lines, line)
	}
	for _, site := range net.Sites {
		rows.Sites = append(rows.Sites, domain.Site{Name: site.Name, Phase: int16(site.Phase)}) //nolint:gosec // G115: Validate checked 1 to 3
		rows.SiteNode = append(rows.SiteNode, site.Bus)
	}
	return rows, nil
}

// unflatten turns the 16 values of a column back into a matrix.
func unflatten(what string, values []float64) (engine.Matrix, error) {
	var m engine.Matrix
	if len(values) != engine.Conductors*engine.Conductors {
		return m, fmt.Errorf("%s has %d values, want 16", what, len(values))
	}
	for i, v := range values {
		m[i/engine.Conductors][i%engine.Conductors] = v
	}
	return m, nil
}

// ToNetwork rebuilds the engine's network from stored rows. The rows may come
// in any order; buses are ordered breadth-first from the root, and the
// children of a bus by name, so the result is the same whatever order the
// rows arrived in.
//
// The second result gives, for each site of the network, the id of its row:
// the engine works by index, the API by id. Sites are in NMI order.
func ToNetwork(feeder domain.Feeder, nodes []domain.FeederNode, lines []domain.FeederLine, sites []domain.Site) (*engine.Network, []uuid.UUID, error) {
	net := &engine.Network{
		Name: feeder.Name,
		Source: engine.Source{
			KVA: feeder.TransformerKVA,
			// The tap is applied here: the engine sees the voltage the
			// transformer is set to.
			VoltageV: feeder.SourceVoltageV * feeder.TapPU,
			AngleDeg: feeder.SourceAngleDeg, ROhm: feeder.SourceROhm, XOhm: feeder.SourceXOhm,
		},
	}

	lineInto := map[uuid.UUID]domain.FeederLine{}
	for _, l := range lines {
		lineInto[l.ToNodeID] = l
	}
	children := map[uuid.UUID][]domain.FeederNode{}
	var roots []domain.FeederNode
	for _, n := range nodes {
		if n.ParentNodeID == nil {
			roots = append(roots, n)
			continue
		}
		children[*n.ParentNodeID] = append(children[*n.ParentNodeID], n)
	}
	if len(roots) != 1 {
		return nil, nil, fmt.Errorf("feeder %s has %d root nodes, want 1", feeder.Code, len(roots))
	}

	index := map[uuid.UUID]int{}
	queue := []domain.FeederNode{roots[0]}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		bus := engine.Bus{Name: node.Name, Parent: -1}
		if node.GroundROhm != nil && node.GroundXOhm != nil {
			bus.Ground = &engine.Impedance{ROhm: *node.GroundROhm, XOhm: *node.GroundXOhm}
		}
		if node.ParentNodeID != nil {
			bus.Parent = index[*node.ParentNodeID]
			l, ok := lineInto[node.ID]
			if !ok {
				return nil, nil, fmt.Errorf("node %s has no line from its parent", node.Name)
			}
			line := &engine.Line{Name: l.Name, Linecode: l.Linecode, LengthKm: l.LengthM / 1000, Switch: l.IsSwitch}
			var err error
			if line.ROhm, err = unflatten(l.Name+".r_ohm", l.ROhm); err != nil {
				return nil, nil, err
			}
			if line.XOhm, err = unflatten(l.Name+".x_ohm", l.XOhm); err != nil {
				return nil, nil, err
			}
			if line.BS, err = unflatten(l.Name+".b_s", l.BS); err != nil {
				return nil, nil, err
			}
			if l.AmpacityA != nil {
				line.AmpacityA = *l.AmpacityA
			}
			bus.Line = line
		}
		index[node.ID] = len(net.Buses)
		net.Buses = append(net.Buses, bus)

		next := children[node.ID]
		sort.Slice(next, func(i, j int) bool { return next[i].Name < next[j].Name })
		queue = append(queue, next...)
	}
	if len(net.Buses) != len(nodes) {
		return nil, nil, fmt.Errorf("feeder %s: %d of %d nodes are not reachable from the root", feeder.Code, len(nodes)-len(net.Buses), len(nodes))
	}

	ordered := append([]domain.Site(nil), sites...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].NMI < ordered[j].NMI })
	ids := make([]uuid.UUID, len(ordered))
	for i, s := range ordered {
		bus, ok := index[s.NodeID]
		if !ok {
			return nil, nil, fmt.Errorf("site %s is on a node that is not in the feeder", s.NMI)
		}
		net.Sites = append(net.Sites, engine.Site{Name: s.NMI, Bus: bus, Phase: int(s.Phase)})
		ids[i] = s.ID
	}
	if err := net.Validate(); err != nil {
		return nil, nil, err
	}
	return net, ids, nil
}
