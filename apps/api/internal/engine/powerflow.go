package engine

import (
	"errors"
	"fmt"
	"math"
	"math/cmplx"
)

// Method selects how the admittance matrix is factorised. Both methods solve
// the same equations and agree to rounding error.
type Method int

const (
	// Tree eliminates the buses from the leaves to the transformer, one 4×4
	// block at a time. On a radial feeder this creates no fill-in, so the
	// work is linear in the number of buses.
	Tree Method = iota
	// Dense factorises the whole matrix. It makes no use of the feeder's
	// shape, and is kept as an independent check on Tree.
	Dense
)

// ErrNotConverged means the iteration found no operating point. With constant
// power loads that is the sign of voltage collapse: the feeder cannot carry
// the power asked of it.
var ErrNotConverged = errors.New("power flow did not converge")

// PowerFlow solves the unbalanced four-wire power flow of one network. It is
// built once and is safe for concurrent use; each caller keeps its own
// Solution.
type PowerFlow struct {
	net *Network
	lin linear

	// Tolerance is the largest change in any node voltage between two
	// iterations at convergence, per unit of NominalVoltage.
	Tolerance float64
	// MaxIterations bounds the iteration.
	MaxIterations int

	// sourceY is the admittance of the transformer's series impedance, and
	// sourceI the current that its voltage source injects into each phase of
	// the first bus.
	sourceY complex128
	sourceE [3]complex128
	sourceI vec
	// lineY and lineB are the series admittance and half the shunt
	// susceptance of the line into each bus.
	lineY []mat
	lineB []mat
	// noLoad is the voltage profile with no customer power.
	noLoad []vec
}

// linear solves Y·v = i in place for the network's admittance matrix.
type linear interface {
	solve(rhs []vec)
}

// Solution is the state of one solved operating point, and the scratch space
// for the next solve. The zero value is ready to use.
type Solution struct {
	// V is the voltage to earth of every conductor of every bus, in the
	// network's bus order.
	V []vec
	// Iterations is the number of iterations of the last Solve.
	Iterations int

	rhs []vec
}

// NewPowerFlow assembles and factorises the admittance matrix of net.
func NewPowerFlow(net *Network, method Method) (*PowerFlow, error) {
	if err := net.Validate(); err != nil {
		return nil, err
	}
	pf := &PowerFlow{net: net, Tolerance: 1e-9, MaxIterations: 100}

	n := len(net.Buses)
	diag := make([]mat, n)
	pf.lineY = make([]mat, n)
	pf.lineB = make([]mat, n)
	singular := fmt.Errorf("%w: the admittance matrix is singular; check that the neutral has a path to earth", ErrInvalidNetwork)

	for i, b := range net.Buses {
		if b.Ground != nil {
			diag[i][Neutral][Neutral] += 1 / complex(b.Ground.ROhm, b.Ground.XOhm)
		}
		if b.Line == nil {
			continue
		}
		var z mat
		for r := range Conductors {
			for c := range Conductors {
				z[r][c] = complex(b.Line.ROhm[r][c], b.Line.XOhm[r][c])
				pf.lineB[i][r][c] = complex(0, b.Line.BS[r][c]/2)
			}
		}
		y, ok := z.inverse()
		if !ok {
			return nil, fmt.Errorf("%w: line %s has a singular impedance matrix", ErrInvalidNetwork, b.Line.Name)
		}
		pf.lineY[i] = y
		for _, end := range []int{i, b.Parent} {
			diag[end].add(&y, 1)
			diag[end].add(&pf.lineB[i], 1)
		}
	}

	// The transformer: each phase of the first bus is tied to its source
	// voltage through the series impedance. As a Norton equivalent that is
	// an admittance to earth and a current injection.
	pf.sourceY = 1 / complex(net.Source.ROhm, net.Source.XOhm)
	for ph := range 3 {
		angle := (net.Source.AngleDeg - 120*float64(ph)) * math.Pi / 180
		pf.sourceE[ph] = cmplx.Rect(net.Source.VoltageV, angle)
		diag[0][ph][ph] += pf.sourceY
		pf.sourceI[ph] = pf.sourceE[ph] * pf.sourceY
	}

	var ok bool
	if method == Dense {
		pf.lin, ok = newDense(net, diag, pf.lineY)
	} else {
		pf.lin, ok = newTree(net, diag, pf.lineY)
	}
	if !ok {
		return nil, singular
	}

	pf.noLoad = make([]vec, n)
	pf.noLoad[0] = pf.sourceI
	pf.lin.solve(pf.noLoad)
	return pf, nil
}

// Solve finds the node voltages when each site draws the complex power in
// load, in volt-amperes: a positive real part is consumption and a negative
// one is export. load is in the order of the network's sites.
//
// Loads are constant power. Each iteration turns them into current injections
// at the present voltages and solves the linear network, the method OpenDSS
// uses. A Solution that already holds voltages for this network is used as
// the starting point, which makes a run of nearby operating points cheap.
func (pf *PowerFlow) Solve(load []complex128, sol *Solution) error {
	n := len(pf.net.Buses)
	if len(load) != len(pf.net.Sites) {
		return fmt.Errorf("got %d loads for %d sites", len(load), len(pf.net.Sites))
	}
	if len(sol.V) != n {
		sol.V = make([]vec, n)
		copy(sol.V, pf.noLoad)
	}
	if len(sol.rhs) != n {
		sol.rhs = make([]vec, n)
	}

	tolerance := pf.Tolerance * NominalVoltage
	for sol.Iterations = 1; sol.Iterations <= pf.MaxIterations; sol.Iterations++ {
		clear(sol.rhs)
		sol.rhs[0] = pf.sourceI
		for k, power := range load {
			if power == 0 {
				continue
			}
			site := pf.net.Sites[k]
			v := &sol.V[site.Bus]
			// S = V·conj(I), with I flowing from the phase to the neutral
			// through the customer's installation.
			current := cmplx.Conj(power / (v[site.Phase-1] - v[Neutral]))
			sol.rhs[site.Bus][site.Phase-1] -= current
			sol.rhs[site.Bus][Neutral] += current
		}
		pf.lin.solve(sol.rhs)

		change := 0.0
		for i := range sol.rhs {
			for c := range Conductors {
				change = max(change, cmplx.Abs(sol.rhs[i][c]-sol.V[i][c]))
			}
		}
		sol.V, sol.rhs = sol.rhs, sol.V
		// A NaN never passes, so a collapse runs to MaxIterations.
		if change <= tolerance {
			return nil
		}
	}
	// Leave no half-converged or non-finite state behind as a starting point.
	sol.V = nil
	return ErrNotConverged
}

// PhaseVoltage returns the phase-to-neutral voltage magnitude of a site, in
// volts. This is the voltage across the customer's installation.
func (pf *PowerFlow) PhaseVoltage(sol *Solution, site int) float64 {
	s := pf.net.Sites[site]
	return cmplx.Abs(sol.V[s.Bus][s.Phase-1] - sol.V[s.Bus][Neutral])
}

// SourcePower returns the complex power that the transformer's source
// delivers, in volt-amperes. It includes the transformer's own losses. A
// negative real part is reverse power flow.
func (pf *PowerFlow) SourcePower(sol *Solution) complex128 {
	var s complex128
	for ph := range 3 {
		current := (pf.sourceE[ph] - sol.V[0][ph]) * pf.sourceY
		s += pf.sourceE[ph] * cmplx.Conj(current)
	}
	return s
}

// LineCurrent returns the current magnitude, in amperes, in each conductor of
// the line into bus. It is the larger of the two ends, which differ only by
// the line's charging current. bus must not be the first bus.
func (pf *PowerFlow) LineCurrent(sol *Solution, bus int) [Conductors]float64 {
	parent := pf.net.Buses[bus].Parent
	var drop vec
	for c := range Conductors {
		drop[c] = sol.V[parent][c] - sol.V[bus][c]
	}
	series := pf.lineY[bus].mulVec(&drop)
	chargeParent := pf.lineB[bus].mulVec(&sol.V[parent])
	chargeBus := pf.lineB[bus].mulVec(&sol.V[bus])

	var out [Conductors]float64
	for c := range Conductors {
		out[c] = max(cmplx.Abs(series[c]+chargeParent[c]), cmplx.Abs(series[c]-chargeBus[c]))
	}
	return out
}

// dense is the whole admittance matrix, factorised.
type dense struct {
	lu      *lu
	scratch int // 4 × buses
}

func newDense(net *Network, diag, lineY []mat) (linear, bool) {
	n := Conductors * len(net.Buses)
	a := make([]complex128, n*n)
	put := func(row, col int, m *mat, scale complex128) {
		for r := range Conductors {
			for c := range Conductors {
				a[(row*Conductors+r)*n+col*Conductors+c] += scale * m[r][c]
			}
		}
	}
	for i, b := range net.Buses {
		put(i, i, &diag[i], 1)
		if b.Line != nil {
			put(i, b.Parent, &lineY[i], -1)
			put(b.Parent, i, &lineY[i], -1)
		}
	}
	f, ok := factorise(n, a)
	return &dense{lu: f, scratch: n}, ok
}

func (d *dense) solve(rhs []vec) {
	flat := make([]complex128, d.scratch)
	for i := range rhs {
		copy(flat[i*Conductors:], rhs[i][:])
	}
	d.lu.solve(flat)
	for i := range rhs {
		copy(rhs[i][:], flat[i*Conductors:])
	}
}

// tree is a block factorisation in tree order. Eliminating a leaf changes
// only its parent's diagonal block, so the factors have the shape of the
// feeder itself.
type tree struct {
	parent []int
	// pivotInv[i] is the inverse of bus i's diagonal block after its
	// children have been eliminated.
	pivotInv []mat
	// up[i] carries bus i's right-hand side to its parent:
	// -Y(parent,i) · pivotInv[i].
	up []mat
	// lineY[i] is the series admittance of the line into bus i; the
	// off-diagonal block is its negative.
	lineY []mat
}

func newTree(net *Network, diag, lineY []mat) (linear, bool) {
	n := len(net.Buses)
	t := &tree{parent: make([]int, n), pivotInv: make([]mat, n), up: make([]mat, n), lineY: lineY}
	pivot := make([]mat, n)
	copy(pivot, diag)
	// Children come after their parents, so walking backwards eliminates
	// every bus before its parent.
	for i := n - 1; i >= 0; i-- {
		inv, ok := pivot[i].inverse()
		if !ok {
			return nil, false
		}
		t.pivotInv[i] = inv
		p := net.Buses[i].Parent
		t.parent[i] = p
		if p < 0 {
			continue
		}
		// Off-diagonal blocks are -lineY[i]; the two signs cancel in the
		// Schur complement.
		t.up[i] = lineY[i].mul(&inv)
		schur := t.up[i].mul(&lineY[i])
		pivot[p].add(&schur, -1)
	}
	return t, true
}

func (t *tree) solve(rhs []vec) {
	for i := len(rhs) - 1; i > 0; i-- {
		carried := t.up[i].mulVec(&rhs[i])
		p := t.parent[i]
		for c := range Conductors {
			rhs[p][c] += carried[c]
		}
	}
	rhs[0] = t.pivotInv[0].mulVec(&rhs[0])
	for i := 1; i < len(rhs); i++ {
		coupled := t.lineY[i].mulVec(&rhs[t.parent[i]])
		for c := range Conductors {
			rhs[i][c] += coupled[c]
		}
		rhs[i] = t.pivotInv[i].mulVec(&rhs[i])
	}
}
