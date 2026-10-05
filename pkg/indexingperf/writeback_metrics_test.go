package indexingperf

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWritebackMetricDescriptorsPreserveDetailedOutput(t *testing.T) {
	t.Parallel()

	collector := New()
	ctx := WithPhase(WithCollector(context.Background(), collector), "phase")
	ObserveLatency(ctx, "noteplan.writeback.meta.submit_wait", 3*time.Millisecond)
	AddCount(ctx, "noteplan.writeback.meta.flush.count", 2)
	ObserveLatency(ctx, "noteplan.writeback.meta.flush", 4*time.Millisecond)
	ObserveSample(ctx, "noteplan.writeback.meta.batch_rows", 5)
	ObserveSample(ctx, "noteplan.writeback.meta.batch_bytes", 2048)

	var window strings.Builder
	collector.appendWindowWritebackMetrics(&window, "phase")
	require.Equal(t, " writeback_meta_submit_wait_cum_ms=3 writeback_meta_submit_wait_p50_ms=3 writeback_meta_submit_wait_p95_ms=3 writeback_meta_submit_wait_max_ms=3 writeback_meta_flushes=2 writeback_meta_flush_cum_ms=4 writeback_meta_batch_rows_p50=5 writeback_meta_batch_rows_p95=5 writeback_meta_batch_rows_max=5 writeback_meta_batch_bytes_p50=2.0KiB writeback_meta_batch_bytes_p95=2.0KiB writeback_meta_batch_bytes_max=2.0KiB", window.String())
	require.True(t, collector.windowWritebackHasActivity("phase"))

	require.Equal(t, []string{
		"writeback_meta_submit_wait_cum=3ms",
		"writeback_meta_submit_wait_p50=3ms",
		"writeback_meta_submit_wait_p95=3ms",
		"writeback_meta_submit_wait_max=3ms",
		"writeback_meta_flushes=2",
		"writeback_meta_flush_cum=4ms",
		"writeback_meta_batch_rows_p50=5",
		"writeback_meta_batch_rows_p95=5",
		"writeback_meta_batch_rows_max=5",
		"writeback_meta_batch_bytes_p50=2.0KiB",
		"writeback_meta_batch_bytes_p95=2.0KiB",
		"writeback_meta_batch_bytes_max=2.0KiB",
	}, collector.appendSummaryWritebackMetrics(nil, "phase"))
}

func TestWritebackMetricDescriptorsPreserveFamilyAndQueueOrder(t *testing.T) {
	t.Parallel()

	collector := New()
	ctx := WithPhase(WithCollector(context.Background(), collector), "phase")
	for i, prefix := range []string{
		"noteplan.writeback.meta", "validation.writeback.metadata", "noteembed.writeback.chunk",
		"noteembed.writeback.intel", "codeembed.writeback.item", "codeembed.writeback.chunk", "codeembed.writeback.intel",
	} {
		AddCount(ctx, prefix+".flush.count", int64(i+1))
		SetGauge(ctx, prefix+".queue_depth", int64(i+1))
	}

	var details strings.Builder
	collector.appendWindowWritebackMetrics(&details, "phase")
	require.Equal(t, " writeback_meta_flushes=1 writeback_validation_metadata_flushes=2 writeback_note_chunk_flushes=3 writeback_note_intel_flushes=4 writeback_item_flushes=5 writeback_chunk_flushes=6 writeback_intel_flushes=7", details.String())

	var queues strings.Builder
	collector.appendWindowWritebackQueueMetrics(&queues, "phase")
	require.Equal(t, " writeback_item_queue_depth_peak=5 writeback_item_queue_depth_avg=5.0 writeback_chunk_queue_depth_peak=6 writeback_chunk_queue_depth_avg=6.0 writeback_meta_queue_depth_peak=1 writeback_meta_queue_depth_avg=1.0 writeback_note_chunk_queue_depth_peak=3 writeback_note_chunk_queue_depth_avg=3.0 writeback_intel_queue_depth_peak=7 writeback_intel_queue_depth_avg=7.0 writeback_note_intel_queue_depth_peak=4 writeback_note_intel_queue_depth_avg=4.0 writeback_validation_metadata_queue_depth_peak=2 writeback_validation_metadata_queue_depth_avg=2.0", queues.String())
}

func TestWritebackMetricDescriptorsPreserveIntegratedTimingOutput(t *testing.T) {
	t.Parallel()

	collector := New()
	ctx := WithPhase(WithCollector(context.Background(), collector), "embed_code")
	AddCount(ctx, "fs.read", 1)
	for i, prefix := range []string{
		"noteplan.writeback.meta", "validation.writeback.metadata", "noteembed.writeback.chunk",
		"noteembed.writeback.intel", "codeembed.writeback.item", "codeembed.writeback.chunk", "codeembed.writeback.intel",
	} {
		AddCount(ctx, prefix+".flush.count", int64(i+1))
	}
	collector.RecordSpan("embed_code", 10*time.Millisecond, nil)

	require.Equal(t,
		"[index-perf] window=1s phase=embed_code files=1 files_s=1.0 writeback_meta_flushes=1 writeback_validation_metadata_flushes=2 writeback_note_chunk_flushes=3 writeback_note_intel_flushes=4 writeback_item_flushes=5 writeback_chunk_flushes=6 writeback_intel_flushes=7",
		collector.RenderWindow(time.Second),
	)
	require.Equal(t,
		"Index timings:\n- embed_code                      10ms   files=1 files/s=100.0 writeback_meta_flushes=1 writeback_validation_metadata_flushes=2 writeback_note_chunk_flushes=3 writeback_note_intel_flushes=4 writeback_item_flushes=5 writeback_chunk_flushes=6 writeback_intel_flushes=7\n\nCritical path walls:\n- embed_code phase_wall=10ms",
		collector.RenderSummary(),
	)
}
