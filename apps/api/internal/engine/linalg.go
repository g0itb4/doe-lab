package engine

import "math/cmplx"

// vec is one complex quantity per conductor of a bus: a voltage to earth, or
// a current injected into the bus.
type vec [Conductors]complex128

// mat is a conductor-by-conductor block of the admittance matrix.
type mat [Conductors][Conductors]complex128

func (m *mat) mulVec(v *vec) vec {
	var out vec
	for i := range Conductors {
		var sum complex128
		for j := range Conductors {
			sum += m[i][j] * v[j]
		}
		out[i] = sum
	}
	return out
}

func (m *mat) mul(o *mat) mat {
	var out mat
	for i := range Conductors {
		for j := range Conductors {
			var sum complex128
			for k := range Conductors {
				sum += m[i][k] * o[k][j]
			}
			out[i][j] = sum
		}
	}
	return out
}

func (m *mat) add(o *mat, scale complex128) {
	for i := range Conductors {
		for j := range Conductors {
			m[i][j] += scale * o[i][j]
		}
	}
}

// singularRatio is how small a pivot may be, against the largest entry of the
// matrix, before the matrix counts as singular.
const singularRatio = 1e-12

// inverse returns the inverse by Gauss-Jordan elimination with partial
// pivoting. ok is false when the block is singular.
func (m *mat) inverse() (inv mat, ok bool) {
	a := *m
	scale := 0.0
	for i := range Conductors {
		inv[i][i] = 1
		for j := range Conductors {
			scale = max(scale, cmplx.Abs(a[i][j]))
		}
	}
	for col := range Conductors {
		pivot := col
		for row := col + 1; row < Conductors; row++ {
			if cmplx.Abs(a[row][col]) > cmplx.Abs(a[pivot][col]) {
				pivot = row
			}
		}
		if cmplx.Abs(a[pivot][col]) <= singularRatio*scale {
			return mat{}, false
		}
		a[col], a[pivot] = a[pivot], a[col]
		inv[col], inv[pivot] = inv[pivot], inv[col]

		p := 1 / a[col][col]
		for j := range Conductors {
			a[col][j] *= p
			inv[col][j] *= p
		}
		for row := range Conductors {
			f := a[row][col]
			if row == col || f == 0 {
				continue
			}
			for j := range Conductors {
				a[row][j] -= f * a[col][j]
				inv[row][j] -= f * inv[col][j]
			}
		}
	}
	return inv, true
}

// lu is a dense LU factorisation with partial pivoting, stored row-major.
type lu struct {
	n    int
	a    []complex128
	perm []int
}

// factorise overwrites a, an n×n row-major matrix, with its LU factors. ok is
// false when the matrix is singular.
func factorise(n int, a []complex128) (f *lu, ok bool) {
	scale := 0.0
	for _, v := range a {
		scale = max(scale, cmplx.Abs(v))
	}
	perm := make([]int, n)
	for col := range n {
		pivot := col
		for row := col + 1; row < n; row++ {
			if cmplx.Abs(a[row*n+col]) > cmplx.Abs(a[pivot*n+col]) {
				pivot = row
			}
		}
		if cmplx.Abs(a[pivot*n+col]) <= singularRatio*scale {
			return nil, false
		}
		perm[col] = pivot
		if pivot != col {
			for j := range n {
				a[col*n+j], a[pivot*n+j] = a[pivot*n+j], a[col*n+j]
			}
		}
		p := 1 / a[col*n+col]
		for row := col + 1; row < n; row++ {
			f := a[row*n+col] * p
			if f == 0 {
				continue
			}
			a[row*n+col] = f
			rowSlice := a[row*n+col+1 : row*n+n]
			pivotSlice := a[col*n+col+1 : col*n+n]
			for j, v := range pivotSlice {
				rowSlice[j] -= f * v
			}
		}
	}
	return &lu{n: n, a: a, perm: perm}, true
}

// solve overwrites b with the solution of A·x = b.
func (f *lu) solve(b []complex128) {
	n := f.n
	for i, p := range f.perm {
		b[i], b[p] = b[p], b[i]
	}
	for i := 1; i < n; i++ {
		var sum complex128
		for j, v := range f.a[i*n : i*n+i] {
			sum += v * b[j]
		}
		b[i] -= sum
	}
	for i := n - 1; i >= 0; i-- {
		sum := b[i]
		for j := i + 1; j < n; j++ {
			sum -= f.a[i*n+j] * b[j]
		}
		b[i] = sum / f.a[i*n+i]
	}
}
