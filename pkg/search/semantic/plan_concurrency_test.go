package semantic

import (
	"math"
	"testing"
)

func TestCalcPlanConcurrency(t *testing.T) {
	tests := []struct {
		name  string
		procs int
		want  int
	}{
		{name: "zero", procs: 0, want: 1},
		{name: "one", procs: 1, want: 2},
		{name: "many", procs: 6, want: 12},
		{name: "overflow clamp", procs: math.MaxInt, want: math.MaxInt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := calcPlanConcurrency(tt.procs); got != tt.want {
				t.Fatalf("calcPlanConcurrency(%d) = %d, want %d", tt.procs, got, tt.want)
			}
		})
	}
}
