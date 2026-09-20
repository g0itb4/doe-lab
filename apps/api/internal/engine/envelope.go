package engine

import (
	"errors"
	"fmt"
	"math/cmplx"
)

// Constraint names the kind of limit that stops an envelope from being
// larger.
type Constraint int

// The constraints, in the order of the database's binding_constraint type.
const (
	// ConstraintNone: nothing limits the envelope. No site in the search has
	// a cap in this direction.
	ConstraintNone Constraint = iota
	// ConstraintVoltageHigh: a customer's voltage would rise above the band.
	ConstraintVoltageHigh
	// ConstraintVoltageLow: a customer's voltage would fall below the band.
	ConstraintVoltageLow
	// ConstraintTransformer: the transformer would exceed its rating.
	ConstraintTransformer
	// ConstraintLine: a conductor would exceed its current rating.
	ConstraintLine
	// ConstraintSiteCap: the network could take more; every site is at its
	// own cap.
	ConstraintSiteCap
)

var constraintNames = [...]string{"none", "voltage_high", "voltage_low", "transformer", "line", "site_cap"}

// String returns the constraint's name as the database and the API spell it.
func (c Constraint) String() string {
	return constraintNames[c]
}

// Binding is the limit that an operating point meets or breaks.
type Binding struct {
	Constraint Constraint
	// Element is the site, the line or "transformer".
	Element string
	// Value and Limit are in the constraint's own unit: per unit of 230 V
	// for a voltage, volt-amperes for the transformer, amperes for a line,
	// watts for a site cap.
	Value float64
	Limit float64
}

// Policy decides how capacity is shared between sites.
type Policy int

const (
	// Equal gives every site the same limit, up to its own cap.
	Equal Policy = iota
	// Proportional gives every site the same fraction of its cap.
	Proportional
)

// Limits are the network limits that an envelope must keep.
type Limits struct {
	// VMinPU and VMaxPU bound each customer's phase-to-neutral voltage, per
	// unit of 230 V.
	VMinPU float64
	VMaxPU float64
	// TransformerPU is the allowed transformer loading, per unit of its
	// rating.
	TransformerPU float64
	// LinePU is the allowed conductor current, per unit of the line's
	// rating.
	LinePU float64
}

// Config is the policy and the limits of an engine.
type Config struct {
	Policy Policy
	Limits Limits
	// PrecisionW is how closely the search finds the limit, in watts.
	PrecisionW float64
}

// ErrInvalidConfig is wrapped by the errors of New for a bad Config.
var ErrInvalidConfig = errors.New("invalid config")

// SiteInput describes one site for one interval.
type SiteInput struct {
	// Base is the site's forecast net power in volt-amperes when it is not
	// part of a search: positive real power is consumption.
	Base complex128
	// ExportCapW and ImportCapW are the largest limits the site may be
	// given. A site with a zero cap in a direction takes no part in that
	// search and stays at Base.
	ExportCapW float64
	ImportCapW float64
}

// Result is the envelope of every site for one interval.
type Result struct {
	// ExportW and ImportW are the limits per site, in the order of the
	// network's sites, in watts at the connection point.
	ExportW []float64
	ImportW []float64
	// Export and Import say what stops each limit from being larger.
	Export Binding
	Import Binding
}

// Engine computes operating envelopes for one network under one Config. It is
// safe for concurrent use; each caller keeps its own Solution.
type Engine struct {
	net *Network
	pf  *PowerFlow
	cfg Config
}

// New builds an engine. The network is factorised once, here.
func New(net *Network, cfg Config) (*Engine, error) {
	l := cfg.Limits
	switch {
	case l.VMinPU <= 0 || l.VMinPU >= l.VMaxPU:
		return nil, fmt.Errorf("%w: want 0 < VMinPU < VMaxPU, got %v and %v", ErrInvalidConfig, l.VMinPU, l.VMaxPU)
	case l.TransformerPU <= 0 || l.LinePU <= 0:
		return nil, fmt.Errorf("%w: transformer and line limits must be positive", ErrInvalidConfig)
	case cfg.PrecisionW <= 0:
		return nil, fmt.Errorf("%w: PrecisionW must be positive", ErrInvalidConfig)
	case cfg.Policy != Equal && cfg.Policy != Proportional:
		return nil, fmt.Errorf("%w: unknown policy %d", ErrInvalidConfig, cfg.Policy)
	}
	pf, err := NewPowerFlow(net, Tree)
	if err != nil {
		return nil, err
	}
	return &Engine{net: net, pf: pf, cfg: cfg}, nil
}

// Check solves the power flow for the given power at every site and returns
// the limit that the operating point breaks by the widest margin. When it
// keeps every limit, the Binding's constraint is ConstraintNone.
func (e *Engine) Check(load []complex128, sol *Solution) (Binding, error) {
	if err := e.pf.Solve(load, sol); err != nil {
		return Binding{}, err
	}
	worst, _ := e.worst(sol)
	return worst, nil
}

// worst returns the limit that sol breaks by the widest relative margin. ok is
// false when sol keeps every limit.
func (e *Engine) worst(sol *Solution) (worst Binding, ok bool) {
	excess := 0.0
	// consider records a limit that is broken by more than any before it.
	// over is the relative margin: 0.02 means 2 % past the limit.
	consider := func(over float64, b Binding) {
		if over > excess {
			excess, worst, ok = over, b, true
		}
	}

	l := e.cfg.Limits
	for i, site := range e.net.Sites {
		pu := e.pf.PhaseVoltage(sol, i) / NominalVoltage
		consider(pu/l.VMaxPU-1, Binding{Constraint: ConstraintVoltageHigh, Element: site.Name, Value: pu, Limit: l.VMaxPU})
		consider(1-pu/l.VMinPU, Binding{Constraint: ConstraintVoltageLow, Element: site.Name, Value: pu, Limit: l.VMinPU})
	}

	loading := cmplx.Abs(e.pf.SourcePower(sol))
	rating := e.net.Source.KVA * 1000 * l.TransformerPU
	consider(loading/rating-1, Binding{Constraint: ConstraintTransformer, Element: "transformer", Value: loading, Limit: rating})

	for i, bus := range e.net.Buses {
		if bus.Line == nil || bus.Line.AmpacityA <= 0 {
			continue
		}
		limit := bus.Line.AmpacityA * l.LinePU
		for _, amperes := range e.pf.LineCurrent(sol, i) {
			consider(amperes/limit-1, Binding{Constraint: ConstraintLine, Element: bus.Line.Name, Value: amperes, Limit: limit})
		}
	}
	return worst, ok
}

// Envelope finds the export and import limits of every site for one interval.
//
// Each search asks: if every site with a cap ran at its limit at the same
// time, how large can the limits be before the feeder breaks a limit? Sites
// with a cap are set to the limit (export, then import, at unity power
// factor); the others stay at their forecast. The limits are raised together,
// by bisection on a common scale, and every step is a full power flow. There
// is no linearisation.
//
// When the feeder already breaks a limit with every limited site at zero, the
// limits in that direction are zero and the Binding names the broken limit.
func (e *Engine) Envelope(sites []SiteInput, sol *Solution) (Result, error) {
	if len(sites) != len(e.net.Sites) {
		return Result{}, fmt.Errorf("got %d site inputs for %d sites", len(sites), len(e.net.Sites))
	}
	exportCaps := make([]float64, len(sites))
	importCaps := make([]float64, len(sites))
	for i, s := range sites {
		if s.ExportCapW < 0 || s.ImportCapW < 0 {
			return Result{}, fmt.Errorf("site %s: caps must not be negative", e.net.Sites[i].Name)
		}
		exportCaps[i], importCaps[i] = s.ExportCapW, s.ImportCapW
	}

	var r Result
	r.ExportW, r.Export = e.search(sites, exportCaps, -1, sol)
	r.ImportW, r.Import = e.search(sites, importCaps, +1, sol)
	return r, nil
}

// search bisects on the scale of the limits in one direction. sign is -1 for
// export and +1 for import: the power a site draws at its limit.
func (e *Engine) search(sites []SiteInput, caps []float64, sign float64, sol *Solution) ([]float64, Binding) {
	largest := 0.0
	for _, c := range caps {
		largest = max(largest, c)
	}
	limits := make([]float64, len(caps))
	load := make([]complex128, len(caps))

	// try sets every limit for the scale, solves, and reports the broken
	// limit, if any.
	try := func(scale float64) (Binding, bool) {
		for i, c := range caps {
			if e.cfg.Policy == Proportional {
				limits[i] = scale * c
			} else {
				limits[i] = min(scale*largest, c)
			}
			load[i] = sites[i].Base
			if c > 0 {
				load[i] = complex(sign*limits[i], 0)
			}
		}
		if err := e.pf.Solve(load, sol); err != nil {
			// No operating point at all: the voltage has collapsed.
			return Binding{Constraint: ConstraintVoltageLow, Limit: e.cfg.Limits.VMinPU}, true
		}
		return e.worst(sol)
	}

	if broken, bad := try(0); bad {
		return limits, broken
	}
	if largest == 0 {
		return limits, Binding{Constraint: ConstraintNone}
	}
	binding, bad := try(1)
	if !bad {
		return limits, Binding{Constraint: ConstraintSiteCap, Value: largest, Limit: largest}
	}

	// The limit lies between lo, which the feeder can carry, and hi, which
	// it cannot. binding is what breaks at hi.
	lo, hi := 0.0, 1.0
	for (hi-lo)*largest > e.cfg.PrecisionW {
		mid := (lo + hi) / 2
		if broken, bad := try(mid); bad {
			hi, binding = mid, broken
		} else {
			lo = mid
		}
	}
	// Leave the limits, and the solution, at the end the feeder can carry.
	try(lo)
	return limits, binding
}
