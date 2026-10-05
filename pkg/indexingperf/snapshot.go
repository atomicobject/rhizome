package indexingperf

import (
	"sort"
	"time"
)

// Snapshot is a non-consuming, immutable view of recorded measurements. Duration
// fields use nanoseconds; retained-prefix quantiles describe all observations when RetainedSamples
// equals Count. Interval unions cover retained intervals only. Raw samples and
// interval timestamps stay inside the collector so exports remain compact.
type Snapshot struct {
	StartedAt            time.Time              `json:"startedAt"`
	Limits               Limits                 `json:"limits"`
	Coverage             Coverage               `json:"coverage"`
	Counters             []CounterSnapshot      `json:"counters"`
	Gauges               []GaugeSnapshot        `json:"gauges"`
	Latencies            []DistributionSnapshot `json:"latencies"`
	Samples              []DistributionSnapshot `json:"samples"`
	Spans                []SpanSnapshot         `json:"spans"`
	Intervals            []IntervalSnapshot     `json:"intervals"`
	DBWrites             []DBWriteSnapshot      `json:"dbWrites"`
	ProviderFingerprints []string               `json:"providerFingerprints,omitempty"`
}

type MetricIdentity struct {
	Phase string `json:"phase,omitempty"`
	Name  string `json:"name"`
}

type CounterSnapshot struct {
	MetricIdentity
	Count int64 `json:"count"`
	Total int64 `json:"total"`
	Max   int64 `json:"max"`
}

type GaugeSnapshot struct {
	MetricIdentity
	Current int64 `json:"current"`
	Peak    int64 `json:"peak"`
	Count   int64 `json:"count"`
	Total   int64 `json:"total"`
}

type DistributionSnapshot struct {
	MetricIdentity
	Count            int64  `json:"count"`
	Total            int64  `json:"total"`
	Max              int64  `json:"max"`
	P50              int64  `json:"p50"`
	P95              int64  `json:"p95"`
	RetainedSamples  int    `json:"retainedSamples"`
	QuantileCoverage string `json:"quantileCoverage"` // all or retained_prefix
}

type SpanSnapshot struct {
	Name              string    `json:"name"`
	Count             int64     `json:"count"`
	DurationNs        int64     `json:"durationNs"`
	Status            string    `json:"status"`
	StartedAt         time.Time `json:"startedAt"`
	EndedAt           time.Time `json:"endedAt"`
	RetainedIntervals int       `json:"retainedIntervals"`
	RetainedUnionNs   int64     `json:"retainedUnionNs"`
	IntervalCoverage  string    `json:"intervalCoverage"`
}

type IntervalSnapshot struct {
	MetricIdentity
	Count             int64  `json:"count"`
	IntervalCoverage  string `json:"intervalCoverage"`
	RetainedUnionNs   int64  `json:"retainedUnionNs"`
	RetainedIntervals int    `json:"retainedIntervals"`
}

type DBWriteSnapshot struct {
	Store  string `json:"store"`
	Op     string `json:"op"`
	Count  int64  `json:"count"`
	WaitNs int64  `json:"waitNs"`
	HoldNs int64  `json:"holdNs"`
}

func (c *Collector) Snapshot() Snapshot {
	out := Snapshot{
		Counters: []CounterSnapshot{}, Gauges: []GaugeSnapshot{}, Latencies: []DistributionSnapshot{},
		Samples: []DistributionSnapshot{}, Spans: []SpanSnapshot{}, Intervals: []IntervalSnapshot{}, DBWrites: []DBWriteSnapshot{},
	}
	if c == nil {
		return out
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out.StartedAt, out.Limits, out.Coverage = c.startedAt.UTC(), c.limits, c.coverage
	for m, v := range c.counters {
		out.Counters = append(out.Counters, CounterSnapshot{identity(m), v.Count, v.Total, v.Max})
	}
	for m, v := range c.gauges {
		stats := c.gaugeStats[m]
		out.Gauges = append(out.Gauges, GaugeSnapshot{identity(m), v, c.gaugePeaks[m], stats.Count, stats.Total})
	}
	for m, v := range c.latencies {
		p50, p95, _ := percentiles(v.Samples)
		out.Latencies = append(out.Latencies, distribution(m, v.Count, int64(v.Total), int64(v.Max), int64(p50), int64(p95), len(v.Samples)))
	}
	for m, v := range c.samples {
		p50, p95, _ := percentiles(v.Samples)
		out.Samples = append(out.Samples, distribution(m, v.Count, v.Total, v.Max, p50, p95, len(v.Samples)))
	}
	for _, v := range c.spans {
		out.Spans = append(out.Spans, SpanSnapshot{v.Name, v.Count, int64(v.Duration), v.Status, v.StartedAt.UTC(), v.EndedAt.UTC(), len(v.Windows), int64(unionDuration(append([]timeWindow(nil), v.Windows...))), detailCoverage(v.Count, len(v.Windows))})
	}
	for m, v := range c.intervals {
		out.Intervals = append(out.Intervals, IntervalSnapshot{identity(m), c.intervalCounts[m], detailCoverage(c.intervalCounts[m], len(v)), int64(unionDuration(append([]timeWindow(nil), v...))), len(v)})
	}
	for _, v := range c.dbWrites {
		out.DBWrites = append(out.DBWrites, DBWriteSnapshot{v.Store, v.Op, v.Count, int64(v.Wait), int64(v.Hold)})
	}
	for fingerprint := range c.providerFingerprints {
		out.ProviderFingerprints = append(out.ProviderFingerprints, fingerprint)
	}
	sort.Slice(out.Counters, func(i, j int) bool {
		return identityLess(out.Counters[i].MetricIdentity, out.Counters[j].MetricIdentity)
	})
	sort.Slice(out.Gauges, func(i, j int) bool { return identityLess(out.Gauges[i].MetricIdentity, out.Gauges[j].MetricIdentity) })
	sort.Slice(out.Latencies, func(i, j int) bool {
		return identityLess(out.Latencies[i].MetricIdentity, out.Latencies[j].MetricIdentity)
	})
	sort.Slice(out.Samples, func(i, j int) bool { return identityLess(out.Samples[i].MetricIdentity, out.Samples[j].MetricIdentity) })
	sort.Slice(out.Spans, func(i, j int) bool { return out.Spans[i].Name < out.Spans[j].Name })
	sort.Slice(out.Intervals, func(i, j int) bool {
		return identityLess(out.Intervals[i].MetricIdentity, out.Intervals[j].MetricIdentity)
	})
	sort.Slice(out.DBWrites, func(i, j int) bool {
		a, b := out.DBWrites[i], out.DBWrites[j]
		if a.Store == b.Store {
			return a.Op < b.Op
		}
		return a.Store < b.Store
	})
	sort.Strings(out.ProviderFingerprints)
	return out
}

func identity(m Metric) MetricIdentity { return MetricIdentity{m.Phase, m.Name} }
func identityLess(a, b MetricIdentity) bool {
	if a.Phase == b.Phase {
		return a.Name < b.Name
	}
	return a.Phase < b.Phase
}
func distribution(m Metric, count, total, max, p50, p95 int64, retained int) DistributionSnapshot {
	coverage := detailCoverage(count, retained)
	return DistributionSnapshot{identity(m), count, total, max, p50, p95, retained, coverage}
}
func detailCoverage(count int64, retained int) string {
	if int64(retained) < count {
		return "retained_prefix"
	}
	return "all"
}
