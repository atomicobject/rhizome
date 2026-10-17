package indexingperf

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCollectorSnapshotAccumulatesMeasurementsAndAvailableZero(t *testing.T) {
	c := New()
	ctx := WithPhase(WithCollector(context.Background(), c), "embed_notes")
	AddCount(ctx, ProviderCalls, 2)
	AddCount(ctx, ProviderCalls, 3)
	MarkCountAvailable(ctx, AgentStartOpNoteReads)
	SetGauge(ctx, ProviderInflight, 3)
	SetGauge(ctx, ProviderInflight, 1)
	ObserveLatency(ctx, ProviderLatency, 9*time.Millisecond)
	ObserveLatency(ctx, ProviderLatency, time.Millisecond)
	ObserveSample(ctx, ProviderBatchTexts, 4)
	ObserveSample(ctx, ProviderBatchTexts, 2)
	at := time.Unix(100, 0)
	c.RecordSpanWindow("embed_notes", at, at.Add(time.Second), nil)
	c.RecordSpanWindow("embed_notes", at.Add(time.Second), at.Add(2*time.Second), errors.New("failed"))
	snapshot := c.Snapshot()
	require.Equal(t, []CounterSnapshot{
		{MetricIdentity: MetricIdentity{Phase: "embed_notes", Name: AgentStartOpNoteReads}},
		{MetricIdentity: MetricIdentity{Phase: "embed_notes", Name: string(ProviderCalls)}, Count: 2, Total: 5, Max: 3},
	}, snapshot.Counters)
	require.Equal(t, []GaugeSnapshot{{MetricIdentity: MetricIdentity{Phase: "embed_notes", Name: string(ProviderInflight)}, Current: 1, Peak: 3, Count: 2, Total: 4}}, snapshot.Gauges)
	require.Equal(t, int64(10*time.Millisecond), snapshot.Latencies[0].Total)
	require.Equal(t, int64(time.Millisecond), snapshot.Latencies[0].P50)
	require.Equal(t, int64(9*time.Millisecond), snapshot.Latencies[0].P95)
	require.Equal(t, int64(6), snapshot.Samples[0].Total)
	require.Equal(t, int64(2), snapshot.Spans[0].Count)
	require.Equal(t, int64(2*time.Second), snapshot.Spans[0].DurationNs)
	require.Equal(t, "error", snapshot.Spans[0].Status)
	require.Equal(t, 1024, snapshot.Limits.Series)
}

func TestCollectorWindowsConsumeOnlyTheirOwnDeltas(t *testing.T) {
	c := New()
	ctx := WithPhase(WithCollector(context.Background(), c), "embed_notes")
	AddCount(ctx, ProviderCalls, 2)
	require.NotEmpty(t, c.RenderWindow(time.Second))
	require.Empty(t, c.RenderWindow(time.Second))
	AddCount(ctx, ProviderCalls, 3)
	c.mu.Lock()
	require.Equal(t, int64(3), c.windowCounterDeltaPhase("embed_notes", ProviderCalls))
	c.mu.Unlock()
	require.Equal(t, int64(5), c.Snapshot().Counters[0].Total)
	require.NotEmpty(t, c.RenderWindow(time.Second))
	require.Empty(t, c.RenderWindow(time.Second))
}

func TestCollectorWindowPrioritizesProviderAndIncludesCounterOnlyPhases(t *testing.T) {
	c := New()
	ctx := WithCollector(context.Background(), c)
	ObserveLatency(WithPhase(ctx, "index_code"), FsWalk, time.Second)
	AddCount(WithPhase(ctx, "embed_notes"), ProviderCalls, 1)
	AddCount(WithPhase(ctx, "custom_phase"), OwnershipReconciliationAcknowledged, 1)
	require.Equal(t, "embed_notes", c.DominantWindowPhase())
	c.mu.Lock()
	phases := c.windowPhases(3)
	c.mu.Unlock()
	require.ElementsMatch(t, []string{"index_code", "embed_notes", "custom_phase"}, phases)
}

func unlabeledDBWrite(ctx context.Context) {
	ObserveDBWrite(ctx, "intel", time.Millisecond, 2*time.Millisecond)
}

func TestObserveDBWriteInfersCallerOpWhenMissing(t *testing.T) {
	c := New()
	unlabeledDBWrite(WithCollector(context.Background(), c))
	snapshot := c.Snapshot()
	require.Len(t, snapshot.DBWrites, 1)
	require.Equal(t, "auto.indexingperf.unlabeledDBWrite", snapshot.DBWrites[0].Op)
	require.Equal(t, int64(time.Millisecond), snapshot.DBWrites[0].WaitNs)
	require.Equal(t, int64(2*time.Millisecond), snapshot.DBWrites[0].HoldNs)
}
