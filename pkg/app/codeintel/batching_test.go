package codeintel

import "testing"

func TestClampIndexWorkers(t *testing.T) {
	tests := []struct {
		in   int
		want int
	}{
		{in: 0, want: 2},
		{in: 1, want: 2},
		{in: 2, want: 4},
		{in: 4, want: 8},
		{in: 8, want: 16},
		{in: 16, want: 32},
		{in: 24, want: 32},
		{in: 64, want: 32},
	}

	for _, tt := range tests {
		if got := ClampIndexWorkers(tt.in); got != tt.want {
			t.Fatalf("ClampIndexWorkers(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
