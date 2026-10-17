package indexingperf

import (
	"sort"
	"strings"
	"time"
)

func (c *Collector) RenderWindow(window time.Duration) string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	defer c.advanceWindowSnapshots()

	phases := c.windowPhases(3)
	if len(phases) == 0 {
		return c.renderWindowCoverageLocked()
	}
	lines := make([]string, 0, len(phases))
	for _, phase := range phases {
		if line := c.renderWindowPhaseLine(window, phase); line != "" {
			lines = append(lines, line)
		}
	}
	if coverage := c.renderWindowCoverageLocked(); coverage != "" {
		lines = append(lines, coverage)
	}
	return strings.Join(lines, "\n")
}

type windowPhaseStats struct {
	active     bool
	provider   time.Duration
	prep       time.Duration
	queue      time.Duration
	db         time.Duration
	gate       time.Duration
	files      int64
	discovered int64
	completed  int64
	chunks     int64
	calls      int64
	texts      int64
}

func (c *Collector) windowPhase() string {
	phases := c.windowPhases(1)
	if len(phases) == 0 {
		return ""
	}
	return phases[0]
}

func (c *Collector) windowPhases(limit int) []string {
	phases := c.windowPhaseStats()
	names := make([]string, 0, len(phases))
	for phase := range phases {
		names = append(names, phase)
	}
	sort.Slice(names, func(i, j int) bool {
		if betterWindowPhase(phases[names[i]], phases[names[j]]) {
			return true
		}
		if betterWindowPhase(phases[names[j]], phases[names[i]]) {
			return false
		}
		return names[i] < names[j]
	})
	if limit > 0 && len(names) > limit {
		names = names[:limit]
	}
	return names
}

func (c *Collector) windowPhaseStats() map[string]windowPhaseStats {
	phases := make(map[string]windowPhaseStats)
	for m, stat := range c.latencies {
		prev := c.lastLatencies[m]
		delta := stat.Total - prev
		if delta <= 0 {
			continue
		}
		entry := phases[m.Phase]
		switch m.Name {
		case ProviderLatency:
			entry.active = true
			entry.provider += delta
		case ProviderRequestVoyageBackoff:
			// Backoff is active provider work, but it is not a completed provider
			// latency sample and must not inflate provider throughput timing.
			entry.active = true
		case CodeembedPrepare:
			entry.active = true
			entry.prep += delta
		case QueueWait:
			entry.active = true
			entry.queue += delta
		case DbWait, DbHold:
			entry.active = true
			entry.db += delta
		case EmbedGateWait:
			entry.active = true
			entry.gate += delta
		case FsWalk:
			entry.active = true
			entry.queue += delta
		default:
			entry.active = true
		}
		phases[m.Phase] = entry
	}
	for m, stat := range c.counters {
		prev := c.lastCounters[m]
		delta := stat.Total - prev
		if delta <= 0 {
			continue
		}
		entry := phases[m.Phase]
		switch m.Name {
		case FsRead:
			entry.active = true
			entry.files += delta
		case FsDiscovered:
			entry.active = true
			entry.discovered += delta
		case FsCompleted:
			entry.active = true
			entry.completed += delta
		case SemanticChunks:
			entry.active = true
			entry.chunks += delta
		case ProviderCalls:
			entry.active = true
			entry.calls += delta
		case ProviderTexts:
			entry.active = true
			entry.texts += delta
		default:
			entry.active = true
		}
		phases[m.Phase] = entry
	}
	for m, value := range c.gauges {
		if value == 0 && c.gaugeStats[m].Count == c.lastGaugeStats[m].Count {
			continue
		}
		entry := phases[m.Phase]
		entry.active = true
		phases[m.Phase] = entry
	}

	for metric, sample := range c.samples {
		if sample.Count > c.lastSampleCounts[metric] {
			entry := phases[metric.Phase]
			entry.active = true
			phases[metric.Phase] = entry
		}
	}
	delete(phases, "")
	for phase, entry := range phases {
		if !entry.active {
			delete(phases, phase)
		}
	}
	return phases
}

func betterWindowPhase(candidate, current windowPhaseStats) bool {
	if hasProviderActivity(candidate) != hasProviderActivity(current) {
		return hasProviderActivity(candidate)
	}
	if candidate.provider != current.provider {
		return candidate.provider > current.provider
	}
	if candidate.prep != current.prep {
		return candidate.prep > current.prep
	}
	if candidate.texts != current.texts {
		return candidate.texts > current.texts
	}
	if candidate.calls != current.calls {
		return candidate.calls > current.calls
	}
	if candidate.chunks != current.chunks {
		return candidate.chunks > current.chunks
	}
	if candidate.db != current.db {
		return candidate.db > current.db
	}
	if candidate.queue != current.queue {
		return candidate.queue > current.queue
	}
	if candidate.files != current.files {
		return candidate.files > current.files
	}
	if candidate.discovered != current.discovered {
		return candidate.discovered > current.discovered
	}
	if candidate.completed != current.completed {
		return candidate.completed > current.completed
	}
	return candidate.gate > current.gate
}

func hasProviderActivity(stats windowPhaseStats) bool {
	return stats.provider > 0 || stats.calls > 0 || stats.texts > 0
}

func (c *Collector) advanceWindowSnapshots() {
	clear(c.windowLatencies)
	clear(c.windowSamples)
	c.windowDroppedSamples = 0
	for m, stat := range c.counters {
		c.lastCounters[m] = stat.Total
	}
	for m, stat := range c.latencies {
		c.lastLatencies[m] = stat.Total
		c.lastLatencyCounts[m] = stat.Count
	}
	for m, stat := range c.gaugeStats {
		c.lastGaugeStats[m] = stat
	}
	for m, stat := range c.samples {
		c.lastSampleCounts[m] = stat.Count
	}
	for m := range c.windowGaugePeaks {
		delete(c.windowGaugePeaks, m)
	}
}

func (c *Collector) DominantWindowPhase() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.windowPhase()
}
