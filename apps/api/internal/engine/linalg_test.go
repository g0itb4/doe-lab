package engine

import (
	"math/cmplx"
	"testing"
)

func TestMatInverse(t *testing.T) {
	t.Parallel()

	// A zero on the diagonal forces a row exchange.
	m := mat{
		{0, 2, 0, 1i},
		{1, 0, 3, 0},
		{0, 1 + 1i, 1, 0},
		{2, 0, 0, 4},
	}
	inv, ok := m.inverse()
	if !ok {
		t.Fatal("inverse reported a singular matrix")
	}
	product := m.mul(&inv)
	for i := range Conductors {
		for j := range Conductors {
			want := complex128(0)
			if i == j {
				want = 1
			}
			if cmplx.Abs(product[i][j]-want) > 1e-12 {
				t.Errorf("(m·inv)[%d][%d] = %v, want %v", i, j, product[i][j], want)
			}
		}
	}

	// Two equal rows.
	singular := mat{{1, 2, 3, 4}, {1, 2, 3, 4}, {0, 1, 0, 0}, {0, 0, 1, 0}}
	if _, ok := singular.inverse(); ok {
		t.Error("inverse of a singular matrix reported ok")
	}
	if _, ok := (&mat{}).inverse(); ok {
		t.Error("inverse of the zero matrix reported ok")
	}
}

func TestMatArithmetic(t *testing.T) {
	t.Parallel()

	m := mat{{1, 2, 0, 0}, {0, 1, 0, 0}, {0, 0, 2, 0}, {0, 0, 0, 1i}}
	v := vec{1, 1, 1, 1}
	if got, want := m.mulVec(&v), (vec{3, 1, 2, 1i}); got != want {
		t.Errorf("mulVec = %v, want %v", got, want)
	}
	sum := m
	sum.add(&m, -0.5)
	if sum[0][1] != 1 || sum[3][3] != 0.5i {
		t.Errorf("add = %v", sum)
	}
}

func TestFactorise(t *testing.T) {
	t.Parallel()

	// x = (1, 2i, -1): the first pivot is zero, so rows must be exchanged.
	a := []complex128{
		0, 1, 2,
		1, 0, 1,
		2, 1, 0,
	}
	b := []complex128{2i - 2, 0, 2 + 2i}
	f, ok := factorise(3, a)
	if !ok {
		t.Fatal("factorise reported a singular matrix")
	}
	f.solve(b)
	for i, want := range []complex128{1, 2i, -1} {
		if cmplx.Abs(b[i]-want) > 1e-12 {
			t.Errorf("x[%d] = %v, want %v", i, b[i], want)
		}
	}

	if _, ok := factorise(2, []complex128{1, 2, 2, 4}); ok {
		t.Error("factorise of a singular matrix reported ok")
	}
}
