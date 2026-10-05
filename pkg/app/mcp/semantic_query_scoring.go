package mcp

import (
	"strings"

	"github.com/atomicobject/rhizome/pkg/search"
)

func evidenceSummary(evs []search.Evidence) (map[string]float64, int) {
	if len(evs) == 0 {
		return nil, 0
	}
	out := map[string]float64{}
	for _, ev := range evs {
		cat := categorizeEvidence(ev.Type)
		if cat == "" {
			continue
		}
		if prev, ok := out[cat]; !ok || ev.RawScore > prev {
			out[cat] = ev.RawScore
		}
	}
	diversity := 0
	for _, v := range out {
		if v > 0 {
			diversity++
		}
	}
	return out, diversity
}

func categorizeEvidence(typ string) string {
	typ = strings.ToLower(strings.TrimSpace(typ))
	switch {
	case typ == "":
		return ""
	case strings.Contains(typ, "vector"):
		return "vector"
	case strings.Contains(typ, "lexical") || strings.Contains(typ, "fts") || strings.Contains(typ, "bm25") || strings.Contains(typ, "title"):
		return "lexical"
	case strings.Contains(typ, "tests_path"):
		return "lexical"
	case strings.HasPrefix(typ, "graph") || typ == "same_community":
		return "graph"
	case strings.Contains(typ, "fusion"):
		return "fusion"
	case strings.Contains(typ, "code_anchor") || strings.Contains(typ, "definition_anchor") || strings.Contains(typ, "symbol_") || strings.Contains(typ, "call_edge") || strings.Contains(typ, "code_ref") || strings.Contains(typ, "doc_link") || strings.Contains(typ, "anchor_graph") || strings.Contains(typ, "outgoing_link"):
		return "refs"
	default:
		return ""
	}
}

func isTestPath(path string) bool {
	return search.IsTestPath(path)
}

func presentationRankWeight(rank, total int) float64 {
	if total <= 1 {
		return 1.2
	}
	if rank < 0 {
		rank = 0
	}
	if rank > total-1 {
		rank = total - 1
	}
	pos := float64(rank) / float64(total-1)
	factor := 1.35 - 0.85*pos
	if factor < 0.5 {
		return 0.5
	}
	if factor > 1.5 {
		return 1.5
	}
	return factor
}
