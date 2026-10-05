package indexingperf

import (
	"fmt"
	"strings"
	"time"
)

// Window detail has its own budget so a full cumulative prefix cannot suppress
// new observations. Callers hold mu; rendering clears only this window state.
func (c *Collector) recordWindowLatencyLocked(metric Metric, value time.Duration) {
	if c.windowLatencies == nil {
		return
	}
	stat := c.windowLatencies[metric]
	stat.Count++
	if value > stat.Max {
		stat.Max = value
	}
	if len(stat.Samples) < c.limits.SamplesPerMetric {
		stat.Samples = append(stat.Samples, value)
	} else {
		c.windowDroppedSamples++
	}
	c.windowLatencies[metric] = stat
}

func (c *Collector) recordWindowSampleLocked(metric Metric, value int64) {
	if c.windowSamples == nil {
		return
	}
	stat := c.windowSamples[metric]
	stat.Count++
	if value > stat.Max {
		stat.Max = value
	}
	if len(stat.Samples) < c.limits.SamplesPerMetric {
		stat.Samples = append(stat.Samples, value)
	} else {
		c.windowDroppedSamples++
	}
	c.windowSamples[metric] = stat
}

func (c *Collector) renderWindowCoverageLocked() string {
	if c.windowDroppedSamples == 0 {
		return ""
	}
	return fmt.Sprintf("[index-perf] coverage: window_dropped_samples=%d; window quantiles use retained prefixes; window totals and maxima remain exact for admitted series", c.windowDroppedSamples)
}

func (c *Collector) renderCumulativeCoverageLocked() string {
	coverage := c.coverage
	if coverage == (Coverage{}) {
		return ""
	}
	parts := []string{fmt.Sprintf("Metrics coverage: dropped_samples=%d dropped_intervals=%d rejected_observations=%d", coverage.DroppedSamples, coverage.DroppedIntervals, coverage.RejectedObservations)}
	if coverage.DroppedSamples > 0 {
		parts = append(parts, "cumulative quantiles use retained prefixes")
	}
	if coverage.DroppedIntervals > 0 {
		parts = append(parts, "interval walls and overlaps cover retained intervals")
	}
	parts = append(parts, "totals and maxima remain exact for admitted series")
	return strings.Join(parts, "; ")
}

func (c *Collector) phaseCoveragePartsLocked(phase string) []string {
	if c.series == nil {
		return nil
	}
	var samplesPartial, intervalsPartial bool
	for metric, stat := range c.latencies {
		if metric.Phase == phase && stat.Count > int64(len(stat.Samples)) {
			samplesPartial = true
		}
	}
	for metric, stat := range c.samples {
		if metric.Phase == phase && stat.Count > int64(len(stat.Samples)) {
			samplesPartial = true
		}
	}
	for metric, count := range c.intervalCounts {
		if metric.Phase == phase && count > int64(len(c.intervals[metric])) {
			intervalsPartial = true
		}
	}
	if span := c.spans[phase]; span.Count > int64(len(span.Windows)) {
		intervalsPartial = true
	}
	var parts []string
	if samplesPartial {
		parts = append(parts, "quantile_coverage=retained_prefix")
	}
	if intervalsPartial {
		parts = append(parts, "interval_coverage=retained_prefix")
	}
	return parts
}
