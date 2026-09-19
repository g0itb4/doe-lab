package dss

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

func TestParse(t *testing.T) {
	t.Parallel()

	const script = `
// a line comment
! another comment
New Circuit.demo  basekv=11.0 Bus1=B1 ! trailing comment
set defaultbasefrequency=50

New Linecode.cable/4w nphases=2  Units=kft
~ Rmatrix=[0.1  |0.01  0.1  ]
more Xmatrix=(0.2 | 0.02 0.2) note="a ! inside quotes" tag='x y'
! new line.Switch_OPEN bus1=B1 bus2=B9 switch=y
new line.L1 lineCode=cable/4w, bus1=B1	bus2=B2 length=
solve
`
	got, err := Parse("demo.dss", strings.NewReader(script))
	if err != nil {
		t.Fatal(err)
	}
	want := []Command{
		{Verb: "new", File: "demo.dss", Line: 4, Args: []Arg{
			{Value: "Circuit.demo"}, {Key: "basekv", Value: "11.0"}, {Key: "bus1", Value: "B1"},
		}},
		{Verb: "set", File: "demo.dss", Line: 5, Args: []Arg{{Key: "defaultbasefrequency", Value: "50"}}},
		{Verb: "new", File: "demo.dss", Line: 7, Args: []Arg{
			{Value: "Linecode.cable/4w"},
			{Key: "nphases", Value: "2"},
			{Key: "units", Value: "kft"},
			{Key: "rmatrix", Value: "0.1  |0.01  0.1"},
			{Key: "xmatrix", Value: "0.2 | 0.02 0.2"},
			{Key: "note", Value: "a ! inside quotes"},
			{Key: "tag", Value: "x y"},
		}},
		// The commented-out switch on line 10 produces nothing.
		{Verb: "new", File: "demo.dss", Line: 11, Args: []Arg{
			{Value: "line.L1"},
			{Key: "linecode", Value: "cable/4w"},
			{Key: "bus1", Value: "B1"},
			{Key: "bus2", Value: "B2"},
			{Key: "length", Value: ""},
		}},
		{Verb: "solve", File: "demo.dss", Line: 12},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse:\n got %+v\nwant %+v", got, want)
	}
}

func TestParseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, script, want string
	}{
		{name: "orphan continuation", script: "~ kv=1", want: "x.dss:1: continuation line with no command"},
		{name: "unclosed bracket", script: "new a.b\n~ m=[1 2", want: `x.dss:2: m: missing closing "]"`},
		{name: "unclosed quote", script: `new a.b n="abc`, want: `x.dss:1: n: missing closing "\""`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse("x.dss", strings.NewReader(tt.script))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one containing %q", err, tt.want)
			}
		})
	}

	t.Run("read failure", func(t *testing.T) {
		t.Parallel()
		_, err := Parse("x.dss", iotest.ErrReader(errors.New("disk gone")))
		if err == nil || !strings.Contains(err.Error(), "x.dss: disk gone") {
			t.Errorf("error = %v", err)
		}
	})
}
