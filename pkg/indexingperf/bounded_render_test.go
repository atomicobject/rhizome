package indexingperf

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBoundedWindowReportsFreshSamplesAfterCumulativeLimit(t *testing.T) {
	c := NewBounded()
	ctx := WithPhase(WithCollector(context.Background(), c), "embed_notes")
	at := time.Unix(100, 0)
	for i := 0; i < 256; i++ {
		AddCount(ctx, "provider.calls", 1)
		ObserveLatency(ctx, "provider.latency", time.Millisecond)
		ObserveSample(ctx, "provider.batch_texts", 1)
		ObserveInterval(ctx, "provider.latency", at.Add(time.Duration(i)*time.Millisecond), time.Millisecond)
	}
	c.RecordSpanWindow("embed_notes", at, at.Add(3*time.Second), nil)
	require.NotEmpty(t, c.RenderWindow(time.Second))
	AddCount(ctx, "provider.calls", 1)
	ObserveLatency(ctx, "provider.latency", 2*time.Second)
	ObserveSample(ctx, "provider.batch_texts", 100)
	ObserveInterval(ctx, "provider.latency", at.Add(time.Second), 2*time.Second)
	_ = c.Snapshot()
	_ = c.Snapshot()
	window := c.RenderWindow(time.Second)
	require.Contains(t, window, "provider_latency_p95_ms=2000")
	require.Contains(t, window, "batch_texts_p95=100")
	require.Contains(t, c.RenderSummary(), "cumulative quantiles use retained prefixes")
	require.Contains(t, c.RenderSummary(), "interval walls and overlaps cover retained intervals")
	require.Contains(t, c.RenderSummary(), "dropped_samples=2")
	require.Contains(t, c.RenderSummary(), "dropped_intervals=193")
}

func TestBoundedWindowMarksOwnOverflowAndResetsDetail(t *testing.T) {
	c := NewBounded(Limits{SamplesPerMetric: 2})
	ctx := WithPhase(WithCollector(context.Background(), c), "embed_notes")
	for i := 1; i <= 3; i++ {
		AddCount(ctx, "provider.calls", 1)
		ObserveLatency(ctx, "provider.latency", time.Duration(i)*time.Second)
		ObserveSample(ctx, "provider.batch_texts", int64(i))
	}
	require.Len(t, c.windowLatencies[Metric{Phase: "embed_notes", Name: "provider.latency", Kind: kindLatency}].Samples, 2)
	require.Len(t, c.windowSamples[Metric{Phase: "embed_notes", Name: "provider.batch_texts", Kind: kindSample}].Samples, 2)
	window := c.RenderWindow(time.Second)
	require.Contains(t, window, "window quantiles use retained prefixes")
	require.Contains(t, window, "window_dropped_samples=2")
	ObserveLatency(ctx, "provider.latency", 4*time.Second)
	ObserveSample(ctx, "provider.batch_texts", 4)
	window = c.RenderWindow(time.Second)
	require.Contains(t, window, "provider_latency_p95_ms=4000")
	require.Contains(t, window, "batch_texts_p95=4")
	require.NotContains(t, window, "window_dropped_samples")
	require.Equal(t, int64(4), c.Snapshot().Latencies[0].Count)
	require.Len(t, c.windowLatencies, 0)
	require.Len(t, c.windowSamples, 0)
}

func TestBoundedCompleteRenderingMatchesLegacyBytes(t *testing.T) {
	bounded, legacy := NewBounded(), New()
	at := time.Unix(100, 0)
	for window := 0; window < 2; window++ {
		for _, c := range []*Collector{bounded, legacy} {
			ctx := WithPhase(WithCollector(context.Background(), c), "embed_notes")
			for i := 1; i <= 3; i++ {
				AddCount(ctx, "provider.calls", 1)
				ObserveLatency(ctx, "provider.latency", time.Duration(i)*time.Millisecond)
				ObserveSample(ctx, "provider.batch_texts", int64(i))
				ObserveInterval(ctx, "provider.latency", at.Add(time.Duration(window)*time.Second), time.Duration(i)*time.Millisecond)
			}
			c.RecordSpanWindow("embed_notes", at, at.Add(time.Second), nil)
		}
		require.Equal(t, legacy.RenderWindow(time.Second), bounded.RenderWindow(time.Second))
	}
	require.Equal(t, legacy.RenderSummary(), bounded.RenderSummary())
}
