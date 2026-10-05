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
		case "provider.latency":
			entry.active = true
			entry.provider += delta
		case "provider.request.voyage.backoff":
			// Backoff is active provider work, but it is not a completed provider
			// latency sample and must not inflate provider throughput timing.
			entry.active = true
		case "codeembed.prepare":
			entry.active = true
			entry.prep += delta
		case "queue.wait":
			entry.active = true
			entry.queue += delta
		case "db.wait", "db.hold":
			entry.active = true
			entry.db += delta
		case "embed.gate_wait":
			entry.active = true
			entry.gate += delta
		case "fs.walk":
			entry.active = true
			entry.queue += delta
		default:
			if strings.HasPrefix(m.Name, "calledge.") {
				entry.active = true
			} else {
				continue
			}
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
		case "fs.read":
			entry.active = true
			entry.files += delta
		case "fs.discovered":
			entry.active = true
			entry.discovered += delta
		case "fs.completed":
			entry.active = true
			entry.completed += delta
		case "semantic.chunks":
			entry.active = true
			entry.chunks += delta
		case "provider.calls":
			entry.active = true
			entry.calls += delta
		case "provider.texts":
			entry.active = true
			entry.texts += delta
		default:
			if m.Name == "provider.request.voyage" || strings.HasPrefix(m.Name, "provider.request.voyage.") ||
				strings.HasPrefix(m.Name, "calledge.") ||
				strings.HasPrefix(m.Name, "semantic.suppressed_anchors.") ||
				strings.HasPrefix(m.Name, "semantic.code_chunks_pruned.") ||
				strings.HasPrefix(m.Name, "semantic.code_plan.") ||
				strings.HasPrefix(m.Name, "semantic.code_chunks.") {
				entry.active = true
			} else {
				continue
			}
		}
		phases[m.Phase] = entry
	}
	for m, value := range c.gauges {
		if (m.Name != "provider.inflight" && m.Name != "intent.writeback.pending_rows") || value <= 0 {
			continue
		}
		entry := phases[m.Phase]
		entry.active = true
		phases[m.Phase] = entry
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
