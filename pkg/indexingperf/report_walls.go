package indexingperf

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type overlapRow struct {
	a   string
	b   string
	dur time.Duration
}

func (c *Collector) renderOverlaps() string {
	rows := make([]overlapRow, 0)
	names := make([]string, 0, len(c.spans))
	for name := range c.spans {
		if name == "total" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			a := c.spans[names[i]]
			b := c.spans[names[j]]
			if len(a.Windows) == 0 || len(b.Windows) == 0 {
				continue
			}
			if overlap := overlapUnionDuration(a.Windows, b.Windows); overlap > 0 {
				rows = append(rows, overlapRow{a: names[i], b: names[j], dur: overlap})
			}
		}
	}
	if len(rows) == 0 {
		return ""
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].dur == rows[j].dur {
			if rows[i].a == rows[j].a {
				return rows[i].b < rows[j].b
			}
			return rows[i].a < rows[j].a
		}
		return rows[i].dur > rows[j].dur
	})
	if len(rows) > 6 {
		rows = rows[:6]
	}
	var b strings.Builder
	b.WriteString("Overlaps:\n")
	for _, row := range rows {
		fmt.Fprintf(&b, "- %s ∩ %s = %s\n", row.a, row.b, shortDur(row.dur))
	}
	return strings.TrimRight(b.String(), "\n")
}

func overlapUnionDuration(a, b []timeWindow) time.Duration {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	aw := normalizeTimeWindows(a)
	bw := normalizeTimeWindows(b)
	var total time.Duration
	i, j := 0, 0
	for i < len(aw) && j < len(bw) {
		start := maxTime(aw[i].StartedAt, bw[j].StartedAt)
		end := minTime(aw[i].EndedAt, bw[j].EndedAt)
		if end.After(start) {
			total += end.Sub(start)
		}
		if aw[i].EndedAt.Before(bw[j].EndedAt) {
			i++
			continue
		}
		j++
	}
	return total
}

func normalizeTimeWindows(windows []timeWindow) []timeWindow {
	if len(windows) == 0 {
		return nil
	}
	sorted := append([]timeWindow(nil), windows...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].StartedAt.Equal(sorted[j].StartedAt) {
			return sorted[i].EndedAt.Before(sorted[j].EndedAt)
		}
		return sorted[i].StartedAt.Before(sorted[j].StartedAt)
	})
	out := make([]timeWindow, 0, len(sorted))
	for _, window := range sorted {
		if window.EndedAt.Before(window.StartedAt) {
			window.EndedAt = window.StartedAt
		}
		if len(out) == 0 {
			out = append(out, window)
			continue
		}
		last := &out[len(out)-1]
		if !window.StartedAt.After(last.EndedAt) {
			if window.EndedAt.After(last.EndedAt) {
				last.EndedAt = window.EndedAt
			}
			continue
		}
		out = append(out, window)
	}
	return out
}

type criticalWallRow struct {
	phase string
	label string
	dur   time.Duration
}

func (c *Collector) renderCriticalPathWalls() string {
	rows := make([]criticalWallRow, 0)
	for phase, span := range c.spans {
		if phase == "total" {
			continue
		}
		if span.Duration > 0 {
			rows = append(rows, criticalWallRow{phase: phase, label: "phase_wall", dur: span.Duration})
		}
	}
	for _, candidate := range []struct {
		label string
		name  string
	}{
		{label: "provider_wall", name: "provider.latency"},
		{label: "writer_busy_wall", name: "node.write.busy"},
	} {
		for phase := range c.spans {
			if dur := c.phaseIntervalUnion(phase, candidate.name); dur > 0 {
				rows = append(rows, criticalWallRow{phase: phase, label: candidate.label, dur: dur})
			}
		}
	}
	if len(rows) == 0 {
		return ""
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].dur == rows[j].dur {
			if rows[i].phase == rows[j].phase {
				return rows[i].label < rows[j].label
			}
			return rows[i].phase < rows[j].phase
		}
		return rows[i].dur > rows[j].dur
	})
	if len(rows) > 6 {
		rows = rows[:6]
	}
	var b strings.Builder
	b.WriteString("Critical path walls:\n")
	for _, row := range rows {
		fmt.Fprintf(&b, "- %s %s=%s\n", row.phase, row.label, shortDur(row.dur))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (c *Collector) phaseIntervalUnion(phase, name string) time.Duration {
	var windows []timeWindow
	for m, vals := range c.intervals {
		if m.Phase == phase && m.Name == name {
			windows = append(windows, vals...)
		}
	}
	return unionDuration(windows)
}

func unionDuration(windows []timeWindow) time.Duration {
	if len(windows) == 0 {
		return 0
	}
	sort.Slice(windows, func(i, j int) bool {
		if windows[i].StartedAt.Equal(windows[j].StartedAt) {
			return windows[i].EndedAt.Before(windows[j].EndedAt)
		}
		return windows[i].StartedAt.Before(windows[j].StartedAt)
	})
	cur := windows[0]
	var total time.Duration
	for _, w := range windows[1:] {
		if !w.StartedAt.After(cur.EndedAt) {
			if w.EndedAt.After(cur.EndedAt) {
				cur.EndedAt = w.EndedAt
			}
			continue
		}
		total += cur.EndedAt.Sub(cur.StartedAt)
		cur = w
	}
	total += cur.EndedAt.Sub(cur.StartedAt)
	return total
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
