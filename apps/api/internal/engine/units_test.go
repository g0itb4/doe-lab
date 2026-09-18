package engine

import "testing"

func TestPerUnit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		volts float64
		want  float64
	}{
		{name: "nominal", volts: 230, want: 1},
		{name: "upper limit", volts: 253, want: 1.1},
		{name: "zero", volts: 0, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := PerUnit(tt.volts); got != tt.want {
				t.Errorf("PerUnit(%v) = %v, want %v", tt.volts, got, tt.want)
			}
		})
	}
}
