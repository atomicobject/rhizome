package indexingperf

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSnapshotDoesNotConsumeWindowsOrExposeMutableState(t *testing.T) {
	c, control := New(), New()
	at := time.Unix(100, 0)
	c.startedAt, control.startedAt = at, at
	for _, collector := range []*Collector{c, control} {
		ctx := WithPhase(WithCollector(context.Background(), collector), "embed_notes")
		AddCount(ctx, "provider.calls", 2)
		SetGauge(ctx, "provider.inflight", 1)
		ObserveLatency(ctx, "provider.latency", time.Second)
		ObserveSample(ctx, "provider.batch_texts", 3)
		ObserveInterval(ctx, "provider.latency", at, time.Second)
		collector.RecordSpanWindow("embed_notes", at, at.Add(time.Second), nil)
		ObserveDBWrite(WithOp(ctx, "flush"), "notes", time.Millisecond, 2*time.Millisecond)
	}
	before := c.Snapshot()
	encoded, err := json.Marshal(before)
	require.NoError(t, err)
	again, err := json.Marshal(c.Snapshot())
	require.NoError(t, err)
	require.Equal(t, encoded, again)
	before.Counters[0].Total = -1
	before.Spans[0].DurationNs = -1
	before.Intervals[0].RetainedUnionNs = -1
	require.Equal(t, control.Snapshot(), c.Snapshot())
	require.Equal(t, control.RenderWindow(time.Second), c.RenderWindow(time.Second))
	require.Equal(t, control.RenderSummary(), c.RenderSummary())
}

func TestBoundedSnapshotRetainsExactTotalsAndMarksPartialDetail(t *testing.T) {
	c := NewBounded(Limits{Series: 20, SamplesPerMetric: 3, IntervalsPerMetric: 2, LabelBytes: 30})
	ctx := WithPhase(WithCollector(context.Background(), c), "work")
	at := time.Unix(100, 0)
	for i := 1; i <= 100; i++ {
		AddCount(ctx, "completed", 1)
		SetGauge(ctx, "pending", int64(i))
		ObserveLatency(ctx, "duration", time.Duration(i))
		ObserveSample(ctx, "batch", int64(i))
		ObserveInterval(ctx, "duration", at.Add(time.Duration(i)*time.Second), time.Second)
		c.RecordSpanWindow("work", at, at.Add(time.Duration(i)), nil)
	}
	MarkCountAvailable(ctx, "observed_zero")
	snapshot := c.Snapshot()
	require.Equal(t, int64(100), snapshot.Counters[0].Total)
	require.Equal(t, int64(0), snapshot.Counters[1].Total)
	require.Equal(t, int64(100), snapshot.Gauges[0].Current)
	require.Equal(t, int64(100), snapshot.Gauges[0].Peak)
	require.Equal(t, int64(5050), snapshot.Latencies[0].Total)
	require.Equal(t, int64(100), snapshot.Latencies[0].Max)
	require.Equal(t, int64(2), snapshot.Latencies[0].P50)
	require.Equal(t, "retained_prefix", snapshot.Latencies[0].QuantileCoverage)
	require.Equal(t, 3, snapshot.Latencies[0].RetainedSamples)
	require.Equal(t, int64(5050), snapshot.Samples[0].Total)
	require.Equal(t, int64(100), snapshot.Spans[0].Count)
	require.Equal(t, int64(5050), snapshot.Spans[0].DurationNs)
	require.Equal(t, 2, snapshot.Spans[0].RetainedIntervals)
	require.Equal(t, "retained_prefix", snapshot.Spans[0].IntervalCoverage)
	require.Equal(t, int64(100), snapshot.Intervals[0].Count)
	require.Equal(t, int64(2*time.Second), snapshot.Intervals[0].RetainedUnionNs)
	require.Equal(t, int64(194), snapshot.Coverage.DroppedSamples)
	require.Equal(t, int64(196), snapshot.Coverage.DroppedIntervals)
}

func TestBoundedCollectorCapsAllSeriesAndLabelStorage(t *testing.T) {
	c := NewBounded(Limits{Series: 4, SamplesPerMetric: 2, IntervalsPerMetric: 2, LabelBytes: 20})
	ctx := WithCollector(context.Background(), c)
	AddCount(ctx, "admitted", 1)
	for i := 0; i < 1000; i++ {
		label := fmt.Sprintf("series%d", i)
		AddCount(ctx, label, 1)
		SetGauge(ctx, label, 1)
		ObserveSample(ctx, label, 1)
		ObserveLatency(ctx, label, time.Second)
		ObserveInterval(ctx, label, time.Now(), time.Second)
		c.RecordSpan(label, time.Second, nil)
		ObserveDBWrite(WithOp(ctx, label), "store", time.Second, time.Second)
		ObserveProviderFingerprint(ctx, fmt.Sprintf("%064x", i))
	}
	AddCount(ctx, strings.Repeat("x", 21), 1)
	AddCount(ctx, "admitted", 1)
	snapshot := c.Snapshot()
	require.Len(t, c.series, 4)
	require.Greater(t, snapshot.Coverage.RejectedObservations, int64(0))
	require.LessOrEqual(t, len(c.counters)+len(c.gauges)+len(c.latencies)+len(c.samples)+len(c.spans)+len(c.dbWrites)+len(c.providerFingerprints), 4)
	require.Equal(t, int64(2), snapshot.Counters[0].Total)
}

func TestSnapshotConcurrentObservationsRemainOperationScoped(t *testing.T) {
	c, other := NewBounded(), NewBounded()
	ctx := WithCollector(context.Background(), c)
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				AddCount(ctx, "completed", 1)
				ObserveLatency(ctx, "duration", time.Millisecond)
				_ = c.Snapshot()
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int64(800), c.Snapshot().Counters[0].Total)
	require.Empty(t, other.Snapshot().Counters)
}
