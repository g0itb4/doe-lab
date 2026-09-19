package dss

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// props reads the key=value arguments of one element. It keeps the first
// error, so a caller reads every property and checks once at the end.
type props struct {
	cmd  Command
	what string // "line.L_2", for errors
	vals map[string]string
	used map[string]bool
	err  error
}

func newProps(cmd Command, what string, args []Arg) *props {
	p := &props{cmd: cmd, what: what, vals: map[string]string{}, used: map[string]bool{}}
	for _, a := range args {
		if a.Key == "" {
			p.fail("unexpected value %q with no key", a.Value)
			continue
		}
		// A repeated key keeps its last value, as in OpenDSS.
		p.vals[a.Key] = a.Value
	}
	return p
}

func (p *props) fail(format string, args ...any) {
	if p.err == nil {
		p.err = p.cmd.errorf("%s: %s", p.what, fmt.Sprintf(format, args...))
	}
}

// str returns the value of key, or def when the key is absent.
func (p *props) str(key, def string) string {
	p.used[key] = true
	if v, ok := p.vals[key]; ok {
		return v
	}
	return def
}

// need returns the value of key and fails when it is absent.
func (p *props) need(key string) string {
	p.used[key] = true
	v, ok := p.vals[key]
	if !ok {
		p.fail("missing %s", key)
	}
	return v
}

func (p *props) parseFloat(key, v string) float64 {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		p.fail("%s=%q is not a number", key, v)
	}
	return f
}

// float returns the number at key, or def when the key is absent.
func (p *props) float(key string, def float64) float64 {
	p.used[key] = true
	if v, ok := p.vals[key]; ok {
		return p.parseFloat(key, v)
	}
	return def
}

// needFloat returns the number at key and fails when it is absent.
func (p *props) needFloat(key string) float64 {
	return p.parseFloat(key, p.need(key))
}

func (p *props) parseInt(key, v string) int {
	n, err := strconv.Atoi(v)
	if err != nil {
		p.fail("%s=%q is not an integer", key, v)
	}
	return n
}

// needInt returns the integer at key and fails when it is absent.
func (p *props) needInt(key string) int {
	return p.parseInt(key, p.need(key))
}

// yes reports whether key holds a true value: OpenDSS accepts any word that
// starts with y or t.
func (p *props) yes(key string) bool {
	v := strings.ToLower(p.str(key, "n"))
	return strings.HasPrefix(v, "y") || strings.HasPrefix(v, "t")
}

// ignore marks keys that the subset accepts but does not model.
func (p *props) ignore(keys ...string) {
	for _, k := range keys {
		p.used[k] = true
	}
}

// done fails on a property that no accessor asked for, then returns the first
// error.
func (p *props) done() error {
	var unknown []string
	for k := range p.vals {
		if !p.used[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		p.fail("unsupported property %s", strings.Join(unknown, ", "))
	}
	return p.err
}

// kmPerUnit converts an OpenDSS length unit to kilometres.
var kmPerUnit = map[string]float64{
	"km":  1,
	"m":   0.001,
	"kft": 0.3048,
	"ft":  0.0003048,
	"mi":  1.609344,
}

// unit returns the kilometres per unit of the length unit at key.
func (p *props) unit(key string) float64 {
	v := strings.ToLower(p.need(key))
	km, ok := kmPerUnit[v]
	if !ok {
		p.fail("%s=%q is not a supported length unit", key, v)
		return 1
	}
	return km
}

// matrix reads a symmetric n×n matrix at key. OpenDSS writes it as rows
// separated by `|`, either in full or as the lower triangle.
func (p *props) matrix(key string, n int) [][]float64 {
	m := make([][]float64, n)
	for i := range m {
		m[i] = make([]float64, n)
	}
	rows := strings.Split(p.need(key), "|")
	if len(rows) != n {
		p.fail("%s has %d rows, want %d", key, len(rows), n)
		return m
	}
	for i, row := range rows {
		cells := strings.FieldsFunc(row, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' })
		if len(cells) != i+1 && len(cells) != n {
			p.fail("%s row %d has %d values, want %d or %d", key, i+1, len(cells), i+1, n)
			return m
		}
		for j, cell := range cells {
			v := p.parseFloat(key, cell)
			m[i][j] = v
			if j <= i {
				m[j][i] = v
			}
		}
	}
	return m
}

// BusRef is a bus name with the nodes an element connects to: `B12.1.4` is
// nodes 1 and 4 of bus B12. Node 0 is earth.
type BusRef struct {
	Bus   string
	Nodes []int
}

// bus reads a bus reference at key. When the reference names no nodes, it gets
// defaultNodes.
func (p *props) bus(key string, defaultNodes ...int) BusRef {
	return p.busValue(key, p.need(key), defaultNodes...)
}

func (p *props) busValue(key, value string, defaultNodes ...int) BusRef {
	parts := strings.Split(value, ".")
	ref := BusRef{Bus: parts[0], Nodes: defaultNodes}
	if len(parts) > 1 {
		ref.Nodes = make([]int, 0, len(parts)-1)
		for _, part := range parts[1:] {
			n, err := strconv.Atoi(part)
			if err != nil || n < 0 {
				p.fail("%s: node %q is not a node number", key, part)
			}
			ref.Nodes = append(ref.Nodes, n)
		}
	}
	return ref
}
