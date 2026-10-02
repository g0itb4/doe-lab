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

	// The same operating point bus by bus, in the network's bus order: what
	// a drawing of the feeder shows.
	//
	// BusVPU is the phase-to-neutral voltage of each phase of each bus, per
	// unit of 230 V.
	BusVPU [][Neutral]float64
	// LineA is the current in each conductor of the line into each bus, in
	// amperes, and LineW the power that line carries towards the bus, in
	// watts: negative is power flowing back towards the transformer. Both
	// are zero for the first bus, which has no line.
	LineA [][Conductors]float64
	LineW []float64
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

	n := len(e.net.Buses)
	s.BusVPU, s.LineA, s.LineW = make([][Neutral]float64, n), make([][Conductors]float64, n), make([]float64, n)
	for i := range e.net.Buses {
		for ph, volts := range e.pf.BusVoltage(sol, i) {
			s.BusVPU[i][ph] = volts / NominalVoltage
		}
		if i > 0 {
			s.LineA[i], s.LineW[i] = e.pf.LineCurrent(sol, i), real(e.pf.LinePower(sol, i))
		}
	}
	return s
}

// Report is what an interval looks like beside its envelopes: where the
// feeder is forecast to be, where the envelopes take it, and what a fixed
// export limit would do to it.
type Report struct {
	// Forecast is the feeder with every site at its forecast.
	Forecast State
	// Envelope is the feeder with every site that has an export cap exporting
	// at its envelope, and the others at their forecast: the operating point
	// that the search for the envelope ended on.
	Envelope State
	// Static is the feeder with every site that has an export cap exporting
	// at the fixed limit, or at its cap where that is lower, and the others
	// at their forecast: the same question an envelope answers, asked of a
	// limit that does not move.
	Static State
	// StaticTotalW is what those sites would export together, in watts.
	StaticTotalW float64
}

// Report solves the three operating points of a Report. exportW is the export
// limit of each site, as Envelope returned it, and staticLimitW the fixed
// export limit to compare against, in watts.
func (e *Engine) Report(sites []SiteInput, exportW []float64, staticLimitW float64, sol *Solution) (Report, error) {
	if len(sites) != len(e.net.Sites) || len(exportW) != len(e.net.Sites) {
		return Report{}, fmt.Errorf("got %d site inputs and %d export limits for %d sites", len(sites), len(exportW), len(e.net.Sites))
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
			load[i] = complex(-exportW[i], 0)
		}
	}
	if err := e.pf.Solve(load, sol); err != nil {
		return Report{}, fmt.Errorf("the envelopes: %w", err)
	}
	r.Envelope = e.state(sol)

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
