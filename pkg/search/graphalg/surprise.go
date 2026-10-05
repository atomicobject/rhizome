package graphalg

import (
	"fmt"
	"sort"
	"strings"
)

// SurprisingConnection is one scored surprising edge.
type SurprisingConnection struct {
	SrcPath    string
	DstPath    string
	EdgeKind   string
	Score      float64
	Confidence string   // "extracted", "inferred", "ambiguous"
	Reasons    []string // human-readable explanations
}

// GraphDocScoreMinimal is the minimal score data needed for surprise scoring.
type GraphDocScoreMinimal struct {
	DocPath   string
	DocType   string // "note" or "code"
	Community string
	Inbound   int
	Outbound  int
	Authority float64
}

// GraphDocEdgeForSurprise is the edge data needed for scoring.
type GraphDocEdgeForSurprise struct {
	SrcPath         string
	DstPath         string
	Kind            string
	Confidence      string
	ConfidenceScore float64
}

// FindSurprisingConnections scores all edges and returns top N most surprising.
// Uses Mode 1 (community-based scoring) when scores are non-empty.
// Falls back to Mode 2 (edge betweenness centrality) when scores is empty.
func FindSurprisingConnections(
	edges []GraphDocEdgeForSurprise,
	scores map[string]GraphDocScoreMinimal,
	topN int,
	minScore float64,
) []SurprisingConnection {
	if len(edges) == 0 {
		return nil
	}

	if len(scores) == 0 {
		return filterByMinScore(edgeBetweennessFallback(edges, topN), minScore)
	}

	return mode1Scoring(edges, scores, topN, minScore)
}

func filterByMinScore(edges []SurprisingConnection, minScore float64) []SurprisingConnection {
	if minScore <= 0 {
		return edges
	}
	out := edges[:0]
	for _, edge := range edges {
		if edge.Score >= minScore {
			out = append(out, edge)
		}
	}
	return out
}

// isPureHub returns true when a node qualifies as a pure hub (high outbound, low inbound).
// Pure hubs are index/MOC notes that create noisy edges; we skip them.
func isPureHub(sc GraphDocScoreMinimal) bool {
	return sc.Outbound > 50 && sc.Inbound < 3
}

// isExplicitLink returns true for edge kinds that represent explicit authoring choices.
// These are not "surprising" because the author deliberately made the connection.
func isExplicitLink(kind string) bool {
	switch kind {
	case "wikilink", "mdlink":
		return true
	}
	return strings.HasPrefix(kind, "note_link:")
}

// mode1Scoring scores edges using community + type boundary + confidence signals.
func mode1Scoring(
	edges []GraphDocEdgeForSurprise,
	scores map[string]GraphDocScoreMinimal,
	topN int,
	minScore float64,
) []SurprisingConnection {
	var candidates []SurprisingConnection

	for _, edge := range edges {
		// Skip self-loops.
		if edge.SrcPath == edge.DstPath {
			continue
		}

		// Skip explicit authoring choices (wikilinks, mdlinks, note_link:*).
		if isExplicitLink(edge.Kind) {
			continue
		}

		// Skip pure-hub endpoints: outbound > 50 && inbound < 3.
		srcScore, srcOK := scores[edge.SrcPath]
		dstScore, dstOK := scores[edge.DstPath]
		if srcOK && isPureHub(srcScore) {
			continue
		}
		if dstOK && isPureHub(dstScore) {
			continue
		}

		score, reasons := computeSurpriseScore(edge, srcScore, srcOK, dstScore, dstOK)

		if score < minScore {
			continue
		}

		conf := edge.Confidence
		if conf == "" {
			conf = "extracted"
		}

		candidates = append(candidates, SurprisingConnection{
			SrcPath:    edge.SrcPath,
			DstPath:    edge.DstPath,
			EdgeKind:   edge.Kind,
			Score:      score,
			Confidence: conf,
			Reasons:    reasons,
		})
	}

	// Sort by score descending, then by src+dst for determinism.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		if candidates[i].SrcPath != candidates[j].SrcPath {
			return candidates[i].SrcPath < candidates[j].SrcPath
		}
		return candidates[i].DstPath < candidates[j].DstPath
	})

	// Deduplicate by community pair: keep only the highest-scored edge per pair.
	// Skip dedup when either community is empty.
	deduped := dedupByCommunityPair(candidates, scores)

	// Apply topN.
	if topN > 0 && len(deduped) > topN {
		deduped = deduped[:topN]
	}
	return deduped
}

// computeSurpriseScore computes the additive surprise score for an edge.
// Returns (score, reasons).
func computeSurpriseScore(
	edge GraphDocEdgeForSurprise,
	srcScore GraphDocScoreMinimal, srcOK bool,
	dstScore GraphDocScoreMinimal, dstOK bool,
) (float64, []string) {
	score := 0.0
	var reasons []string

	// 1. Confidence weight.
	// Lower confidence is more surprising, and the numeric score matters so near-misses
	// can rank differently even when the label is the same.
	confValue := ConfidenceValue(edge.Confidence, edge.ConfidenceScore)
	score += ConfidenceSurpriseMultiplier(edge.Confidence, edge.ConfidenceScore)
	reasons = append(reasons, fmt.Sprintf("confidence score %.2f", confValue))

	// 2. Cross-community bonus.
	if srcOK && dstOK && srcScore.Community != "" && dstScore.Community != "" {
		if srcScore.Community != dstScore.Community {
			score += 2.0
			reasons = append(reasons, fmt.Sprintf("bridges community %s ↔ %s",
				srcScore.Community, dstScore.Community))
		}
	}

	// 3. Cross-type bonus (code ↔ note).
	if srcOK && dstOK && srcScore.DocType != dstScore.DocType {
		score += 2.0
		reasons = append(reasons, fmt.Sprintf("crosses %s ↔ %s boundary",
			srcScore.DocType, dstScore.DocType))
	}

	// 4. Peripheral→hub detection.
	if srcOK && dstOK {
		srcDeg := srcScore.Inbound + srcScore.Outbound
		dstDeg := dstScore.Inbound + dstScore.Outbound
		minDeg := srcDeg
		maxDeg := dstDeg
		if dstDeg < minDeg {
			minDeg, maxDeg = dstDeg, srcDeg
		}
		if minDeg <= 2 && maxDeg >= 8 {
			score += 1.5
			reasons = append(reasons, "peripheral node reaches high-degree hub")
		}
	}

	// 5. Low-authority endpoints (both below threshold).
	if srcOK && dstOK && srcScore.Authority < 0.1 && dstScore.Authority < 0.1 {
		score += 1.0
		reasons = append(reasons, "connects two low-authority nodes")
	}

	return score, reasons
}

// dedupByCommunityPair keeps the highest-scored edge per (communityA, communityB) pair.
// Edges where either community is empty skip dedup and are always included.
// Input must be sorted by score descending.
func dedupByCommunityPair(candidates []SurprisingConnection, scores map[string]GraphDocScoreMinimal) []SurprisingConnection {
	seen := make(map[string]struct{})
	var out []SurprisingConnection

	for _, c := range candidates {
		srcScore, srcOK := scores[c.SrcPath]
		dstScore, dstOK := scores[c.DstPath]

		if !srcOK || !dstOK || srcScore.Community == "" || dstScore.Community == "" {
			// No community info — always include.
			out = append(out, c)
			continue
		}

		// Build canonical pair key (sorted so A↔B == B↔A).
		commA, commB := srcScore.Community, dstScore.Community
		if commA > commB {
			commA, commB = commB, commA
		}
		key := commA + "\x00" + commB

		if _, already := seen[key]; already {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, c)
	}
	return out
}
