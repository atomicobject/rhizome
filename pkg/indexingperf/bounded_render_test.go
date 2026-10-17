package indexingperf

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBoundedWindowsRetainNewDetailAfterCumulativeLimit(t *testing.T) {
	c := New(Limits{SamplesPerMetric: 2, IntervalsPerMetric: 2})
	ctx := WithPhase(WithCollector(context.Background(), c), "embed_notes")
	at := time.Unix(100, 0)
	for i := 1; i <= 3; i++ {
		ObserveLatency(ctx, ProviderLatency, time.Duration(i)*time.Second)
		ObserveSample(ctx, ProviderBatchTexts, int64(i))
		ObserveInterval(ctx, ProviderLatency, at, time.Duration(i)*time.Second)
	}
	snapshot := c.Snapshot()
	require.Equal(t, int64(2), snapshot.Coverage.DroppedSamples)
	require.Equal(t, int64(1), snapshot.Coverage.DroppedIntervals)
	require.Equal(t, "retained_prefix", snapshot.Latencies[0].QuantileCoverage)
	require.Equal(t, int64(2), c.windowDroppedSamples)
	require.NotEmpty(t, c.RenderWindow(time.Second))
	require.Zero(t, c.windowDroppedSamples)
	require.Empty(t, c.windowLatencies)
	require.Empty(t, c.windowSamples)
	ObserveLatency(ctx, ProviderLatency, 4*time.Second)
	ObserveSample(ctx, ProviderBatchTexts, 4)
	c.mu.Lock()
	_, p95, peak := c.windowLatencyWindowPercentilesPhase("embed_notes", ProviderLatency)
	_, sampleP95, samplePeak := c.windowSamplePercentilesPhase("embed_notes", ProviderBatchTexts)
	c.mu.Unlock()
	require.Equal(t, 4*time.Second, p95)
	require.Equal(t, 4*time.Second, peak)
	require.Equal(t, int64(4), sampleP95)
	require.Equal(t, int64(4), samplePeak)
	require.Equal(t, int64(4), c.Snapshot().Latencies[0].Count)
	require.NotEmpty(t, c.RenderWindow(time.Second))
	require.Empty(t, c.windowLatencies)
	require.Empty(t, c.windowSamples)
}
