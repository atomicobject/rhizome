package graph

import (
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	isoDateRegex     = regexp.MustCompile(`\d{4}-\d{2}-\d{2}(?:[T _]?\d{2}:?\d{2}(?::?\d{2})?)?`)
	headingDateRegex = regexp.MustCompile(`(?m)^\s{0,3}#*\s*(\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}(?::\d{2})?)?)\b`)
)

func applyNeighborRecency(adjacency map[string]map[string]struct{}, baseTimes map[string]time.Time, now time.Time, cascade bool) map[string]time.Time {
	inbound := make(map[string][]string)
	for src, targets := range adjacency {
		for dst := range targets {
			inbound[dst] = append(inbound[dst], src)
		}
	}

	nodes := make([]string, 0, len(adjacency))
	for node := range adjacency {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)

	clampedBase := make(map[string]time.Time, len(baseTimes))
	for path, ts := range baseTimes {
		if ts.IsZero() {
			continue
		}
		if ts.After(now) {
			ts = now
		}
		clampedBase[path] = ts
	}

	if !cascade {
		return recencyPass(nodes, adjacency, inbound, clampedBase, clampedBase, now, nil)
	}

	current := make(map[string]time.Time, len(adjacency))
	for _, node := range nodes {
		if ts := clampedBase[node]; !ts.IsZero() {
			current[node] = ts
		}
	}

	for pass := 0; pass < recencyPropagationPasses; pass++ {
		next := recencyPass(nodes, adjacency, inbound, clampedBase, current, now, current)
		current = next
	}

	return current
}

func recencyPass(nodes []string, adjacency map[string]map[string]struct{}, inbound map[string][]string, base map[string]time.Time, neighborTimes map[string]time.Time, now time.Time, previous map[string]time.Time) map[string]time.Time {
	type neighborTime struct {
		path string
		ts   time.Time
	}

	effective := make(map[string]time.Time, len(adjacency))
	if len(nodes) == 0 {
		return effective
	}

	workerCount := runtime.NumCPU()
	if workerCount < 1 {
		workerCount = 1
	}
	if workerCount > len(nodes) {
		workerCount = len(nodes)
	}

	chunkSize := (len(nodes) + workerCount - 1) / workerCount
	partials := make([]map[string]time.Time, workerCount)

	var wg sync.WaitGroup
	for w := 0; w < workerCount; w++ {
		start := w * chunkSize
		end := start + chunkSize
		if end > len(nodes) {
			end = len(nodes)
		}
		if start >= end {
			continue
		}
		wg.Add(1)
		go func(idx int, slice []string) {
			defer wg.Done()
			local := make(map[string]time.Time, len(slice))
			for _, node := range slice {
				best := base[node]
				if prev, ok := previous[node]; ok && prev.After(best) {
					best = prev
				}

				seen := make(map[string]struct{})
				var neighbors []neighborTime

				for dst := range adjacency[node] {
					if ts := neighborTimes[dst]; !ts.IsZero() {
						if ts.After(now) {
							ts = now
						}
						if _, dup := seen[dst]; !dup {
							seen[dst] = struct{}{}
							neighbors = append(neighbors, neighborTime{path: dst, ts: ts})
						}
					}
				}
				for _, src := range inbound[node] {
					if ts := neighborTimes[src]; !ts.IsZero() {
						if ts.After(now) {
							ts = now
						}
						if _, dup := seen[src]; !dup {
							seen[src] = struct{}{}
							neighbors = append(neighbors, neighborTime{path: src, ts: ts})
						}
					}
				}

				sort.Slice(neighbors, func(i, j int) bool {
					return neighbors[i].ts.After(neighbors[j].ts)
				})
				if len(neighbors) > neighborSampleLimit {
					neighbors = neighbors[:neighborSampleLimit]
				}

				for _, n := range neighbors {
					age := now.Sub(n.ts)
					if age < -maxFutureTolerance {
						continue
					}
					if age > neighborFreshWindow {
						continue
					}
					adjusted := n.ts.Add(-neighborStalenessOffset)
					if best.IsZero() || adjusted.After(best) {
						best = adjusted
					}
				}

				if !best.IsZero() {
					local[node] = best
				}
			}
			partials[idx] = local
		}(w, nodes[start:end])
	}

	wg.Wait()

	for _, part := range partials {
		for k, v := range part {
			effective[k] = v
		}
	}

	return effective
}

// ResolveContentTime extracts a content-driven timestamp from a note using frontmatter,
// filename dates, and in-note headings/lines. It rejects non-ISO or implausible dates.
func ResolveContentTime(path, content string) (time.Time, bool) {
	now := time.Now()

	if t, ok := parseDateFromFrontmatter(content, now); ok {
		return clampFuture(t, now), true
	}
	if t, ok := parseDateFromFilename(path, now); ok {
		return clampFuture(t, now), true
	}
	if t, ok := parseMostRecentHeadingDate(content, now); ok {
		return clampFuture(t, now), true
	}
	if t, ok := parseDateFromTopLines(content, now, topLinesForDateDetection); ok {
		return clampFuture(t, now), true
	}
	return time.Time{}, false
}

func parseDateFromFrontmatter(content string, now time.Time) (time.Time, bool) {
	frontmatter, err := markdownsyntax.ExtractFrontmatter(content)
	if err != nil || frontmatter == nil {
		return time.Time{}, false
	}
	// Prefer event/meeting, then "last updated" style fields before creation.
	candidates := []string{"event_date", "meeting_date", "updated", "modified", "date", "created"}
	for _, key := range candidates {
		if raw, ok := frontmatter[key]; ok {
			if t, ok := parseDateValue(raw, now); ok {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

func parseDateValue(raw interface{}, now time.Time) (time.Time, bool) {
	switch v := raw.(type) {
	case string:
		return parseISOTimestamp(v, now)
	case time.Time:
		if saneTimestamp(v, now) {
			return v, true
		}
	case *time.Time:
		if v != nil && saneTimestamp(*v, now) {
			return *v, true
		}
	case []interface{}:
		for _, item := range v {
			if t, ok := parseDateValue(item, now); ok {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

func parseDateFromFilename(path string, now time.Time) (time.Time, bool) {
	match := isoDateRegex.FindString(path)
	if match == "" {
		return time.Time{}, false
	}
	return parseISOTimestamp(match, now)
}

func parseDateFromTopLines(content string, now time.Time, limit int) (time.Time, bool) {
	lines := strings.Split(content, "\n")
	if limit > len(lines) {
		limit = len(lines)
	}
	for i := 0; i < limit; i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if match := isoDateRegex.FindString(line); match != "" {
			if t, ok := parseISOTimestamp(match, now); ok {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

func parseMostRecentHeadingDate(content string, now time.Time) (time.Time, bool) {
	matches := headingDateRegex.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return time.Time{}, false
	}
	var latest time.Time
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		candidate := m[1]
		if t, ok := parseISOTimestamp(candidate, now); ok {
			if latest.IsZero() || t.After(latest) {
				latest = t
			}
		}
	}
	if latest.IsZero() {
		return time.Time{}, false
	}
	return latest, true
}

func parseISOTimestamp(value string, now time.Time) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02",
		"2006-01-02T15:04",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04",
		"2006-01-02 15:04:05",
		"2006-01-02T1504",
		"2006-01-02T150405",
	}
	candidate := value
	if m := isoDateRegex.FindString(value); m != "" {
		candidate = m
	}
	for _, layout := range layouts {
		if ts, err := time.Parse(layout, candidate); err == nil {
			if saneTimestamp(ts, now) {
				return ts.UTC(), true
			}
		}
	}
	return time.Time{}, false
}

func clampFuture(ts time.Time, now time.Time) time.Time {
	if ts.After(now) {
		return now
	}
	return ts
}

func saneTimestamp(ts time.Time, now time.Time) bool {
	if ts.IsZero() {
		return false
	}
	if ts.Year() < minSaneYear {
		return false
	}
	if ts.After(now.Add(maxFutureTolerance)) {
		return false
	}
	return true
}

func communityRecency(members []string, modTimes map[string]time.Time, windowDays int) *GraphRecency {
	if len(members) == 0 || windowDays <= 0 {
		return nil
	}
	latestPath := ""
	var latestTime time.Time
	recentCount := 0
	window := time.Duration(windowDays) * 24 * time.Hour
	now := time.Now()

	for _, m := range members {
		mt, ok := modTimes[m]
		if !ok || mt.IsZero() {
			continue
		}
		if latestTime.IsZero() || mt.After(latestTime) {
			latestTime = mt
			latestPath = m
		}
		if now.Sub(mt) <= window {
			recentCount++
		}
	}
	if latestTime.IsZero() && recentCount == 0 {
		return nil
	}
	age := 0.0
	if !latestTime.IsZero() {
		age = now.Sub(latestTime).Hours() / 24.0
		if age < 0 {
			age = 0
		}
	}
	return &GraphRecency{
		LatestPath:      latestPath,
		LatestAgeDays:   age,
		LatestTimestamp: latestTime,
		RecentCount:     recentCount,
		WindowDays:      windowDays,
	}
}
