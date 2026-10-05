package indexingperf

import (
	"context"
	"strings"
	"sync"
	"time"
)

type collectorKey struct{}
type phaseKey struct{}
type opKey struct{}

type metricKind string

const (
	kindCounter metricKind = "counter"
	kindGauge   metricKind = "gauge"
	kindLatency metricKind = "latency"
	kindSample  metricKind = "sample"
)

type Metric struct {
	Phase string
	Name  string
	Kind  metricKind
}

type statCounter struct {
	Count int64
	Total int64
	Max   int64
}

type statLatency struct {
	Count   int64
	Total   time.Duration
	Max     time.Duration
	Samples []time.Duration
}

type statSample struct {
	Count   int64
	Total   int64
	Max     int64
	Samples []int64
}

type DBWriteMetric struct {
	Store string
	Op    string
	Count int64
	Wait  time.Duration
	Hold  time.Duration
}

type spanMetric struct {
	Name      string
	Count     int64
	Duration  time.Duration
	Status    string
	StartedAt time.Time
	EndedAt   time.Time
	Windows   []timeWindow
}

type timeWindow struct {
	StartedAt time.Time
	EndedAt   time.Time
}

type Collector struct {
	mu                   sync.Mutex
	startedAt            time.Time
	limits               Limits
	series               map[string]struct{}
	coverage             Coverage
	providerFingerprints map[string]struct{}
	counters             map[Metric]statCounter
	gauges               map[Metric]int64
	gaugeStats           map[Metric]statCounter
	gaugePeaks           map[Metric]int64
	windowGaugePeaks     map[Metric]int64
	windowLatencies      map[Metric]statLatency
	windowSamples        map[Metric]statSample
	windowDroppedSamples int64
	latencies            map[Metric]statLatency
	samples              map[Metric]statSample
	intervals            map[Metric][]timeWindow
	intervalCounts       map[Metric]int64
	dbWrites             map[string]DBWriteMetric
	spans                map[string]spanMetric
	metricAllowed        func(Metric) bool
	spanAllowed          func(string) bool
	collectDBWrites      bool

	lastCounters      map[Metric]int64
	lastLatencyCounts map[Metric]int64
	lastGaugeStats    map[Metric]statCounter
	lastLatencies     map[Metric]time.Duration
	lastSampleCounts  map[Metric]int64
	lastDBWrites      map[string]DBWriteMetric
}

func New() *Collector {
	return &Collector{
		startedAt:         time.Now(),
		counters:          make(map[Metric]statCounter),
		gauges:            make(map[Metric]int64),
		gaugeStats:        make(map[Metric]statCounter),
		gaugePeaks:        make(map[Metric]int64),
		windowGaugePeaks:  make(map[Metric]int64),
		latencies:         make(map[Metric]statLatency),
		samples:           make(map[Metric]statSample),
		intervals:         make(map[Metric][]timeWindow),
		intervalCounts:    make(map[Metric]int64),
		dbWrites:          make(map[string]DBWriteMetric),
		spans:             make(map[string]spanMetric),
		collectDBWrites:   true,
		lastCounters:      make(map[Metric]int64),
		lastLatencyCounts: make(map[Metric]int64),
		lastGaugeStats:    make(map[Metric]statCounter),
		lastLatencies:     make(map[Metric]time.Duration),
		lastSampleCounts:  make(map[Metric]int64),
		lastDBWrites:      make(map[string]DBWriteMetric),
	}
}

func WithCollector(ctx context.Context, c *Collector) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if c == nil {
		return ctx
	}
	return context.WithValue(ctx, collectorKey{}, c)
}

func FromContext(ctx context.Context) *Collector {
	if ctx == nil {
		return nil
	}
	if v := ctx.Value(collectorKey{}); v != nil {
		if c, ok := v.(*Collector); ok {
			return c
		}
	}
	return nil
}

func WithPhase(ctx context.Context, phase string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(phase) == "" {
		return ctx
	}
	return context.WithValue(ctx, phaseKey{}, phase)
}

func PhaseFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v := ctx.Value(phaseKey{}); v != nil {
		if phase, ok := v.(string); ok {
			return phase
		}
	}
	return ""
}

func WithOp(ctx context.Context, op string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(op) == "" {
		return ctx
	}
	return context.WithValue(ctx, opKey{}, op)
}

func OpFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v := ctx.Value(opKey{}); v != nil {
		if op, ok := v.(string); ok {
			return op
		}
	}
	return ""
}

func StartSpan(ctx context.Context, name string) func(error) {
	if FromContext(ctx) == nil {
		return func(error) {}
	}
	started := time.Now()
	return func(err error) {
		if c := FromContext(ctx); c != nil {
			c.RecordSpanWindow(name, started, time.Now(), err)
		}
	}
}

func (c *Collector) RecordSpan(name string, d time.Duration, err error) {
	ended := time.Now()
	started := ended.Add(-d)
	c.RecordSpanWindow(name, started, ended, err)
}

func (c *Collector) RecordSpanWindow(name string, started, ended time.Time, err error) {
	if c == nil || strings.TrimSpace(name) == "" {
		return
	}
	if c.spanAllowed != nil && !c.spanAllowed(name) {
		return
	}
	status := "ok"
	if err != nil {
		status = "error"
	}
	if ended.Before(started) {
		ended = started
	}
	c.mu.Lock()
	if !c.admitLocked("span", name) {
		c.mu.Unlock()
		return
	}
	cur := c.spans[name]
	cur.Name = name
	cur.Count++
	cur.Duration += ended.Sub(started)
	if cur.StartedAt.IsZero() || started.Before(cur.StartedAt) {
		cur.StartedAt = started
	}
	if ended.After(cur.EndedAt) {
		cur.EndedAt = ended
	}
	if cur.Status == "" || status == "error" {
		cur.Status = status
	}
	if c.keepInterval(len(cur.Windows)) {
		cur.Windows = append(cur.Windows, timeWindow{StartedAt: started, EndedAt: ended})
	}
	c.spans[name] = cur
	c.mu.Unlock()
}

func AddCount(ctx context.Context, name string, delta int64) {
	if c := FromContext(ctx); c != nil {
		metric := metricFor(ctx, name, kindCounter)
		if c.metricAllowed == nil || c.metricAllowed(metric) {
			c.addCounter(metric, delta)
		}
	}
}

// MarkCountAvailable records that a counter-bearing operation was observed
// even when its correct count is zero.
func MarkCountAvailable(ctx context.Context, name string) {
	if c := FromContext(ctx); c != nil {
		metric := metricFor(ctx, name, kindCounter)
		if c.metricAllowed == nil || c.metricAllowed(metric) {
			c.mu.Lock()
			if c.admitMetric(metric) {
				if _, ok := c.counters[metric]; !ok {
					c.counters[metric] = statCounter{}
				}
			}
			c.mu.Unlock()
		}
	}
}

func AddBytes(ctx context.Context, name string, delta int64) {
	AddCount(ctx, name+"_bytes", delta)
}

func SetGauge(ctx context.Context, name string, value int64) {
	if c := FromContext(ctx); c != nil {
		metric := metricFor(ctx, name, kindGauge)
		if c.metricAllowed == nil || c.metricAllowed(metric) {
			c.setGauge(metric, value)
		}
	}
}

func ObserveLatency(ctx context.Context, name string, d time.Duration) {
	if c := FromContext(ctx); c != nil {
		metric := metricFor(ctx, name, kindLatency)
		if c.metricAllowed == nil || c.metricAllowed(metric) {
			c.addLatency(metric, d)
		}
	}
}

func ObserveSample(ctx context.Context, name string, value int64) {
	if c := FromContext(ctx); c != nil {
		metric := metricFor(ctx, name, kindSample)
		if c.metricAllowed == nil || c.metricAllowed(metric) {
			c.addSample(metric, value)
		}
	}
}

func ObserveInterval(ctx context.Context, name string, started time.Time, d time.Duration) {
	if c := FromContext(ctx); c != nil {
		metric := metricFor(ctx, name, kindLatency)
		if c.metricAllowed == nil || c.metricAllowed(metric) {
			c.addInterval(metric, started, d)
		}
	}
}

// ObserveDBWrite records write-lock wait/hold time for the indexing timing report.
//
// Docs:
// - [[indexing-observability-maintenance-policy#^spec-0047-us2-ac4]]
func ObserveDBWrite(ctx context.Context, store string, wait, hold time.Duration) {
	c := FromContext(ctx)
	if c == nil {
		return
	}
	if !c.collectDBWrites {
		return
	}
	op := OpFromContext(ctx)
	if strings.TrimSpace(op) == "" {
		// Keep timings actionable even when a caller forgets WithOp(...). Auto
		// labels are noisier than explicit names, but they are much better than a
		// collapsing “unknown” bucket that hides new write hotspots.
		op = inferDBWriteOp()
	}
	c.mu.Lock()
	key := store + "|" + op
	if !c.admitLocked("db", store, op) {
		c.mu.Unlock()
		return
	}
	cur := c.dbWrites[key]
	cur.Store = store
	cur.Op = op
	cur.Count++
	cur.Wait += wait
	cur.Hold += hold
	c.dbWrites[key] = cur
	c.mu.Unlock()
	ObserveLatency(ctx, "db.wait", wait)
	ObserveLatency(ctx, "db.hold", hold)
}

func (c *Collector) addCounter(m Metric, delta int64) {
	if delta == 0 {
		return
	}
	c.mu.Lock()
	if !c.admitMetric(m) {
		c.mu.Unlock()
		return
	}
	cur := c.counters[m]
	cur.Count++
	cur.Total += delta
	if delta > cur.Max {
		cur.Max = delta
	}
	c.counters[m] = cur
	c.mu.Unlock()
}

func (c *Collector) setGauge(m Metric, value int64) {
	c.mu.Lock()
	if !c.admitMetric(m) {
		c.mu.Unlock()
		return
	}
	c.gauges[m] = value
	stats := c.gaugeStats[m]
	stats.Count++
	stats.Total += value
	if value > stats.Max {
		stats.Max = value
	}
	c.gaugeStats[m] = stats
	if value > c.gaugePeaks[m] {
		c.gaugePeaks[m] = value
	}
	if value > c.windowGaugePeaks[m] {
		c.windowGaugePeaks[m] = value
	}
	c.mu.Unlock()
}

func (c *Collector) addLatency(m Metric, d time.Duration) {
	if d < 0 {
		d = 0
	}
	c.mu.Lock()
	if !c.admitMetric(m) {
		c.mu.Unlock()
		return
	}
	cur := c.latencies[m]
	cur.Count++
	cur.Total += d
	if d > cur.Max {
		cur.Max = d
	}
	if c.keepSample(len(cur.Samples)) {
		cur.Samples = append(cur.Samples, d)
	}
	c.latencies[m] = cur
	c.recordWindowLatencyLocked(m, d)
	c.mu.Unlock()
}

func (c *Collector) addInterval(m Metric, started time.Time, d time.Duration) {
	if started.IsZero() {
		return
	}
	if d < 0 {
		d = 0
	}
	ended := started.Add(d)
	c.mu.Lock()
	if !c.admitMetric(m) {
		c.mu.Unlock()
		return
	}
	c.intervalCounts[m]++
	if c.keepInterval(len(c.intervals[m])) {
		c.intervals[m] = append(c.intervals[m], timeWindow{StartedAt: started, EndedAt: ended})
	}
	c.mu.Unlock()
}

func (c *Collector) addSample(m Metric, value int64) {
	if value < 0 {
		value = 0
	}
	c.mu.Lock()
	if !c.admitMetric(m) {
		c.mu.Unlock()
		return
	}
	cur := c.samples[m]
	cur.Count++
	cur.Total += value
	if value > cur.Max {
		cur.Max = value
	}
	if c.keepSample(len(cur.Samples)) {
		cur.Samples = append(cur.Samples, value)
	}
	c.samples[m] = cur
	c.recordWindowSampleLocked(m, value)
	c.mu.Unlock()
}

func metricFor(ctx context.Context, name string, kind metricKind) Metric {
	return Metric{
		Phase: PhaseFromContext(ctx),
		Name:  name,
		Kind:  kind,
	}
}
