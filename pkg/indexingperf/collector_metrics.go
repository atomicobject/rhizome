package indexingperf

import (
	"math"
	"slices"
	"strings"
	"time"
)

func (c *Collector) windowCounterDeltaPhase(phase, name string) int64 {
	metric := Metric{Phase: phase, Name: name, Kind: kindCounter}
	return c.counters[metric].Total - c.lastCounters[metric]
}

func (c *Collector) windowCounterDeltaPrefixPhase(phase, prefix string) map[string]int64 {
	out := make(map[string]int64)
	for m, stat := range c.counters {
		if m.Phase != phase || !strings.HasPrefix(m.Name, prefix) {
			continue
		}
		prev := c.lastCounters[m]
		delta := stat.Total - prev
		if delta <= 0 {
			continue
		}
		out[strings.TrimPrefix(m.Name, prefix)] += delta
	}
	return out
}

func (c *Collector) windowLatencyDeltaPhase(phase, name string) int64 {
	metric := Metric{Phase: phase, Name: name, Kind: kindLatency}
	return (c.latencies[metric].Total - c.lastLatencies[metric]).Milliseconds()
}

func (c *Collector) windowLatencyWindowPercentilesPhase(phase, name string) (p50, p95, peak time.Duration) {
	metric := Metric{Phase: phase, Name: name, Kind: kindLatency}
	if c.windowLatencies != nil {
		stat := c.windowLatencies[metric]
		p50, p95, _ = percentiles(stat.Samples)
		return p50, p95, stat.Max
	}
	samples := c.latencies[metric].Samples
	start := min(max(c.lastLatencyCounts[metric], 0), int64(len(samples)))
	return percentiles(samples[start:])
}

func (c *Collector) windowGaugeAveragePhase(phase, name string) float64 {
	metric := Metric{Phase: phase, Name: name, Kind: kindGauge}
	stat, previous := c.gaugeStats[metric], c.lastGaugeStats[metric]
	count := stat.Count - previous.Count
	if count == 0 {
		return 0
	}
	return float64(stat.Total-previous.Total) / float64(count)
}

func (c *Collector) windowGaugePeakPhase(phase, name string) int64 {
	return c.windowGaugePeaks[Metric{Phase: phase, Name: name, Kind: kindGauge}]
}

func (c *Collector) windowLatencyAvgPhase(phase, name string) time.Duration {
	metric := Metric{Phase: phase, Name: name, Kind: kindLatency}
	stat := c.latencies[metric]
	count := stat.Count - c.lastLatencyCounts[metric]
	if count == 0 {
		return 0
	}
	return (stat.Total - c.lastLatencies[metric]) / time.Duration(count)
}

func (c *Collector) phaseCounterTotal(phase, name string) int64 {
	return c.counters[Metric{Phase: phase, Name: name, Kind: kindCounter}].Total
}

func (c *Collector) phaseCounterTotalPrefix(phase, prefix string) map[string]int64 {
	out := make(map[string]int64)
	for m, stat := range c.counters {
		if m.Phase != phase || !strings.HasPrefix(m.Name, prefix) {
			continue
		}
		if stat.Total <= 0 {
			continue
		}
		out[strings.TrimPrefix(m.Name, prefix)] += stat.Total
	}
	return out
}

func (c *Collector) phaseDuration(phase string) time.Duration {
	if span, ok := c.spans[phase]; ok {
		return span.Duration
	}
	return 0
}

func (c *Collector) phaseLatencyTotal(phase, name string) time.Duration {
	return c.latencies[Metric{Phase: phase, Name: name, Kind: kindLatency}].Total
}

func (c *Collector) phaseGaugePeak(phase, name string) int64 {
	return c.gaugePeaks[Metric{Phase: phase, Name: name, Kind: kindGauge}]
}

func (c *Collector) phaseGaugeAverage(phase, name string) float64 {
	stat := c.gaugeStats[Metric{Phase: phase, Name: name, Kind: kindGauge}]
	if stat.Count == 0 {
		return 0
	}
	return float64(stat.Total) / float64(stat.Count)
}

func (c *Collector) phaseLatencyAvg(phase, name string) time.Duration {
	stat := c.latencies[Metric{Phase: phase, Name: name, Kind: kindLatency}]
	if stat.Count == 0 {
		return 0
	}
	return stat.Total / time.Duration(stat.Count)
}

func (c *Collector) phaseLatencyMax(phase, name string) time.Duration {
	return c.latencies[Metric{Phase: phase, Name: name, Kind: kindLatency}].Max
}

func (c *Collector) phaseLatencyPercentiles(phase, name string) (p50, p95, max time.Duration) {
	stat := c.latencies[Metric{Phase: phase, Name: name, Kind: kindLatency}]
	p50, p95, _ = percentiles(stat.Samples)
	return p50, p95, stat.Max
}

func (c *Collector) phaseSamplePercentiles(phase, name string) (p50, p95, max int64) {
	stat := c.samples[Metric{Phase: phase, Name: name, Kind: kindSample}]
	p50, p95, _ = percentiles(stat.Samples)
	return p50, p95, stat.Max
}

func (c *Collector) currentGaugePhase(phase, name string) int64 {
	return c.gauges[Metric{Phase: phase, Name: name, Kind: kindGauge}]
}

func (c *Collector) windowSampleCountPhase(phase, name string) int64 {
	metric := Metric{Phase: phase, Name: name, Kind: kindSample}
	count := c.samples[metric].Count
	return count - min(max(c.lastSampleCounts[metric], 0), count)
}

func (c *Collector) phaseSampleCount(phase, name string) int64 {
	return c.samples[Metric{Phase: phase, Name: name, Kind: kindSample}].Count
}

func (c *Collector) windowSamplePercentilesPhase(phase, name string) (p50, p95, peak int64) {
	metric := Metric{Phase: phase, Name: name, Kind: kindSample}
	if c.windowSamples != nil {
		stat := c.windowSamples[metric]
		p50, p95, _ = percentiles(stat.Samples)
		return p50, p95, stat.Max
	}
	samples := c.samples[metric].Samples
	start := min(max(c.lastSampleCounts[metric], 0), int64(len(samples)))
	return percentiles(samples[start:])
}

// percentiles sorts a copy so rendering never reorders the recorded samples that
// consecutive windows address by their insertion count.
func percentiles[T ~int64](values []T) (p50, p95, max T) {
	if len(values) == 0 {
		return 0, 0, 0
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return percentile(sorted, 0.50), percentile(sorted, 0.95), sorted[len(sorted)-1]
}

func percentile[T ~int64](sorted []T, pct float64) T {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(pct*float64(len(sorted)))) - 1
	return sorted[min(max(idx, 0), len(sorted)-1)]
}
