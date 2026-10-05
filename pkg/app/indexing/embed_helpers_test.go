package indexing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type recordingProgressBar struct {
	updates [][2]int
}

func (r *recordingProgressBar) Println(string) {}

func (r *recordingProgressBar) Update(done, total int) {
	r.updates = append(r.updates, [2]int{done, total})
}

func TestEmbedAggregatorSuppressesZeroDoneTotalChurn(t *testing.T) {
	t.Parallel()

	bar := &recordingProgressBar{}
	agg := &embedAggregator{bar: bar}
	code := agg.forPhase("code")

	code(0, 10)
	code(0, 11)
	code(0, 12)
	code(3, 12)

	require.Equal(t, [][2]int{
		{0, 10},
		{3, 12},
	}, bar.updates)
}
