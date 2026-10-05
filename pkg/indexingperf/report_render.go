package indexingperf

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type counterReportMetric struct {
	name  string
	label string
}

type latencyReportMetric struct {
	name        string
	label       string
	percentiles bool
}

func (c *Collector) windowCountersHaveActivity(phase string, metrics []counterReportMetric) bool {
	for _, metric := range metrics {
		if c.windowCounterDeltaPhase(phase, metric.name) != 0 {
			return true
		}
	}
	return false
}

func (c *Collector) appendWindowCounterMetrics(b *strings.Builder, phase string, metrics []counterReportMetric) {
	for _, metric := range metrics {
		if count := c.windowCounterDeltaPhase(phase, metric.name); count > 0 {
			fmt.Fprintf(b, " %s=%d", metric.label, count)
		}
	}
}

func (c *Collector) appendSummaryCounterMetrics(parts []string, phase string, metrics []counterReportMetric) []string {
	for _, metric := range metrics {
		if count := c.phaseCounterTotal(phase, metric.name); count > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", metric.label, count))
		}
	}
	return parts
}

func (c *Collector) appendWindowLatencyMetrics(b *strings.Builder, phase string, metrics []latencyReportMetric) {
	for _, metric := range metrics {
		total := c.windowLatencyDeltaPhase(phase, metric.name)
		if total <= 0 {
			continue
		}
		fmt.Fprintf(b, " %s_cum_ms=%d", metric.label, total)
		if metric.percentiles {
			p50, p95, max := c.windowLatencyWindowPercentilesPhase(phase, metric.name)
			if max > 0 {
				fmt.Fprintf(b, " %s_p50_ms=%d %s_p95_ms=%d %s_max_ms=%d", metric.label, p50.Milliseconds(), metric.label, p95.Milliseconds(), metric.label, max.Milliseconds())
			}
		}
	}
}

func (c *Collector) appendSummaryLatencyMetrics(parts []string, phase string, metrics []latencyReportMetric) []string {
	for _, metric := range metrics {
		total := c.phaseLatencyTotal(phase, metric.name)
		if total <= 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s_cum=%s", metric.label, shortDur(total)))
		if metric.percentiles {
			p50, p95, max := c.phaseLatencyPercentiles(phase, metric.name)
			if max > 0 {
				parts = append(parts,
					fmt.Sprintf("%s_p50=%s", metric.label, shortDur(p50)),
					fmt.Sprintf("%s_p95=%s", metric.label, shortDur(p95)),
					fmt.Sprintf("%s_max=%s", metric.label, shortDur(max)),
				)
			}
		}
	}
	return parts
}

func shortDur(d time.Duration) string {
	if d >= time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}

func ceilDivInt64(n, d int64) int64 {
	if n <= 0 || d <= 0 {
		return 0
	}
	return (n + d - 1) / d
}

func formatSuffixCounts(label string, counts map[string]int64) string {
	if len(counts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(counts))
	for key, value := range counts {
		if value <= 0 {
			continue
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", key, counts[key]))
	}
	return fmt.Sprintf("%s=%s", label, strings.Join(parts, ","))
}

func humanBytes(v int64) string {
	const unit = 1024
	if v < unit {
		return fmt.Sprintf("%dB", v)
	}
	div, exp := int64(unit), 0
	for n := v / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(v)/float64(div), "KMGTPE"[exp])
}
