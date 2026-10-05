package semanticruntime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPolicyForFullScanUsesThroughputPackers(t *testing.T) {
	policy := PolicyFor(IndexRequest{Source: IndexSourceFullScan, LatencyClass: LatencyAuto})

	require.Equal(t, 256, policy.CodeEmbedPacker.MinTexts)
	require.Equal(t, 256, policy.CodeEmbedPacker.AdaptiveMinTexts)
	require.Equal(t, 512, policy.CodeEmbedPacker.MaxTexts)
	require.Equal(t, 384<<10, policy.CodeEmbedPacker.MaxBytes)
	require.Equal(t, 256, policy.NoteEmbedPacker.MinTexts)
	require.Equal(t, 512, policy.NoteEmbedPacker.AdaptiveMinTexts)
	require.Equal(t, 512, policy.NoteEmbedPacker.MaxTexts)
	require.Equal(t, 384<<10, policy.NoteEmbedPacker.MaxBytes)
}

func TestPolicyForWatcherKeepsSmallBatchesUntilBurst(t *testing.T) {
	low := PolicyFor(IndexRequest{Source: IndexSourceWatcher, LatencyClass: LatencyAuto, PathCount: 2})
	require.Equal(t, 8, low.CodeEmbedPacker.MinTexts)
	require.Equal(t, 128, low.CodeEmbedPacker.MaxTexts)

	burst := PolicyFor(IndexRequest{Source: IndexSourceWatcher, LatencyClass: LatencyAuto, PathCount: 32})
	require.Equal(t, 64, burst.NoteEmbedPacker.MinTexts)
	require.Equal(t, 256, burst.NoteEmbedPacker.MaxTexts)
}
