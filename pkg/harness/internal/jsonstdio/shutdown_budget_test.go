package jsonstdio

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCloseGracePeriodReservesKillAndReapBudget(t *testing.T) {
	for _, tc := range []struct {
		name      string
		remaining time.Duration
		want      time.Duration
	}{
		{name: "expired", remaining: -time.Second, want: 0},
		{name: "short deadline", remaining: 300 * time.Millisecond, want: 0},
		{name: "cleanup budget only", remaining: time.Second, want: 0},
		{name: "reserve cleanup", remaining: 1500 * time.Millisecond, want: 500 * time.Millisecond},
		{name: "default deadline", remaining: 3 * time.Second, want: 1500 * time.Millisecond},
		{name: "ample budget", remaining: 10 * time.Second, want: 2 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, closeGracePeriod(tc.remaining))
		})
	}
}
