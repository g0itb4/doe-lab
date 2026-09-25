package engine

import (
	"fmt"
	"math"
	"math/cmplx"
)

// State describes one solved operating point of the feeder.
type State struct {
	// SourceVA is the power that the transformer delivers, in volt-amperes.
	// A negative real part is reverse power flow.
	SourceVA complex128
	// LoadingPU is the transformer's loading, per unit of its rating.
	LoadingPU float64
	// VMinPU and VMaxPU are the lowest and the highest customer voltage, per
	// unit of 230 V. Both are zero for a network with no customers.
	VMinPU float64
	VMaxPU float64
	// Worst is the limit that the operating point breaks by the widest
	// margin. Its constraint is ConstraintNone when it keeps every limit.
	Worst Binding
}

// state reads a State from a solved operating point.
func (e *Engine) state(sol *Solution) State {
	var s State
	for i := range e.net.Sites {
		pu := e.pf.PhaseVoltage(sol, i) / NominalVoltage
		if i == 0 {
			s.VMinPU = pu
		}
		s.VMinPU, s.VMaxPU = min(s.VMinPU, pu), max(s.VMaxPU, pu)
	}
	s.SourceVA = e.pf.SourcePower(sol)
	s.LoadingPU = cmplx.Abs(s.SourceVA) / (e.net.Source.KVA * 1000)
	s.Worst, _ = e.worst(sol)
	return s
}

// Report is what an interval looks like beside its envelopes: where the
// feeder is forecast to be, and what a fixed export limit would do to it.
type Report struct {
	// Forecast is the feeder with every site at its forecast.
	Forecast State
	// Static is the feeder with every site that has an export cap exporting
	// at the fixed limit, or at its cap where that is lower, and the others
	// at their forecast: the same question an envelope answers, asked of a
	// limit that does not move.
	Static State
	// StaticTotalW is what those sites would export together, in watts.
	StaticTotalW float64
}

// Report solves the two operating points of a Report. staticLimitW is the
// fixed export limit to compare against, in watts.
func (e *Engine) Report(sites []SiteInput, staticLimitW float64, sol *Solution) (Report, error) {
	if len(sites) != len(e.net.Sites) {
		return Report{}, fmt.Errorf("got %d site inputs for %d sites", len(sites), len(e.net.Sites))
	}
	if staticLimitW < 0 || math.IsNaN(staticLimitW) {
		return Report{}, fmt.Errorf("the fixed limit must not be negative, got %v", staticLimitW)
	}
	var r Report
	load := make([]complex128, len(sites))

	for i, s := range sites {
		load[i] = s.Base
	}
	if err := e.pf.Solve(load, sol); err != nil {
		return Report{}, fmt.Errorf("the forecast: %w", err)
	}
	r.Forecast = e.state(sol)

	for i, s := range sites {
		if s.ExportCapW > 0 {
			limit := min(staticLimitW, s.ExportCapW)
			load[i] = complex(-limit, 0)
			r.StaticTotalW += limit
		}
	}
	if err := e.pf.Solve(load, sol); err != nil {
		return Report{}, fmt.Errorf("a fixed limit of %.0f W: %w", staticLimitW, err)
	}
	r.Static = e.state(sol)
	return r, nil
}
