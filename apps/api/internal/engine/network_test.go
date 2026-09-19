package engine

import (
	"errors"
	"strings"
	"testing"
)

// threeBus is the smallest feeder that exercises a branch: a transformer bus,
// a mid bus and one customer bus.
func threeBus() *Network {
	line := func(name string, r, x float64) *Line {
		l := &Line{Name: name, Linecode: "test", LengthKm: 0.1}
		for i := range Conductors {
			l.ROhm[i][i] = r
			l.XOhm[i][i] = x
		}
		return l
	}
	return &Network{
		Name:   "three-bus",
		Source: Source{KVA: 100, VoltageV: 240, ROhm: 0.01, XOhm: 0.02},
		Buses: []Bus{
			{Name: "tx", Parent: -1, Ground: &Impedance{ROhm: 0.5}},
			{Name: "mid", Parent: 0, Line: line("main", 0.05, 0.02)},
			{Name: "house", Parent: 1, Line: line("service", 0.10, 0.01), Ground: &Impedance{ROhm: 10}},
		},
		Sites: []Site{{Name: "house-a", Bus: 2, Phase: 1}},
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	if err := threeBus().Validate(); err != nil {
		t.Fatalf("valid network: %v", err)
	}

	tests := []struct {
		name   string
		change func(n *Network)
		want   string
	}{
		{name: "no source voltage", change: func(n *Network) { n.Source.VoltageV = 0 }, want: "source voltage and rating must be positive"},
		{name: "no rating", change: func(n *Network) { n.Source.KVA = 0 }, want: "source voltage and rating must be positive"},
		{name: "ideal source", change: func(n *Network) { n.Source.ROhm, n.Source.XOhm = 0, 0 }, want: "source impedance must not be zero"},
		{name: "no buses", change: func(n *Network) { n.Buses = nil }, want: "no buses"},
		{name: "root has a parent", change: func(n *Network) { n.Buses[0].Parent = 1 }, want: "bus 0 (tx) must be the root"},
		{name: "root has a line", change: func(n *Network) { n.Buses[0].Line = n.Buses[1].Line }, want: "bus 0 (tx) must be the root"},
		{name: "parent after child", change: func(n *Network) { n.Buses[1].Parent = 2 }, want: "bus 1 (mid) has parent 2, want an earlier bus"},
		{name: "second root", change: func(n *Network) { n.Buses[2].Parent = -1 }, want: "bus 2 (house) has parent -1"},
		{name: "missing line", change: func(n *Network) { n.Buses[2].Line = nil }, want: "bus 2 (house) has no line to its parent"},
		{name: "site on a missing bus", change: func(n *Network) { n.Sites[0].Bus = 3 }, want: "site house-a is on bus 3, which does not exist"},
		{name: "site on a negative bus", change: func(n *Network) { n.Sites[0].Bus = -1 }, want: "which does not exist"},
		{name: "site on the neutral", change: func(n *Network) { n.Sites[0].Phase = 4 }, want: "site house-a is on phase 4, want 1 to 3"},
		{name: "site on phase zero", change: func(n *Network) { n.Sites[0].Phase = 0 }, want: "want 1 to 3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			n := threeBus()
			tt.change(n)
			err := n.Validate()
			if !errors.Is(err, ErrInvalidNetwork) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want ErrInvalidNetwork containing %q", err, tt.want)
			}
		})
	}
}
