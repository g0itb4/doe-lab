package dss

import (
	"strings"
)

// Circuit is a parsed OpenDSS model, with lengths in kilometres and linecode
// values per kilometre.
type Circuit struct {
	Name   string
	Source Source
	// Linecodes is keyed by lower-cased name. A later definition of a name
	// replaces an earlier one.
	Linecodes    map[string]Linecode
	Lines        []Line
	Loads        []Load
	Transformers []Transformer
	Reactors     []Reactor
}

// Source is the circuit's voltage source.
type Source struct {
	Bus      string
	BaseKV   float64 // line to line
	PU       float64
	AngleDeg float64 // of phase 1
}

// Linecode is the impedance of a cable or line type, per kilometre.
type Linecode struct {
	Name       string
	Conductors int
	R          [][]float64 // ohm/km
	X          [][]float64 // ohm/km
	C          [][]float64 // nF/km
}

// Line is a line or a switch between two buses.
type Line struct {
	Name       string
	Linecode   string
	Bus1, Bus2 BusRef
	LengthKm   float64
	Conductors int
	// Switch marks a closed switch. OpenDSS replaces the impedance of a
	// switch with a short fixed one, whatever its linecode says.
	Switch bool
}

// Load is a customer connection.
type Load struct {
	Name   string
	Bus    BusRef
	Phases int
	KV     float64
}

// Transformer is a multi-winding transformer.
type Transformer struct {
	Name       string
	XHLPercent float64
	// Lead is true for a European Dy11 connection (`leadlag=euro` or
	// `lead`): the low-voltage side leads by 30 degrees.
	Lead     bool
	Windings []Winding
}

// Winding is one winding of a transformer.
type Winding struct {
	Bus      BusRef
	Delta    bool
	KV       float64
	KVA      float64
	RPercent float64
	Tap      float64
}

// Reactor is a series impedance between two bus references. The feeders use it
// to earth the neutral.
type Reactor struct {
	Name       string
	Bus1, Bus2 BusRef
	R, X       float64 // ohm
}

// Interpret builds a circuit from parsed commands. `redirect` commands must be
// expanded first; Read does that.
func Interpret(cmds []Command) (*Circuit, error) {
	c := &Circuit{Linecodes: map[string]Linecode{}}
	for _, cmd := range cmds {
		var err error
		switch cmd.Verb {
		case "new":
			err = c.newElement(cmd)
		case "set":
			err = checkFrequency(cmd)
		case "clear", "solve", "export", "batchedit":
			// Solution control. The engine has its own.
		default:
			err = cmd.errorf("unsupported command %q", cmd.Verb)
		}
		if err != nil {
			return nil, err
		}
	}
	return c, nil
}

// checkFrequency rejects a base frequency other than 50 Hz. The engine turns
// capacitance into susceptance at 50 Hz and takes reactances as given.
func checkFrequency(cmd Command) error {
	p := newProps(cmd, "set", cmd.Args)
	for key := range p.vals {
		if strings.Contains(key, "frequency") && p.float(key, 0) != 50 {
			p.fail("%s=%s: only 50 Hz is supported", key, p.vals[key])
		}
		p.ignore(key)
	}
	return p.done()
}

func (c *Circuit) newElement(cmd Command) error {
	if len(cmd.Args) == 0 {
		return cmd.errorf("new: missing element")
	}
	what := cmd.Args[0].Value
	class, name, ok := strings.Cut(what, ".")
	if cmd.Args[0].Key != "" || !ok || name == "" {
		return cmd.errorf("new: want class.name, got %q", what)
	}
	p := newProps(cmd, what, cmd.Args[1:])
	switch strings.ToLower(class) {
	case "circuit":
		c.Name = name
		c.Source = Source{
			Bus:      p.str("bus1", "sourcebus"),
			BaseKV:   p.needFloat("basekv"),
			PU:       p.float("pu", 1),
			AngleDeg: p.float("angle", 0),
		}
		p.ignore("phases", "model")
	case "linecode":
		c.Linecodes[strings.ToLower(name)] = linecode(p, name)
	case "line":
		c.Lines = append(c.Lines, line(p, name))
	case "load":
		c.Loads = append(c.Loads, Load{
			Name:   name,
			Phases: p.needInt("phases"),
			Bus:    p.bus("bus1"),
			KV:     p.needFloat("kv"),
		})
		// Power comes from the profiles, not from the script. The voltage
		// limits only choose where OpenDSS switches load model.
		p.ignore("kw", "kvar", "pf", "vminpu", "vmaxpu")
	case "transformer":
		c.Transformers = append(c.Transformers, transformer(p, name, cmd.Args[1:]))
	case "reactor":
		c.Reactors = append(c.Reactors, Reactor{
			Name: name,
			Bus1: p.bus("bus1"),
			Bus2: p.bus("bus2"),
			R:    p.needFloat("r"),
			X:    p.float("x", 0),
		})
		p.ignore("phases")
	default:
		p.fail("unsupported element class")
	}
	return p.done()
}

func linecode(p *props, name string) Linecode {
	n := p.needInt("nphases")
	km := p.unit("units")
	lc := Linecode{
		Name:       name,
		Conductors: n,
		R:          p.matrix("rmatrix", n),
		X:          p.matrix("xmatrix", n),
		C:          p.matrix("cmatrix", n),
	}
	// Values are per unit length: ohm per kft becomes ohm per km.
	for _, m := range [][][]float64{lc.R, lc.X, lc.C} {
		for i := range m {
			for j := range m[i] {
				m[i][j] /= km
			}
		}
	}
	return lc
}

func line(p *props, name string) Line {
	l := Line{
		Name:       name,
		Linecode:   strings.ToLower(p.need("linecode")),
		Conductors: p.needInt("phases"),
		Switch:     p.yes("switch"),
	}
	l.LengthKm = p.needFloat("length") * p.unit("units")
	nodes := make([]int, l.Conductors)
	for i := range nodes {
		nodes[i] = i + 1
	}
	l.Bus1 = p.bus("bus1", nodes...)
	l.Bus2 = p.bus("bus2", nodes...)
	return l
}

// transformer reads the winding properties in order: `wdg=N` selects the
// winding that the following bus, conn, kv, tap, %r and kva belong to. Before
// any `wdg`, the properties belong to winding 1.
func transformer(p *props, name string, args []Arg) Transformer {
	t := Transformer{Name: name, XHLPercent: p.needFloat("xhl")}
	leadlag := strings.ToLower(p.str("leadlag", "lag"))
	t.Lead = leadlag == "euro" || leadlag == "lead"
	p.ignore("wdg", "bus", "conn", "kv", "tap", "%r", "kva", "phases", "windings")

	n := 1
	winding := func() *Winding {
		for len(t.Windings) < n {
			t.Windings = append(t.Windings, Winding{Tap: 1})
		}
		return &t.Windings[n-1]
	}
	for _, a := range args {
		switch a.Key {
		case "wdg":
			if n = p.parseInt("wdg", a.Value); n < 1 {
				p.fail("wdg=%s: windings count from 1", a.Value)
				n = 1
			}
		case "bus":
			// A wye winding that names no nodes has its star point on earth:
			// nodes 1, 2, 3 and 0.
			winding().Bus = p.busValue("bus", a.Value, 1, 2, 3, 0)
		case "conn":
			winding().Delta = strings.HasPrefix(strings.ToLower(a.Value), "d")
		case "kv":
			winding().KV = p.parseFloat("kv", a.Value)
		case "tap":
			winding().Tap = p.parseFloat("tap", a.Value)
		case "%r":
			winding().RPercent = p.parseFloat("%r", a.Value)
		case "kva":
			winding().KVA = p.parseFloat("kva", a.Value)
		}
	}
	return t
}
