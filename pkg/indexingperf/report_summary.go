package indexingperf

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// RenderSummary renders the cumulative `rzm index --timings` diagnostic report.
//
// Docs:
// - [[indexing-observability-maintenance-policy#^spec-0047-us2]]
//
// WHY: phase labels are part of the maintenance/debugging contract; avoid broad
// wrapper names that hide planning, provider, queue, DB-write, graph, or upkeep cost.
func (c *Collector) RenderSummary() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	var b strings.Builder
	b.WriteString("Index timings:\n")
	phaseOrder := []string{
		"open_config",
		"open_stores",
		"ownership_discovery",
		"ownership_transition",
		"index_code",
		"rebuild_call_edges",
		"ingest_notes",
		"validation_projection_source",
		"validation_projection_ontology",
		"plan_ontology",
		"load_ontology_inputs",
		"compute_ontology",
		"write_ontology",
		"plan_code_embeddings",
		"plan_note_embeddings",
		"embed_code",
		"embed_notes",
		"embed_ontology_nodes",
		"shared_provider",
		"sync_ontology_body",
		"sync_ontology_body_final",
		"sync_intent_embeddings",
		"compute_graph",
		"analyze_optimize_checkpoint",
		"vacuum",
		"total",
	}
	for _, phase := range phaseOrder {
		span, hasSpan := c.spans[phase]
		parts := c.phaseSummaryParts(phase)
		parts = append(parts, c.phaseCoveragePartsLocked(phase)...)
		if !hasSpan && len(parts) == 0 {
			continue
		}
		if hasSpan {
			fmt.Fprintf(&b, "- %-28s %7s", phase, shortDur(span.Duration))
		} else {
			// Counters and latency may be observed by a shared provider node even
			// when its caller has no enclosing phase span. Calling this duration
			// zero would falsely claim that the work was instantaneous.
			fmt.Fprintf(&b, "- %-28s %7s", phase, "unspanned")
		}
		if span.Count > 1 {
			fmt.Fprintf(&b, " x%d", span.Count)
		}
		if len(parts) > 0 {
			b.WriteString("   ")
			b.WriteString(strings.Join(parts, " "))
		}
		if hasSpan && span.Status != "" && span.Status != "ok" {
			b.WriteByte(' ')
			b.WriteString(span.Status)
		}
		b.WriteByte('\n')
	}

	if len(c.dbWrites) > 0 {
		b.WriteString("Write-locked DB ops:\n")
		writeKeys := make([]string, 0, len(c.dbWrites))
		readKeys := make([]string, 0, len(c.dbWrites))
		for key, row := range c.dbWrites {
			if isReadLikeDBOp(row.Op) {
				readKeys = append(readKeys, key)
				continue
			}
			writeKeys = append(writeKeys, key)
		}
		sort.Strings(writeKeys)
		sort.Strings(readKeys)
		if len(writeKeys) > 0 {
			b.WriteString("- writes:\n")
			for _, key := range writeKeys {
				row := c.dbWrites[key]
				fmt.Fprintf(&b, "  - %s count=%d wait=%s hold=%s\n",
					row.Op, row.Count, shortDur(row.Wait), shortDur(row.Hold))
			}
		}
		if len(readKeys) > 0 {
			b.WriteString("- read-like:\n")
			for _, key := range readKeys {
				row := c.dbWrites[key]
				fmt.Fprintf(&b, "  - %s count=%d wait=%s hold=%s\n",
					row.Op, row.Count, shortDur(row.Wait), shortDur(row.Hold))
			}
		}
	}
	if bottlenecks := c.renderBottlenecks(); bottlenecks != "" {
		b.WriteByte('\n')
		b.WriteString(bottlenecks)
	}
	if overlaps := c.renderOverlaps(); overlaps != "" {
		b.WriteByte('\n')
		b.WriteString(overlaps)
	}
	if critical := c.renderCriticalPathWalls(); critical != "" {
		b.WriteByte('\n')
		b.WriteString(critical)
	}
	if coverage := c.renderCumulativeCoverageLocked(); coverage != "" {
		b.WriteByte('\n')
		b.WriteString(coverage)
	}
	return strings.TrimRight(b.String(), "\n")
}

type bottleneckRow struct {
	phase string
	label string
	dur   time.Duration
}

func (c *Collector) renderBottlenecks() string {
	rows := make([]bottleneckRow, 0)
	for phase := range c.spans {
		for _, candidate := range []struct {
			label string
			name  string
		}{
			{label: "provider_busy", name: "provider.latency"},
			{label: "prepare_blocked", name: "node.prepare.blocked"},
			{label: "embed_starved", name: "node.embed.starved"},
			{label: "embed_pack_wait", name: "node.embed.pack_wait"},
			{label: "writer_busy", name: "node.write.busy"},
			{label: "writer_deferred", name: "node.write.deferred"},
			{label: "enqueue_blocked", name: "queue.wait"},
		} {
			if dur := c.phaseLatencyTotal(phase, candidate.name); dur > 0 {
				rows = append(rows, bottleneckRow{phase: phase, label: candidate.label, dur: dur})
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
	b.WriteString("Bottlenecks:\n")
	for _, row := range rows {
		fmt.Fprintf(&b, "- %s %s=%s\n", row.phase, row.label, shortDur(row.dur))
	}
	return strings.TrimRight(b.String(), "\n")
}

func isReadLikeDBOp(op string) bool {
	op = strings.ToLower(strings.TrimSpace(op))
	if op == "" {
		return false
	}
	readHints := []string{
		"callfilesbycallees",
		"lookup",
		"list",
		"bypath",
		"bypaths",
		"referrerpaths",
		"calleesforfiles",
		"calleesforowners",
		"annotationuses",
	}
	for _, hint := range readHints {
		if strings.Contains(op, hint) {
			return true
		}
	}
	return false
}
