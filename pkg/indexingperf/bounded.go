package indexingperf

import (
	"context"
	"strings"
)

// Limits bounds retained detail, not cumulative measurements for admitted series.
// SamplesPerMetric applies separately to cumulative and render-window prefixes.
// Nonpositive options use defaults. New remains unbounded for legacy text reports.
type Limits struct {
	Series             int `json:"series"`
	SamplesPerMetric   int `json:"samplesPerMetric"`
	IntervalsPerMetric int `json:"intervalsPerMetric"`
	LabelBytes         int `json:"labelBytes"`
}

// Coverage makes incomplete detail explicit. Samples retain the first N values;
// their quantiles describe that retained prefix, rather than the whole operation.
type Coverage struct {
	RejectedObservations int64 `json:"rejectedObservations"`
	DroppedSamples       int64 `json:"droppedSamples"`
	DroppedIntervals     int64 `json:"droppedIntervals"`
}

func NewBounded(options ...Limits) *Collector {
	limits := Limits{Series: 1024, SamplesPerMetric: 256, IntervalsPerMetric: 64, LabelBytes: 120}
	if len(options) > 0 {
		opt := options[0]
		if opt.Series > 0 {
			limits.Series = opt.Series
		}
		if opt.SamplesPerMetric > 0 {
			limits.SamplesPerMetric = opt.SamplesPerMetric
		}
		if opt.IntervalsPerMetric > 0 {
			limits.IntervalsPerMetric = opt.IntervalsPerMetric
		}
		if opt.LabelBytes > 0 {
			limits.LabelBytes = opt.LabelBytes
		}
	}
	c := New()
	c.limits, c.series = limits, make(map[string]struct{})
	c.windowLatencies = make(map[Metric]statLatency)
	c.windowSamples = make(map[Metric]statSample)
	return c
}

// admitLocked is called with mu held. Reject long labels instead of truncating them
// into collisions; metric labels must be curated and must never contain content.
func (c *Collector) admitLocked(kind string, labels ...string) bool {
	if c.series == nil {
		return true
	}
	for _, label := range labels {
		if len(label) > c.limits.LabelBytes {
			c.coverage.RejectedObservations++
			return false
		}
	}
	key := kind + "\x00" + strings.Join(labels, "\x00")
	if _, ok := c.series[key]; ok {
		return true
	}
	if len(c.series) >= c.limits.Series {
		c.coverage.RejectedObservations++
		return false
	}
	c.series[key] = struct{}{}
	return true
}

func (c *Collector) admitMetric(m Metric) bool { return c.admitLocked(string(m.Kind), m.Phase, m.Name) }

func (c *Collector) keepSample(count int) bool {
	if c.series == nil || count < c.limits.SamplesPerMetric {
		return true
	}
	c.coverage.DroppedSamples++
	return false
}

func (c *Collector) keepInterval(count int) bool {
	if c.series == nil || count < c.limits.IntervalsPerMetric {
		return true
	}
	c.coverage.DroppedIntervals++
	return false
}

// ObserveProviderFingerprint records an opaque provider-configuration digest.
// Callers must hash configuration and must not pass endpoints, models, or keys.
func ObserveProviderFingerprint(ctx context.Context, fingerprint string) {
	c := FromContext(ctx)
	if c == nil || len(fingerprint) != 64 {
		return
	}
	for _, ch := range fingerprint {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			return
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.admitLocked("provider", fingerprint) {
		return
	}
	if c.providerFingerprints == nil {
		c.providerFingerprints = make(map[string]struct{})
	}
	c.providerFingerprints[fingerprint] = struct{}{}
}

// DiagnosticFallbackPrefix identifies counters for an observed search fallback
// branch. They count the branch, not whether fallback recovered the request.
const DiagnosticFallbackPrefix = "search.fallback."
