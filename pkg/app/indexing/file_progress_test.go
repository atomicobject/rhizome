package indexing

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/indexingpipe"
	"github.com/stretchr/testify/require"
)

func TestFixedTotalFileProgressCountsEdgeWorkDynamically(t *testing.T) {
	t.Parallel()

	bar := &recordingProgressBar{}
	progress := newFixedTotalFileProgress(bar, 2)

	progress.onCompleted(indexingpipe.FileCandidate{})
	progress.AddEdgeWork([]string{"pkg/a.go", "pkg/b.go", "pkg/a.go"})
	progress.CompleteEdgeWork([]string{"pkg/b.go", "pkg/a.go", "pkg/b.go"})

	require.Equal(t, [][2]int{
		{1, 2},
		{1, 4},
		{3, 4},
	}, bar.updates)
}

func TestMainPhaseProgressDoesNotRegressWhenEdgeWorkIsDiscovered(t *testing.T) {
	t.Parallel()

	base := &progressRecorder{}
	progress := newIntegratedProgress(base, false)
	progress.SetSegment(0, mainPhaseEnd)

	work := newFixedTotalFileProgress(progress, 2)
	work.onCompleted(indexingpipe.FileCandidate{})
	work.AddEdgeWork([]string{"pkg/a.go", "pkg/b.go"})
	work.CompleteEdgeWork([]string{"pkg/a.go"})
	work.onCompleted(indexingpipe.FileCandidate{})
	work.CompleteEdgeWork([]string{"pkg/b.go"})

	got := make([]int, 0, len(base.updates))
	for _, update := range base.updates {
		got = append(got, update[0])
	}
	require.Equal(t, []int{0, 475, 475, 475, 712, 950}, got)
	for i := 1; i < len(got); i++ {
		require.GreaterOrEqual(t, got[i], got[i-1])
	}
}

func TestFixedTotalFileProgressCountsScopeWorkDynamically(t *testing.T) {
	t.Parallel()

	bar := &recordingProgressBar{}
	progress := newFixedTotalFileProgress(bar, 1)

	progress.onCompleted(indexingpipe.FileCandidate{})
	progress.AddScopeWork([]int64{10, 11, 10})
	progress.CompleteScopeWork([]int64{11, 10, 11})

	require.Equal(t, [][2]int{
		{1, 1},
		{1, 3},
		{3, 3},
	}, bar.updates)
}
