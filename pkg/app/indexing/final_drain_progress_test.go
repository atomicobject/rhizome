package indexing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFinalDrainProgressAccumulatesPlannerAndRebuildWork(t *testing.T) {
	t.Parallel()

	base := &progressRecorder{}
	progress := newIntegratedProgress(base, false)
	progress.SetSegment(mainPhaseEnd, finalDrainEnd)

	drain := newFinalDrainProgress(progress)
	drain.Start(3)
	drain.Advance(1)
	drain.Start(2)
	drain.Advance(2)
	drain.Advance(2)

	got := make([]int, 0, len(base.updates))
	for _, update := range base.updates {
		got = append(got, update[0])
	}
	require.Equal(t, []int{0, 950, 950, 959, 959, 966, 978}, got)
}
