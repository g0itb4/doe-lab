package dss

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"doelab/api/internal/engine"
)

// omega is the angular frequency at 50 Hz.
const omega = 2 * math.Pi * 50

// switchOhm is the resistance and the reactance of each conductor of a closed
// switch. OpenDSS gives a line marked `switch=y` an impedance of 1 + j1 ohm per
// unit length in both sequences and a length of 0.001, whatever its linecode
// says: 1 milliohm of each, with no coupling between conductors.
const switchOhm = 0.001

// Network converts the circuit to the engine's model of a radial feeder below
// one delta-wye transformer. It fails on anything the engine does not model,
// and on a feeder that is not a tree.
func (c *Circuit) Network() (*engine.Network, error) {
	net := &engine.Network{Name: c.Name}
	root, err := c.source(net)
	if err != nil {
		return nil, err
	}
	index, err := c.tree(net, root)
	if err != nil {
		return nil, err
	}

	for _, r := range c.Reactors {
		i, ok := index[strings.ToLower(r.Bus1.Bus)]
		neutralToEarth := slices.Equal(r.Bus1.Nodes, []int{engine.Neutral + 1}) && slices.Equal(r.Bus2.Nodes, []int{0}) &&
			strings.EqualFold(r.Bus1.Bus, r.Bus2.Bus)
		if !ok || !neutralToEarth || net.Buses[i].Ground != nil {
			return nil, fmt.Errorf("reactor %s: want one neutral-to-earth reactor on a bus of the feeder", r.Name)
		}
		net.Buses[i].Ground = &engine.Impedance{ROhm: r.R, XOhm: r.X}
	}

	for _, l := range c.Loads {
		i, ok := index[strings.ToLower(l.Bus.Bus)]
		singlePhase := l.Phases == 1 && len(l.Bus.Nodes) == 2 &&
			l.Bus.Nodes[0] >= 1 && l.Bus.Nodes[0] <= 3 && l.Bus.Nodes[1] == engine.Neutral+1
		if !ok || !singlePhase {
			return nil, fmt.Errorf("load %s: want a single-phase load from a phase to the neutral of a bus of the feeder", l.Name)
		}
		net.Sites = append(net.Sites, engine.Site{Name: l.Name, Bus: i, Phase: l.Bus.Nodes[0]})
	}
	return net, nil
}

// source fills in the transformer equivalent and returns the name of its
// low-voltage bus.
func (c *Circuit) source(net *engine.Network) (string, error) {
	if len(c.Transformers) != 1 || len(c.Transformers[0].Windings) != 2 {
		return "", fmt.Errorf("circuit %s: want exactly one two-winding transformer", c.Name)
	}
	t := c.Transformers[0]
	hv, lv := t.Windings[0], t.Windings[1]
	if !hv.Delta || lv.Delta || !strings.EqualFold(hv.Bus.Bus, c.Source.Bus) || !slices.Equal(lv.Bus.Nodes, []int{1, 2, 3, 0}) {
		return "", fmt.Errorf("transformer %s: want a delta winding on the source bus and an earthed wye winding", t.Name)
	}
	if hv.KV <= 0 || lv.KV <= 0 || lv.KVA <= 0 || hv.Tap <= 0 {
		return "", fmt.Errorf("transformer %s: kv, kva and tap must be positive", t.Name)
	}

	// The delta winding sees the source's line-to-line voltage. Each wye
	// winding is rated at kv/√3.
	ratio := (lv.KV * lv.Tap / math.Sqrt(3)) / (hv.KV * hv.Tap)
	// Per-unit impedances are on the transformer's own base.
	zBase := lv.KV * lv.KV * 1000 / lv.KVA
	shift := -30.0
	if t.Lead {
		shift = 30
	}
	net.Source = engine.Source{
		KVA:      lv.KVA,
		VoltageV: c.Source.BaseKV * c.Source.PU * 1000 * ratio,
		AngleDeg: c.Source.AngleDeg + shift,
		ROhm:     (hv.RPercent + lv.RPercent) / 100 * zBase,
		XOhm:     t.XHLPercent / 100 * zBase,
	}
	return lv.Bus.Bus, nil
}

// tree orders the buses breadth-first from root and attaches each line to the
// bus at its far end. It returns the index of each bus by lower-cased name.
func (c *Circuit) tree(net *engine.Network, root string) (map[string]int, error) {
	type edge struct {
		line  *engine.Line
		other string // the bus at the other end, as written
	}
	adjacent := map[string][]edge{}
	for _, l := range c.Lines {
		line, err := c.line(l)
		if err != nil {
			return nil, err
		}
		a, b := strings.ToLower(l.Bus1.Bus), strings.ToLower(l.Bus2.Bus)
		adjacent[a] = append(adjacent[a], edge{line: line, other: l.Bus2.Bus})
		adjacent[b] = append(adjacent[b], edge{line: line, other: l.Bus1.Bus})
	}

	index := map[string]int{strings.ToLower(root): 0}
	net.Buses = []engine.Bus{{Name: root, Parent: -1}}
	for i := 0; i < len(net.Buses); i++ {
		for _, e := range adjacent[strings.ToLower(net.Buses[i].Name)] {
			if _, seen := index[strings.ToLower(e.other)]; seen {
				continue
			}
			index[strings.ToLower(e.other)] = len(net.Buses)
			net.Buses = append(net.Buses, engine.Bus{Name: e.other, Parent: i, Line: e.line})
		}
	}

	// A tree with n buses has n-1 lines. Fewer buses reached than named
	// means an island; more lines than that means a loop.
	named := len(adjacent)
	if _, ok := adjacent[strings.ToLower(root)]; !ok {
		named++ // the root, when no line touches it
	}
	if len(net.Buses) < named {
		return nil, fmt.Errorf("circuit %s: %d of %d buses are not connected to %s",
			c.Name, named-len(net.Buses), named, root)
	}
	if len(c.Lines) != len(net.Buses)-1 {
		return nil, fmt.Errorf("circuit %s: %d lines join %d buses, so the feeder has a loop",
			c.Name, len(c.Lines), len(net.Buses))
	}
	return index, nil
}

// line computes the impedance of one line over its whole length.
func (c *Circuit) line(l Line) (*engine.Line, error) {
	full := []int{1, 2, 3, 4}
	if l.Conductors != engine.Conductors || !slices.Equal(l.Bus1.Nodes, full) || !slices.Equal(l.Bus2.Nodes, full) {
		return nil, fmt.Errorf("line %s: want a 4-conductor line on nodes 1.2.3.4", l.Name)
	}
	line := &engine.Line{Name: l.Name, Linecode: l.Linecode, LengthKm: l.LengthKm, Switch: l.Switch}
	if l.Switch {
		for i := range engine.Conductors {
			line.ROhm[i][i] = switchOhm
			line.XOhm[i][i] = switchOhm
		}
		return line, nil
	}
	lc, ok := c.Linecodes[l.Linecode]
	if !ok || lc.Conductors != engine.Conductors {
		return nil, fmt.Errorf("line %s: no 4-conductor linecode %q", l.Name, l.Linecode)
	}
	// Zero when the cable type has no assumed rating.
	line.AmpacityA, _ = engine.AssumedAmpacity(l.Linecode)
	for i := range engine.Conductors {
		for j := range engine.Conductors {
			line.ROhm[i][j] = lc.R[i][j] * l.LengthKm
			line.XOhm[i][j] = lc.X[i][j] * l.LengthKm
			// Capacitance is in nF per km.
			line.BS[i][j] = omega * lc.C[i][j] * 1e-9 * l.LengthKm
		}
	}
	return line, nil
}
