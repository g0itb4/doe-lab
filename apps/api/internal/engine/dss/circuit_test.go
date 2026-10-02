package dss

import (
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

// demo is a small feeder in the dialect of the CSIRO models: a delta-wye
// transformer, a main, a switch, two service cables and two customers.
var demo = fstest.MapFS{
	"Master.dss": {Data: []byte(`clear
new circuit.demo angle=-30.0 basekv=11.0 phases=3 bus1=B0 model=ideal

set defaultbasefrequency=50.0
set basefrequency=50.0

redirect parts/Linecodes.dss
redirect parts/Network.dss

batchedit load..* kw=1
solve
export Voltages
`)},
	"parts/Linecodes.dss": {Data: []byte(`
New Linecode.main nphases=4  Units=kft
~ Rmatrix=[9  |9  9  |9  9  9  |9  9  9  9  ]
~ Xmatrix=[9  |9  9  |9  9  9  |9  9  9  9  ]
~ Cmatrix=[9  |9  9  |9  9  9  |9  9  9  9  ]

! The same name again: this definition wins.
New Linecode.MAIN nphases=4  Units=kft
~ Rmatrix=[0.3048  |0.03048  0.3048  |0.03048  0.03048  0.3048  |0.03048  0.03048  0.03048  0.3048  ]
~ Xmatrix=[0.6096  |0.06096  0.6096  |0.06096  0.06096  0.6096  |0.06096  0.06096  0.06096  0.6096  ]
~ Cmatrix=[3.048  |0  3.048  |0  0  3.048  |0  0  0  3.048  ]

New Linecode.service nphases=4 units=km
~ rmatrix=[2 0 0 0 | 0 2 0 0 | 0 0 2 0 | 0 0 0 2]
~ xmatrix=[1 0 0 0 | 0 1 0 0 | 0 0 1 0 | 0 0 0 1]
~ cmatrix=[0 0 0 0 | 0 0 0 0 | 0 0 0 0 | 0 0 0 0]
`)},
	"parts/Network.dss": {Data: []byte(`
new transformer.T1 leadlag=euro xhl=4 wdg=1 bus=B0 conn=delta kv=11.0 tap=1.0 %r=0.5 kva=500 wdg=2 bus=B1 conn=wye kv=0.4 tap=1.0 %r=0.5 kva=500
new line.S1 lineCode=main bus1=b3 bus2=B4 length=0.001 units=km switch=y phases=4
! new line.S2_OPEN lineCode=main bus1=B4 bus2=B1 length=0.001 units=km switch=y phases=4
new line.L2 lineCode=service bus1=B3 bus2=B2 length=50 units=m phases=4
new line.L1 lineCode=main bus1=B1 bus2=B2 length=0.5 units=km phases=4
new load.Ld1 phases=1 bus1=B3.1.4 kv=0.23  vminpu=0.1 vmaxpu=2
new load.Ld2 phases=1 bus1=B4.3.4 kv=0.23
new reactor.g1 phases=1 bus1=B1.4 bus2=B1.0 r=0.3 x=0.0
new reactor.g3 phases=1 bus1=B3.4 bus2=B3.0 r=10
`)},
}

func TestReadCircuit(t *testing.T) {
	t.Parallel()

	c, err := Read(demo, "Master.dss")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "demo" {
		t.Errorf("Name = %q", c.Name)
	}
	if want := (Source{Bus: "B0", BaseKV: 11, PU: 1, AngleDeg: -30}); c.Source != want {
		t.Errorf("Source = %+v, want %+v", c.Source, want)
	}

	// The second definition of "main" replaced the first, and its values are
	// per km: 0.3048 ohm/kft is exactly 1 ohm/km.
	main := c.Linecodes["main"]
	if len(c.Linecodes) != 2 || main.Name != "MAIN" || main.Conductors != 4 {
		t.Fatalf("Linecodes = %+v", c.Linecodes)
	}
	approx(t, "main R11", main.R[0][0], 1)
	approx(t, "main R21", main.R[1][0], 0.1)
	approx(t, "main R12 (mirrored)", main.R[0][1], 0.1)
	approx(t, "main X44", main.X[3][3], 2)
	approx(t, "main C11", main.C[0][0], 10)
	if got := c.Linecodes["service"].R[1][1]; got != 2 {
		t.Errorf("service R22 = %v, want 2 (full matrix, already per km)", got)
	}

	wantLines := []Line{
		{
			Name: "S1", Linecode: "main", LengthKm: 0.001, Conductors: 4, Switch: true,
			Bus1: BusRef{Bus: "b3", Nodes: []int{1, 2, 3, 4}}, Bus2: BusRef{Bus: "B4", Nodes: []int{1, 2, 3, 4}},
		},
		{
			Name: "L2", Linecode: "service", LengthKm: 0.05, Conductors: 4,
			Bus1: BusRef{Bus: "B3", Nodes: []int{1, 2, 3, 4}}, Bus2: BusRef{Bus: "B2", Nodes: []int{1, 2, 3, 4}},
		},
		{
			Name: "L1", Linecode: "main", LengthKm: 0.5, Conductors: 4,
			Bus1: BusRef{Bus: "B1", Nodes: []int{1, 2, 3, 4}}, Bus2: BusRef{Bus: "B2", Nodes: []int{1, 2, 3, 4}},
		},
	}
	if !reflect.DeepEqual(c.Lines, wantLines) {
		t.Errorf("Lines:\n got %+v\nwant %+v", c.Lines, wantLines)
	}

	wantLoads := []Load{
		{Name: "Ld1", Phases: 1, KV: 0.23, Bus: BusRef{Bus: "B3", Nodes: []int{1, 4}}},
		{Name: "Ld2", Phases: 1, KV: 0.23, Bus: BusRef{Bus: "B4", Nodes: []int{3, 4}}},
	}
	if !reflect.DeepEqual(c.Loads, wantLoads) {
		t.Errorf("Loads = %+v", c.Loads)
	}

	wantTx := []Transformer{{
		Name: "T1", XHLPercent: 4, Lead: true,
		Windings: []Winding{
			{Bus: BusRef{Bus: "B0", Nodes: []int{1, 2, 3, 0}}, Delta: true, KV: 11, KVA: 500, RPercent: 0.5, Tap: 1},
			{Bus: BusRef{Bus: "B1", Nodes: []int{1, 2, 3, 0}}, KV: 0.4, KVA: 500, RPercent: 0.5, Tap: 1},
		},
	}}
	if !reflect.DeepEqual(c.Transformers, wantTx) {
		t.Errorf("Transformers = %+v", c.Transformers)
	}

	wantReactors := []Reactor{
		{Name: "g1", R: 0.3, Bus1: BusRef{Bus: "B1", Nodes: []int{4}}, Bus2: BusRef{Bus: "B1", Nodes: []int{0}}},
		{Name: "g3", R: 10, Bus1: BusRef{Bus: "B3", Nodes: []int{4}}, Bus2: BusRef{Bus: "B3", Nodes: []int{0}}},
	}
	if !reflect.DeepEqual(c.Reactors, wantReactors) {
		t.Errorf("Reactors = %+v", c.Reactors)
	}
}

func TestLengthUnits(t *testing.T) {
	t.Parallel()

	for unit, wantKm := range map[string]float64{"km": 2, "m": 0.002, "kft": 0.6096, "ft": 0.0006096, "mi": 3.218688, "KM": 2} {
		c, err := Interpret(mustParse(t, "new line.a linecode=x bus1=a bus2=b phases=4 length=2 units="+unit))
		if err != nil {
			t.Fatalf("%s: %v", unit, err)
		}
		approx(t, unit, c.Lines[0].LengthKm, wantKm)
	}
}

func TestTransformerWindingDefaults(t *testing.T) {
	t.Parallel()

	// Properties before any wdg belong to winding 1, and lag is the default.
	c, err := Interpret(mustParse(t, "new transformer.t xhl=5 bus=hv.1.2.3 conn=Delta kv=11 kva=100 wdg=2 bus=lv kv=0.4 kva=100 conn=wye"))
	if err != nil {
		t.Fatal(err)
	}
	tx := c.Transformers[0]
	if tx.Lead || len(tx.Windings) != 2 || !tx.Windings[0].Delta || tx.Windings[1].Delta {
		t.Errorf("transformer = %+v", tx)
	}
	if got := tx.Windings[0].Bus; !reflect.DeepEqual(got, BusRef{Bus: "hv", Nodes: []int{1, 2, 3}}) {
		t.Errorf("winding 1 bus = %+v", got)
	}
	if tx.Windings[0].Tap != 1 || tx.Windings[1].Tap != 1 {
		t.Errorf("taps = %v, %v, want the default 1", tx.Windings[0].Tap, tx.Windings[1].Tap)
	}
}

func TestInterpretErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, script, want string
	}{
		{name: "unknown verb", script: "plot circuit", want: `t.dss:1: unsupported command "plot"`},
		{name: "new without element", script: "new", want: "new: missing element"},
		{name: "new without class", script: "new thing kv=1", want: `want class.name, got "thing"`},
		{name: "new without name", script: "new line. kv=1", want: `want class.name, got "line."`},
		{name: "new with keyed element", script: "new object=line.a", want: "want class.name"},
		{name: "unknown class", script: "new capacitor.c1 kvar=5", want: "capacitor.c1: unsupported element class"},
		{name: "unknown property", script: "new reactor.r bus1=a.4 bus2=a.0 r=1 zeta=2 alpha=1", want: "reactor.r: unsupported property alpha, zeta"},
		{name: "missing property", script: "new reactor.r bus1=a.4 bus2=a.0", want: "reactor.r: missing r"},
		{name: "bad float", script: "new circuit.c basekv=eleven", want: `basekv="eleven" is not a number`},
		{name: "bad integer", script: "new load.l phases=one bus1=a.1.4 kv=0.23", want: `phases="one" is not an integer`},
		{name: "positional value", script: "new load.l phases=1 bus1=a.1.4 kv=0.23 stray", want: `unexpected value "stray" with no key`},
		{name: "bad node", script: "new load.l phases=1 bus1=a.x.4 kv=0.23", want: `bus1: node "x" is not a node number`},
		{name: "negative node", script: "new load.l phases=1 bus1=a.-1 kv=0.23", want: `bus1: node "-1" is not a node number`},
		{name: "bad unit", script: "new line.a linecode=x bus1=a bus2=b phases=4 length=2 units=furlong", want: `units="furlong" is not a supported length unit`},
		{name: "60 Hz", script: "set basefrequency=60", want: "basefrequency=60: only 50 Hz is supported"},
		{name: "matrix rows", script: "new linecode.c nphases=2 units=km rmatrix=[1] xmatrix=[1|1 1] cmatrix=[1|1 1]", want: "rmatrix has 1 rows, want 2"},
		{name: "matrix row length", script: "new linecode.c nphases=3 units=km rmatrix=[1|1 1|1 1] xmatrix=[1|1 1|1 1 1] cmatrix=[1|1 1|1 1 1]", want: "rmatrix row 3 has 2 values, want 3 or 3"},
		{name: "matrix value", script: "new linecode.c nphases=1 units=km rmatrix=[x] xmatrix=[1] cmatrix=[1]", want: `rmatrix="x" is not a number`},
		{name: "winding zero", script: "new transformer.t xhl=1 wdg=0 bus=a", want: "wdg=0: windings count from 1"},
		{name: "winding not a number", script: "new transformer.t xhl=1 wdg=first bus=a", want: `wdg="first" is not an integer`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Interpret(mustParse(t, tt.script))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

func TestReadErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		fsys fstest.MapFS
		want string
	}{
		{name: "missing master", fsys: fstest.MapFS{}, want: "open script: open Master.dss"},
		{
			name: "missing redirect target",
			fsys: fstest.MapFS{"Master.dss": {Data: []byte("redirect gone.dss")}},
			want: "open script: open gone.dss",
		},
		{
			name: "redirect without a file",
			fsys: fstest.MapFS{"Master.dss": {Data: []byte("redirect")}},
			want: "Master.dss:1: redirect: want one file name",
		},
		{
			name: "redirect loop",
			fsys: fstest.MapFS{
				"Master.dss": {Data: []byte("redirect a/b.dss")},
				"a/b.dss":    {Data: []byte("redirect ../Master.dss")},
			},
			want: "Master.dss: redirect loop",
		},
		{
			name: "syntax error in a redirected file",
			fsys: fstest.MapFS{
				"Master.dss": {Data: []byte("redirect b.dss")},
				"b.dss":      {Data: []byte("~ kv=1")},
			},
			want: "b.dss:1: continuation line",
		},
		{
			name: "unsupported content",
			fsys: fstest.MapFS{"Master.dss": {Data: []byte("new generator.g kw=5")}},
			want: "unsupported element class",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Read(tt.fsys, "Master.dss")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

func mustParse(t *testing.T, script string) []Command {
	t.Helper()
	cmds, err := Parse("t.dss", strings.NewReader(script))
	if err != nil {
		t.Fatal(err)
	}
	return cmds
}

func approx(t *testing.T, what string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9*math.Max(1, math.Abs(want)) {
		t.Errorf("%s = %.12g, want %.12g", what, got, want)
	}
}

// rawFeeders holds the CSIRO feeders as downloaded by `just data`, and
// rawLV10 the one the demo started with. The raw files are not in the
// repository, so tests that read them skip when they are absent.
const (
	rawFeeders = "../../../../../data/raw/csiro/LV"
	rawLV10    = rawFeeders + "/LV10_223bus"
)

func readLV10(t *testing.T) *Circuit {
	t.Helper()
	if _, err := os.Stat(rawLV10 + "/Master.dss"); err != nil {
		t.Skip("raw CSIRO data not downloaded; run `just data`")
	}
	c, err := Read(os.DirFS(rawLV10), "Master.dss")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestLV10Linecodes(t *testing.T) {
	t.Parallel()
	c := readLV10(t)

	// The 16 mm² copper service cable: 0.454302 ohm per 1000 ft.
	// 1000 ft = 0.3048 km, so R11 = 0.454302 / 0.3048 = 1.490492126 ohm/km.
	service, ok := c.Linecodes["ugsc_16cu_xlpe/nyl/pvc_ug_4w_bundled"]
	if !ok {
		t.Fatal("service cable linecode missing")
	}
	approx(t, "service R11 ohm/km", service.R[0][0], 1.4904921259842519)
	// Carson's earth-return resistance, π²·f·1e-4 = 0.0493 ohm/km at 50 Hz,
	// shows in every off-diagonal term.
	if got := service.R[1][0]; math.Abs(got-0.0493) > 0.0005 {
		t.Errorf("service R21 = %v ohm/km, want about 0.0493", got)
	}
	if len(c.Linecodes) != 22 {
		t.Errorf("%d distinct linecodes, want 22 (28 definitions, 6 of them repeats)", len(c.Linecodes))
	}
}
