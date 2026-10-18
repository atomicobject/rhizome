package indexingperf

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReportsDoNotReorderOrConsumeSnapshotMeasurements(t *testing.T) {
	c := New()
	ctx := WithPhase(WithCollector(context.Background(), c), "index_code")
	ObserveLatency(ctx, CodeindexWorkerTotal, 9*time.Millisecond)
	ObserveLatency(ctx, CodeindexWorkerTotal, time.Millisecond)
	before := c.Snapshot()
	require.NotEmpty(t, c.RenderWindow(time.Second))
	require.NotEmpty(t, c.RenderSummary())
	ObserveLatency(ctx, CodeindexWorkerTotal, 5*time.Millisecond)
	after := c.Snapshot()
	require.Equal(t, int64(10*time.Millisecond), before.Latencies[0].Total)
	require.Equal(t, int64(15*time.Millisecond), after.Latencies[0].Total)
	require.Equal(t, int64(5*time.Millisecond), after.Latencies[0].P50)
	require.Equal(t, int64(9*time.Millisecond), after.Latencies[0].P95)
	require.Equal(t, []time.Duration{9 * time.Millisecond, time.Millisecond, 5 * time.Millisecond}, c.latencies[Metric{Phase: "index_code", Name: CodeindexWorkerTotal, Kind: kindLatency}].Samples)
}

func TestRendererGoldens(t *testing.T) {
	for _, renderer := range []string{"window", "summary"} {
		t.Run(renderer, func(t *testing.T) {
			c := New()
			ctx := WithPhase(WithCollector(context.Background(), c), "embed_notes")
			AddCount(WithPhase(ctx, "custom_phase"), OwnershipReconciliationAcknowledged, 1)
			AddCount(ctx, ProviderCalls, 2)
			AddCount(ctx, ProviderTexts, 6)
			ObserveLatency(ctx, ProviderLatency, 40*time.Millisecond)
			ObserveSample(ctx, ProviderBatchTexts, 3)
			ObserveSample(ctx, ProviderCapTexts, 4)
			ObserveSample(ctx, ProviderDispatchFloorTexts, 2)
			ObserveLatency(ctx, WritebackMetric.Select("noteembed.writeback.intel", "submit_wait"), 5*time.Millisecond)
			AddCount(ctx, WritebackMetric.Select("noteembed.writeback.intel", "flush.count"), 1)
			SetGauge(ctx, ProviderInflight, 2)
			at := time.Unix(100, 0)
			c.RecordSpanWindow("embed_notes", at, at.Add(time.Second), nil)
			actual := c.RenderSummary()
			if renderer == "window" {
				actual = c.RenderWindow(time.Second)
			}
			expected, err := os.ReadFile(filepath.Join("testdata", renderer+".golden"))
			require.NoError(t, err)
			require.Equal(t, string(expected), actual+"\n")
		})
	}
}
