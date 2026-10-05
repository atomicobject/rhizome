package indexingperf

import (
	"fmt"
	"strings"
)

// writebackMetricDescriptor binds an internal metric namespace to its stable
// diagnostic-output prefix. Ordering is part of the --timings contract.
type writebackMetricDescriptor struct {
	metricPrefix string
	outputPrefix string
}

var writebackMetricDescriptors = [...]writebackMetricDescriptor{
	{metricPrefix: "noteplan.writeback.meta", outputPrefix: "writeback_meta"},
	{metricPrefix: "validation.writeback.metadata", outputPrefix: "writeback_validation_metadata"},
	{metricPrefix: "noteembed.writeback.chunk", outputPrefix: "writeback_note_chunk"},
	{metricPrefix: "noteembed.writeback.intel", outputPrefix: "writeback_note_intel"},
	{metricPrefix: "codeembed.writeback.item", outputPrefix: "writeback_item"},
	{metricPrefix: "codeembed.writeback.chunk", outputPrefix: "writeback_chunk"},
	{metricPrefix: "codeembed.writeback.intel", outputPrefix: "writeback_intel"},
}

// Queue metrics historically use a different stable order than the detailed
// writeback metrics. Keep that presentation contract explicit here.
var writebackQueueMetricDescriptors = [...]writebackMetricDescriptor{
	writebackMetricDescriptors[4],
	writebackMetricDescriptors[5],
	writebackMetricDescriptors[0],
	writebackMetricDescriptors[2],
	writebackMetricDescriptors[6],
	writebackMetricDescriptors[3],
	writebackMetricDescriptors[1],
}

func (c *Collector) windowWritebackHasActivity(phase string) bool {
	for _, descriptor := range writebackMetricDescriptors {
		if c.windowLatencyDeltaPhase(phase, descriptor.metricPrefix+".submit_wait") > 0 ||
			c.windowCounterDeltaPhase(phase, descriptor.metricPrefix+".flush.count") > 0 {
			return true
		}
	}
	return false
}

func (c *Collector) appendWindowWritebackMetrics(b *strings.Builder, phase string) {
	for _, descriptor := range writebackMetricDescriptors {
		submit := c.windowLatencyDeltaPhase(phase, descriptor.metricPrefix+".submit_wait")
		submitP50, submitP95, submitMax := c.windowLatencyWindowPercentilesPhase(phase, descriptor.metricPrefix+".submit_wait")
		flushes := c.windowCounterDeltaPhase(phase, descriptor.metricPrefix+".flush.count")
		flush := c.windowLatencyDeltaPhase(phase, descriptor.metricPrefix+".flush")
		rowsP50, rowsP95, rowsMax := c.windowSamplePercentilesPhase(phase, descriptor.metricPrefix+".batch_rows")
		bytesP50, bytesP95, bytesMax := c.windowSamplePercentilesPhase(phase, descriptor.metricPrefix+".batch_bytes")

		if submit > 0 {
			fmt.Fprintf(b, " %s_submit_wait_cum_ms=%d", descriptor.outputPrefix, submit)
			if submitMax > 0 {
				fmt.Fprintf(b, " %s_submit_wait_p50_ms=%d", descriptor.outputPrefix, submitP50.Milliseconds())
				fmt.Fprintf(b, " %s_submit_wait_p95_ms=%d", descriptor.outputPrefix, submitP95.Milliseconds())
				fmt.Fprintf(b, " %s_submit_wait_max_ms=%d", descriptor.outputPrefix, submitMax.Milliseconds())
			}
		}
		if flushes > 0 {
			fmt.Fprintf(b, " %s_flushes=%d", descriptor.outputPrefix, flushes)
		}
		if flush > 0 {
			fmt.Fprintf(b, " %s_flush_cum_ms=%d", descriptor.outputPrefix, flush)
		}
		if rowsMax > 0 {
			fmt.Fprintf(b, " %s_batch_rows_p50=%d", descriptor.outputPrefix, rowsP50)
			fmt.Fprintf(b, " %s_batch_rows_p95=%d", descriptor.outputPrefix, rowsP95)
			fmt.Fprintf(b, " %s_batch_rows_max=%d", descriptor.outputPrefix, rowsMax)
		}
		if bytesMax > 0 {
			fmt.Fprintf(b, " %s_batch_bytes_p50=%s", descriptor.outputPrefix, humanBytes(bytesP50))
			fmt.Fprintf(b, " %s_batch_bytes_p95=%s", descriptor.outputPrefix, humanBytes(bytesP95))
			fmt.Fprintf(b, " %s_batch_bytes_max=%s", descriptor.outputPrefix, humanBytes(bytesMax))
		}
	}
}

func (c *Collector) appendSummaryWritebackMetrics(parts []string, phase string) []string {
	for _, descriptor := range writebackMetricDescriptors {
		submit := c.phaseLatencyTotal(phase, descriptor.metricPrefix+".submit_wait")
		submitP50, submitP95, submitMax := c.phaseLatencyPercentiles(phase, descriptor.metricPrefix+".submit_wait")
		flushes := c.phaseCounterTotal(phase, descriptor.metricPrefix+".flush.count")
		flush := c.phaseLatencyTotal(phase, descriptor.metricPrefix+".flush")
		rowsP50, rowsP95, rowsMax := c.phaseSamplePercentiles(phase, descriptor.metricPrefix+".batch_rows")
		bytesP50, bytesP95, bytesMax := c.phaseSamplePercentiles(phase, descriptor.metricPrefix+".batch_bytes")

		if submit > 0 {
			parts = append(parts, fmt.Sprintf("%s_submit_wait_cum=%s", descriptor.outputPrefix, shortDur(submit)))
			if submitMax > 0 {
				parts = append(parts,
					fmt.Sprintf("%s_submit_wait_p50=%s", descriptor.outputPrefix, shortDur(submitP50)),
					fmt.Sprintf("%s_submit_wait_p95=%s", descriptor.outputPrefix, shortDur(submitP95)),
					fmt.Sprintf("%s_submit_wait_max=%s", descriptor.outputPrefix, shortDur(submitMax)),
				)
			}
		}
		if flushes > 0 {
			parts = append(parts, fmt.Sprintf("%s_flushes=%d", descriptor.outputPrefix, flushes))
		}
		if flush > 0 {
			parts = append(parts, fmt.Sprintf("%s_flush_cum=%s", descriptor.outputPrefix, shortDur(flush)))
		}
		if rowsMax > 0 {
			parts = append(parts,
				fmt.Sprintf("%s_batch_rows_p50=%d", descriptor.outputPrefix, rowsP50),
				fmt.Sprintf("%s_batch_rows_p95=%d", descriptor.outputPrefix, rowsP95),
				fmt.Sprintf("%s_batch_rows_max=%d", descriptor.outputPrefix, rowsMax),
			)
		}
		if bytesMax > 0 {
			parts = append(parts,
				fmt.Sprintf("%s_batch_bytes_p50=%s", descriptor.outputPrefix, humanBytes(bytesP50)),
				fmt.Sprintf("%s_batch_bytes_p95=%s", descriptor.outputPrefix, humanBytes(bytesP95)),
				fmt.Sprintf("%s_batch_bytes_max=%s", descriptor.outputPrefix, humanBytes(bytesMax)),
			)
		}
	}
	return parts
}

func (c *Collector) appendWindowWritebackQueueMetrics(b *strings.Builder, phase string) {
	for _, descriptor := range writebackQueueMetricDescriptors {
		if peak := c.windowGaugePeakPhase(phase, descriptor.metricPrefix+".queue_depth"); peak > 0 {
			fmt.Fprintf(b, " %s_queue_depth_peak=%d", descriptor.outputPrefix, peak)
		}
		if average := c.windowGaugeAveragePhase(phase, descriptor.metricPrefix+".queue_depth"); average > 0 {
			fmt.Fprintf(b, " %s_queue_depth_avg=%.1f", descriptor.outputPrefix, average)
		}
	}
}

func (c *Collector) appendSummaryWritebackQueueMetrics(parts []string, phase string) []string {
	for _, descriptor := range writebackQueueMetricDescriptors {
		if peak := c.phaseGaugePeak(phase, descriptor.metricPrefix+".queue_depth"); peak > 0 {
			parts = append(parts, fmt.Sprintf("%s_queue_depth_peak=%d", descriptor.outputPrefix, peak))
		}
		if average := c.phaseGaugeAverage(phase, descriptor.metricPrefix+".queue_depth"); average > 0 {
			parts = append(parts, fmt.Sprintf("%s_queue_depth_avg=%.1f", descriptor.outputPrefix, average))
		}
	}
	return parts
}
