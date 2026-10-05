package indexingperf

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCollectorReportsKeepWindowSamplesInRecordingOrder(t *testing.T) {
	t.Parallel()

	collector := New()
	ctx := WithPhase(WithCollector(context.Background(), collector), "code")
	ObserveLatency(ctx, "codeindex.worker_total", 9*time.Millisecond)
	ObserveLatency(ctx, "codeindex.worker_total", time.Millisecond)
	AddCount(ctx, "calledge.direct_paths", 2)

	require.Equal(t, "[index-perf] window=1s phase=code worker_total_cum_ms=10 worker_total_p50_ms=1 worker_total_p95_ms=9 worker_total_max_ms=9 calledge_direct_paths=2", collector.RenderWindow(time.Second))

	ObserveLatency(ctx, "codeindex.worker_total", 5*time.Millisecond)
	AddCount(ctx, "calledge.direct_paths", 3)
	require.Equal(t, "[index-perf] window=1s phase=code worker_total_cum_ms=5 worker_total_p50_ms=5 worker_total_p95_ms=5 worker_total_max_ms=5 calledge_direct_paths=3", collector.RenderWindow(time.Second))
	require.Equal(t, "calledge_direct_paths=5 worker_total_cum=15ms worker_total_p50=5ms worker_total_p95=9ms worker_total_max=9ms", strings.Join(collector.phaseSummaryParts("code"), " "))
	require.Empty(t, collector.RenderWindow(time.Second))
}

func TestCollectorReportsKeepPhaseAndMetricKindsSeparate(t *testing.T) {
	t.Parallel()

	collector := New()
	ctx := WithPhase(WithCollector(context.Background(), collector), "code")
	ObserveLatency(ctx, "codeindex.worker_total", 150*time.Microsecond)
	ObserveSample(ctx, "codeindex.worker_total", 999)
	AddCount(ctx, "codeindex.worker_total", 999)
	SetGauge(ctx, "codeindex.worker_total", 999)
	ObserveLatency(WithPhase(ctx, "notes"), "codeindex.worker_total", 100*time.Millisecond)

	// Sub-millisecond work remains in the cumulative report but does not activate
	// a rolling window whose duration fields are whole milliseconds.
	require.Empty(t, collector.renderWindowPhaseLine(time.Second, "code"))
	require.Equal(t, "worker_total_cum=0ms worker_total_p50=0ms worker_total_p95=0ms worker_total_max=0ms", strings.Join(collector.phaseSummaryParts("code"), " "))
	require.Equal(t, "worker_total_cum=100ms worker_total_p50=100ms worker_total_p95=100ms worker_total_max=100ms", strings.Join(collector.phaseSummaryParts("notes"), " "))
}
