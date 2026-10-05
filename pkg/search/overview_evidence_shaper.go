package search

import (
	"context"
	"strings"
)

// CompositeShaper applies multiple result shapers in order.
type CompositeShaper struct {
	Shapers []Shaper
}

func (s *CompositeShaper) Shape(ctx context.Context, spec QuerySpec, results []RankedResult) ([]RankedResult, error) {
	var err error
	for _, shaper := range s.Shapers {
		if shaper == nil {
			continue
		}
		results, err = shaper.Shape(ctx, spec, results)
		if err != nil {
			return results, err
		}
	}
	return results, nil
}

// OverviewEvidenceShaper keeps overview packets doc-led while ensuring credible
// representative implementation anchors are visible early enough to act on.
type OverviewEvidenceShaper struct {
	Window        int
	MaxPromotions int
	MinScoreRatio float64
}

func (s *OverviewEvidenceShaper) Shape(ctx context.Context, spec QuerySpec, results []RankedResult) ([]RankedResult, error) {
	_ = ctx
	if spec.Intent != IntentOverview && spec.Intent != IntentSubsystemOverview {
		return results, nil
	}
	if len(results) < 2 {
		return results, nil
	}

	window := s.Window
	if window <= 0 {
		window = 8
	}
	window = min(window, len(results))
	desiredCode := s.maxPromotions(window)
	codeInWindow := 0
	for _, rr := range results[:window] {
		if s.credibleOverviewCode(rr, results[0].FinalScore) {
			codeInWindow++
		}
	}
	if codeInWindow >= desiredCode {
		return results, nil
	}

	out := append([]RankedResult(nil), results...)
	promoted := 0
	for i := window; i < len(out) && codeInWindow < desiredCode; i++ {
		if !s.credibleOverviewCode(out[i], results[0].FinalScore) {
			continue
		}
		target := min(2+promoted, window-1)
		item := out[i]
		item.Evidence = append(item.Evidence, Evidence{
			Type:   "overview_shaped",
			Source: "overview",
			Details: map[string]string{
				"reason": "representative_code",
			},
		})
		copy(out[target+1:i+1], out[target:i])
		out[target] = item
		promoted++
		codeInWindow++
	}
	if promoted > 0 {
		AddRuntimeWarning(ctx, Warning{
			Code:    "overview_representative_code",
			Kind:    "result_shaping",
			Source:  "overview",
			Message: "Overview promoted representative code results into the first page.",
		})
	}
	return out, nil
}

func (s *OverviewEvidenceShaper) maxPromotions(window int) int {
	if s.MaxPromotions > 0 {
		return s.MaxPromotions
	}
	if window >= 10 {
		return 3
	}
	return 2
}

func (s *OverviewEvidenceShaper) minScoreRatio() float64 {
	if s.MinScoreRatio > 0 {
		return s.MinScoreRatio
	}
	return 0.45
}

func (s *OverviewEvidenceShaper) credibleOverviewCode(rr RankedResult, bestScore float64) bool {
	if rr.Type != "code" || strings.TrimSpace(rr.Path) == "" || IsTestPath(rr.Path) {
		return false
	}
	if bestScore > 0 && rr.FinalScore < bestScore*s.minScoreRatio() {
		return false
	}
	if IsEntryPointFile(rr.Path) && !hasOnlyPathEvidence(rr.Evidence) {
		return true
	}
	for _, ev := range rr.Evidence {
		switch strings.TrimSpace(strings.ToLower(ev.Type)) {
		case "code_vector_similarity", "anchor_vector_similarity", "symbol_exact", "symbol_match",
			"definition_anchor", "code_ref", "call_edge", "anchor_graph_edge", "graph_anchor_pagerank":
			return true
		case "intel_fts_match":
			if !isPathOnlyEvidence(ev) && evidenceScore(ev) >= 0.35 {
				return true
			}
		}
	}
	return strings.TrimSpace(rr.Symbol) != "" || strings.TrimSpace(rr.FQN) != "" || strings.TrimSpace(rr.AnchorID) != ""
}

func hasOnlyPathEvidence(evidence []Evidence) bool {
	if len(evidence) == 0 {
		return false
	}
	for _, ev := range evidence {
		if !isPathOnlyEvidence(ev) {
			return false
		}
	}
	return true
}

func isPathOnlyEvidence(ev Evidence) bool {
	return strings.EqualFold(strings.TrimSpace(ev.Type), "intel_fts_match") &&
		strings.EqualFold(strings.TrimSpace(ev.Details["pathOnly"]), "true")
}

func evidenceScore(ev Evidence) float64 {
	if ev.Score > 0 {
		return ev.Score
	}
	return ev.RawScore
}
