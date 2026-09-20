package engine

import (
	"errors"
	"math"
	"math/rand/v2"
	"strings"
	"testing"
)

// lax keeps every limit out of the way, so a test can tighten one at a time.
func lax() Config {
	return Config{
		Policy:     Equal,
		PrecisionW: 1,
		Limits:     Limits{VMinPU: 0.5, VMaxPU: 1.5, TransformerPU: 10, LinePU: 10},
	}
}

// twoSite is the hand case with a second customer at the house, on phase 2.
func twoSite() *Network {
	n := handCase()
	n.Sites = append(n.Sites, Site{Name: "house-b", Bus: 2, Phase: 2})
	return n
}

func mustEngine(t testing.TB, net *Network, cfg Config) *Engine {
	t.Helper()
	e, err := New(net, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestConstraintString(t *testing.T) {
	t.Parallel()

	want := []string{"none", "voltage_high", "voltage_low", "transformer", "line", "site_cap"}
	for i, w := range want {
		if got := Constraint(i).String(); got != w {
			t.Errorf("Constraint(%d) = %q, want %q", i, got, w)
		}
	}
}

// Each limit binds in turn on the hand case, where the operating point is
// known exactly: drawing 2300 W gives 10 A, 230 V at the customer and 2400 VA
// at the source; exporting 2500 W gives 250 V.
func TestCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		load    complex128
		limits  func(l *Limits)
		network func(n *Network)
		want    Binding
	}{
		{
			name: "within every limit",
			load: 2300,
			want: Binding{Constraint: ConstraintNone},
		},
		{
			name:   "voltage high",
			load:   -2500,
			limits: func(l *Limits) { l.VMaxPU = 1.05 },
			want:   Binding{Constraint: ConstraintVoltageHigh, Element: "house-a", Value: 250.0 / 230, Limit: 1.05},
		},
		{
			name:   "voltage low",
			load:   2300,
			limits: func(l *Limits) { l.VMinPU = 1.02 },
			want:   Binding{Constraint: ConstraintVoltageLow, Element: "house-a", Value: 1, Limit: 1.02},
		},
		{
			name: "transformer",
			load: 2300,
			// 2 % of 100 kVA.
			limits: func(l *Limits) { l.TransformerPU = 0.02 },
			want:   Binding{Constraint: ConstraintTransformer, Element: "transformer", Value: 2400, Limit: 2000},
		},
		{
			name:    "line",
			load:    2300,
			limits:  func(l *Limits) { l.LinePU = 0.8 },
			network: func(n *Network) { n.Buses[2].Line.AmpacityA = 10 },
			want:    Binding{Constraint: ConstraintLine, Element: "service", Value: 10, Limit: 8},
		},
		{
			name: "the widest margin wins",
			load: 2300,
			// Voltage is 1.96 % under its limit; the transformer is 20 %
			// over.
			limits: func(l *Limits) { l.VMinPU, l.TransformerPU = 1.02, 0.02 },
			want:   Binding{Constraint: ConstraintTransformer, Element: "transformer", Value: 2400, Limit: 2000},
		},
		{
			name: "an unrated line is not checked",
			load: 2300,
			// The main has no rating; only the service does, and it is
			// within it.
			limits:  func(l *Limits) { l.LinePU = 1 },
			network: func(n *Network) { n.Buses[2].Line.AmpacityA = 11 },
			want:    Binding{Constraint: ConstraintNone},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			net, cfg := handCase(), lax()
			if tt.limits != nil {
				tt.limits(&cfg.Limits)
			}
			if tt.network != nil {
				tt.network(net)
			}
			got, err := mustEngine(t, net, cfg).Check([]complex128{tt.load}, &Solution{})
			if err != nil {
				t.Fatal(err)
			}
			if got.Constraint != tt.want.Constraint || got.Element != tt.want.Element ||
				math.Abs(got.Value-tt.want.Value) > 1e-6 || math.Abs(got.Limit-tt.want.Limit) > 1e-9 {
				t.Errorf("Check = %+v, want %+v", got, tt.want)
			}
		})
	}

	t.Run("no operating point", func(t *testing.T) {
		t.Parallel()
		_, err := mustEngine(t, handCase(), lax()).Check([]complex128{20000}, &Solution{})
		if !errors.Is(err, ErrNotConverged) {
			t.Errorf("error = %v, want ErrNotConverged", err)
		}
	})
}

func TestNewErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(c *Config)
		want   string
	}{
		{name: "zero VMin", change: func(c *Config) { c.Limits.VMinPU = 0 }, want: "want 0 < VMinPU < VMaxPU"},
		{name: "inverted band", change: func(c *Config) { c.Limits.VMinPU, c.Limits.VMaxPU = 1.1, 0.94 }, want: "want 0 < VMinPU < VMaxPU"},
		{name: "zero transformer limit", change: func(c *Config) { c.Limits.TransformerPU = 0 }, want: "transformer and line limits must be positive"},
		{name: "zero line limit", change: func(c *Config) { c.Limits.LinePU = 0 }, want: "transformer and line limits must be positive"},
		{name: "zero precision", change: func(c *Config) { c.PrecisionW = 0 }, want: "PrecisionW must be positive"},
		{name: "unknown policy", change: func(c *Config) { c.Policy = 7 }, want: "unknown policy 7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := lax()
			tt.change(&cfg)
			_, err := New(handCase(), cfg)
			if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want ErrInvalidConfig containing %q", err, tt.want)
			}
		})
	}

	t.Run("invalid network", func(t *testing.T) {
		t.Parallel()
		n := handCase()
		n.Buses[0].Ground = nil
		if _, err := New(n, lax()); !errors.Is(err, ErrInvalidNetwork) {
			t.Errorf("error = %v, want ErrInvalidNetwork", err)
		}
	})
}

// On the hand case the answers are known in closed form. The customer's
// voltage is V = (240 ± sqrt(240² ∓ 4·P)) / 2 over a 1 ohm loop, so:
//
//	export limit for V ≤ 250 V:  P = 250·(250 - 240) = 2500 W
//	import limit for V ≥ 230 V:  P = 230·(240 - 230) = 2300 W
func TestEnvelopeKnownAnswer(t *testing.T) {
	t.Parallel()

	cfg := lax()
	cfg.Limits.VMaxPU = 250.0 / 230
	cfg.Limits.VMinPU = 1
	e := mustEngine(t, handCase(), cfg)

	var sol Solution
	// Caps that no bisection step lands exactly on the answer from.
	r, err := e.Envelope([]SiteInput{{ExportCapW: 4700, ImportCapW: 4700}}, &sol)
	if err != nil {
		t.Fatal(err)
	}
	// The search stops within PrecisionW below the true limit, never above.
	if r.ExportW[0] > 2500 || r.ExportW[0] < 2499 {
		t.Errorf("export limit = %.3f W, want within 1 W below 2500", r.ExportW[0])
	}
	if r.ImportW[0] > 2300 || r.ImportW[0] < 2299 {
		t.Errorf("import limit = %.3f W, want within 1 W below 2300", r.ImportW[0])
	}
	if r.Export.Constraint != ConstraintVoltageHigh || r.Export.Element != "house-a" {
		t.Errorf("export binding = %+v, want voltage_high at house-a", r.Export)
	}
	if r.Import.Constraint != ConstraintVoltageLow || r.Import.Element != "house-a" {
		t.Errorf("import binding = %+v, want voltage_low at house-a", r.Import)
	}

	// Running at the returned limits keeps every limit.
	for _, load := range []complex128{complex(-r.ExportW[0], 0), complex(r.ImportW[0], 0)} {
		b, err := e.Check([]complex128{load}, &sol)
		if err != nil || b.Constraint != ConstraintNone {
			t.Errorf("at %v W: binding %+v, error %v; want within every limit", real(load), b, err)
		}
	}
}

func TestEnvelopeSiteCap(t *testing.T) {
	t.Parallel()

	e := mustEngine(t, handCase(), lax())
	r, err := e.Envelope([]SiteInput{{ExportCapW: 1000, ImportCapW: 1500}}, &Solution{})
	if err != nil {
		t.Fatal(err)
	}
	if r.ExportW[0] != 1000 || r.Export.Constraint != ConstraintSiteCap || r.Export.Limit != 1000 {
		t.Errorf("export = %v W, %+v; want the 1000 W cap", r.ExportW[0], r.Export)
	}
	if r.ImportW[0] != 1500 || r.Import.Constraint != ConstraintSiteCap {
		t.Errorf("import = %v W, %+v; want the 1500 W cap", r.ImportW[0], r.Import)
	}
}

func TestEnvelopeNoCaps(t *testing.T) {
	t.Parallel()

	e := mustEngine(t, handCase(), lax())
	r, err := e.Envelope([]SiteInput{{Base: 500}}, &Solution{})
	if err != nil {
		t.Fatal(err)
	}
	if r.ExportW[0] != 0 || r.ImportW[0] != 0 || r.Export.Constraint != ConstraintNone || r.Import.Constraint != ConstraintNone {
		t.Errorf("result = %+v, want zero limits and no binding", r)
	}
}

// A cap below the search precision: the loop has nothing to narrow, and the
// limit is zero with the constraint that breaks at the cap.
func TestEnvelopeCapBelowPrecision(t *testing.T) {
	t.Parallel()

	cfg := lax()
	cfg.PrecisionW = 5000
	cfg.Limits.VMaxPU = 250.0 / 230
	e := mustEngine(t, handCase(), cfg)
	r, err := e.Envelope([]SiteInput{{ExportCapW: 4000}}, &Solution{})
	if err != nil {
		t.Fatal(err)
	}
	if r.ExportW[0] != 0 || r.Export.Constraint != ConstraintVoltageHigh {
		t.Errorf("export = %v W, %+v", r.ExportW[0], r.Export)
	}
}

// The passive neighbour already pulls its voltage below the band, so the
// feeder breaks a limit before any limited site moves: the limits are zero.
func TestEnvelopeZeroWhenAlreadyViolated(t *testing.T) {
	t.Parallel()

	cfg := lax()
	cfg.Limits.VMinPU = 1
	e := mustEngine(t, twoSite(), cfg)
	r, err := e.Envelope([]SiteInput{
		{ExportCapW: 5000, ImportCapW: 5000},
		{Base: 3000}, // house-b, passive: below 230 V on its own
	}, &Solution{})
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]struct {
		w float64
		b Binding
	}{"export": {r.ExportW[0], r.Export}, "import": {r.ImportW[0], r.Import}} {
		if got.w != 0 || got.b.Constraint != ConstraintVoltageLow || got.b.Element != "house-b" {
			t.Errorf("%s = %v W, %+v; want 0 W, voltage_low at house-b", name, got.w, got.b)
		}
	}
	if r.ExportW[1] != 0 || r.ImportW[1] != 0 {
		t.Errorf("the passive site got limits %v and %v, want none", r.ExportW[1], r.ImportW[1])
	}
}

func TestEnvelopePolicies(t *testing.T) {
	t.Parallel()

	inputs := []SiteInput{{ExportCapW: 1000}, {ExportCapW: 5000}}
	cfg := lax()
	cfg.Limits.VMaxPU = 250.0 / 230

	// Equal: both rise together until house-a reaches its 1000 W cap, then
	// house-b carries on alone to the voltage limit.
	equal, err := mustEngine(t, twoSite(), cfg).Envelope(inputs, &Solution{})
	if err != nil {
		t.Fatal(err)
	}
	if equal.ExportW[0] != 1000 || equal.ExportW[1] <= 1000 || equal.ExportW[1] >= 5000 {
		t.Errorf("equal: limits %v, want 1000 and something between 1000 and 5000", equal.ExportW)
	}

	// Proportional: the same fraction of each cap.
	cfg.Policy = Proportional
	prop, err := mustEngine(t, twoSite(), cfg).Envelope(inputs, &Solution{})
	if err != nil {
		t.Fatal(err)
	}
	if prop.ExportW[0] >= 1000 || math.Abs(prop.ExportW[1]-5*prop.ExportW[0]) > 1e-9 {
		t.Errorf("proportional: limits %v, want a 1:5 split below the caps", prop.ExportW)
	}
	if prop.Export.Constraint != ConstraintVoltageHigh || prop.Export.Element != "house-b" {
		t.Errorf("proportional binding = %+v, want voltage_high at house-b", prop.Export)
	}
}

// With every limit out of the way, the only thing that stops import is that
// the feeder has no operating point at all: voltage collapse, at most
// E²/4R = 14.4 kW on the hand case.
func TestEnvelopeVoltageCollapse(t *testing.T) {
	t.Parallel()

	cfg := lax()
	cfg.Limits.VMinPU = 0.01
	e := mustEngine(t, handCase(), cfg)
	r, err := e.Envelope([]SiteInput{{ImportCapW: 20000}}, &Solution{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Import.Constraint != ConstraintVoltageLow || r.Import.Element != "" {
		t.Errorf("import binding = %+v, want voltage_low with no element", r.Import)
	}
	if r.ImportW[0] < 10000 || r.ImportW[0] > 14400 {
		t.Errorf("import limit = %.0f W, want between 10 kW and the 14.4 kW collapse point", r.ImportW[0])
	}
}

func TestEnvelopeErrors(t *testing.T) {
	t.Parallel()

	e := mustEngine(t, handCase(), lax())
	if _, err := e.Envelope(nil, &Solution{}); err == nil || !strings.Contains(err.Error(), "got 0 site inputs for 1 sites") {
		t.Errorf("error = %v", err)
	}
	for _, in := range []SiteInput{{ExportCapW: -1}, {ImportCapW: -1}} {
		if _, err := e.Envelope([]SiteInput{in}, &Solution{}); err == nil || !strings.Contains(err.Error(), "site house-a: caps must not be negative") {
			t.Errorf("error = %v", err)
		}
	}
}

// lv10Config is the configuration the demo runs: the Australian voltage band
// of AS 61000.3.100 (+10 %, -6 % of 230 V), and plant at its rating.
func lv10Config() Config {
	return Config{
		Policy:     Equal,
		PrecisionW: 1,
		Limits:     Limits{VMinPU: 0.94, VMaxPU: 1.10, TransformerPU: 1, LinePU: 1},
	}
}

// demoTap is the transformer tap the demo runs LV10 at: one 2.5 % step below
// the dataset's nominal tap.
//
// At the nominal tap the unloaded feeder sits at 250 V, 1.087 pu, which leaves
// 3 V of headroom under the 253 V limit, and an unbalanced evening load lifts
// the lightly loaded phases above the limit through the neutral with no
// export at all. Envelopes would be near zero all day. A network operator in
// that position lowers the tap; so does the demo.
const demoTap = 0.975

// lv10Demo is LV10 with the transformer at demoTap.
func lv10Demo() *Network {
	net := *lv10()
	net.Source.VoltageV *= demoTap
	return &net
}

// propertyTrials is how many random intervals each property test draws. The
// pre-commit hook runs with -short under the race detector and coverage,
// where a search is some twenty times slower.
func propertyTrials() int {
	if testing.Short() {
		return 2
	}
	return 12
}

// randomInterval draws one interval for LV10: about six sites in ten have
// flexible DER with caps, and the rest draw a forecast load of up to maxLoadW
// at 0.95 lagging.
func randomInterval(rng *rand.Rand, sites int, maxLoadW float64) []SiteInput {
	in := make([]SiteInput, sites)
	for i := range in {
		if rng.Float64() < 0.6 {
			in[i] = SiteInput{ExportCapW: 5000 + 5000*rng.Float64(), ImportCapW: 7000 + 7000*rng.Float64()}
			continue
		}
		p := maxLoadW * rng.Float64()
		in[i].Base = complex(p, p*math.Tan(math.Acos(0.95)))
	}
	return in
}

func sum(xs []float64) float64 {
	total := 0.0
	for _, x := range xs {
		total += x
	}
	return total
}

// Property: widening the voltage band never shrinks an envelope.
func TestPropertyMonotonicInVoltageBand(t *testing.T) {
	t.Parallel()
	net := lv10Demo()
	rng := rand.New(rand.NewPCG(2026, 1001))

	for trial := range propertyTrials() {
		in := randomInterval(rng, len(net.Sites), 3000)
		policy := Policy(trial % 2)

		previousExport, previousImport := -1.0, -1.0
		for step := range 4 {
			cfg := lv10Config()
			cfg.Policy = policy
			// The upper limit rises from 1.08 to 1.14 and the lower one
			// falls from 0.98 to 0.86.
			cfg.Limits.VMaxPU = 1.08 + 0.02*float64(step)
			cfg.Limits.VMinPU = 0.98 - 0.04*float64(step)
			r, err := mustEngine(t, net, cfg).Envelope(in, &Solution{})
			if err != nil {
				t.Fatal(err)
			}
			export, imp := sum(r.ExportW), sum(r.ImportW)
			// Each search stops within PrecisionW per site of the true
			// limit, so allow that much.
			slack := cfg.PrecisionW * float64(len(net.Sites))
			if export < previousExport-slack {
				t.Errorf("trial %d: export fell from %.0f to %.0f W when VMax rose to %.2f", trial, previousExport, export, cfg.Limits.VMaxPU)
			}
			if imp < previousImport-slack {
				t.Errorf("trial %d: import fell from %.0f to %.0f W when VMin fell to %.2f", trial, previousImport, imp, cfg.Limits.VMinPU)
			}
			previousExport, previousImport = export, imp
		}
		if previousExport <= 0 || previousImport <= 0 {
			t.Errorf("trial %d: the widest band gave export %.0f W and import %.0f W, want both positive", trial, previousExport, previousImport)
		}
	}
}

// Property: when the feeder breaks a limit before any limited site moves, the
// limits are zero.
func TestPropertyZeroWhenBaseViolates(t *testing.T) {
	t.Parallel()
	net := lv10()
	rng := rand.New(rand.NewPCG(2026, 1002))
	cfg := lv10Config()
	// A band the unloaded feeder is already above: its source sits at
	// 250 V, 1.087 pu.
	cfg.Limits.VMaxPU = 1.05
	e := mustEngine(t, net, cfg)

	for trial := range propertyTrials() {
		in := randomInterval(rng, len(net.Sites), 300)
		r, err := e.Envelope(in, &Solution{})
		if err != nil {
			t.Fatal(err)
		}
		if sum(r.ExportW) != 0 || sum(r.ImportW) != 0 {
			t.Errorf("trial %d: limits %.0f W export, %.0f W import; want zero", trial, sum(r.ExportW), sum(r.ImportW))
		}
		if r.Export.Constraint != ConstraintVoltageHigh || r.Import.Constraint != ConstraintVoltageHigh {
			t.Errorf("trial %d: bindings %v and %v, want voltage_high", trial, r.Export.Constraint, r.Import.Constraint)
		}
	}
}

// Property: an envelope is safe. With every limited site at its limit, the
// feeder keeps every limit, and the limits respect the caps. The one exception
// is a feeder that breaks a limit before any limited site moves: then the
// limits are zero, and the binding names what is already broken.
func TestPropertyEnvelopeIsFeasible(t *testing.T) {
	t.Parallel()
	net := lv10Demo()
	rng := rand.New(rand.NewPCG(2026, 1003))

	for trial := range propertyTrials() {
		cfg := lv10Config()
		cfg.Policy = Policy(trial % 2)
		e := mustEngine(t, net, cfg)
		// Every other trial is heavy enough to break a limit at the base.
		in := randomInterval(rng, len(net.Sites), 2000+6000*float64(trial%2))
		var sol Solution
		r, err := e.Envelope(in, &sol)
		if err != nil {
			t.Fatal(err)
		}

		for _, direction := range []struct {
			sign    float64
			limits  []float64
			binding Binding
			limitOf func(SiteInput) float64
		}{
			{-1, r.ExportW, r.Export, func(s SiteInput) float64 { return s.ExportCapW }},
			{+1, r.ImportW, r.Import, func(s SiteInput) float64 { return s.ImportCapW }},
		} {
			base := make([]complex128, len(in))
			atLimit := make([]complex128, len(in))
			for i, s := range in {
				limit, limitCap := direction.limits[i], direction.limitOf(s)
				if limit < 0 || limit > limitCap {
					t.Fatalf("trial %d site %d: limit %.1f W outside [0, %.1f]", trial, i, limit, limitCap)
				}
				base[i], atLimit[i] = s.Base, s.Base
				if limitCap > 0 {
					base[i], atLimit[i] = 0, complex(direction.sign*limit, 0)
				}
			}

			atBase, err := e.Check(base, &sol)
			if err != nil {
				t.Fatal(err)
			}
			if atBase.Constraint != ConstraintNone {
				if sum(direction.limits) != 0 || direction.binding.Constraint != atBase.Constraint {
					t.Errorf("trial %d direction %+.0f: the base breaks %v, but the limits sum to %.0f W bound by %v",
						trial, direction.sign, atBase.Constraint, sum(direction.limits), direction.binding.Constraint)
				}
				continue
			}
			b, err := e.Check(atLimit, &sol)
			if err != nil || b.Constraint != ConstraintNone {
				t.Errorf("trial %d direction %+.0f: at the limits the feeder breaks %+v (error %v)", trial, direction.sign, b, err)
			}
			if sum(direction.limits) <= 0 {
				t.Errorf("trial %d direction %+.0f: the base keeps every limit, but the limits are zero", trial, direction.sign)
			}
		}
		t.Logf("trial %d: export %.0f W per site, bound by %v at %s; import %.0f W per site, bound by %v at %s",
			trial, maxOf(r.ExportW), r.Export.Constraint, r.Export.Element, maxOf(r.ImportW), r.Import.Constraint, r.Import.Element)
	}
}

func maxOf(xs []float64) float64 {
	m := 0.0
	for _, x := range xs {
		m = math.Max(m, x)
	}
	return m
}
