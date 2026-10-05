package unifiedsearch

import (
	"context"
	"path/filepath"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
)

// targetCandidateResults materializes an ambiguous precision target's
// candidates as ranked results so callers see the choices instead of an empty
// page. The evidence type is never primary, so candidates stay supporting and
// the target status stays ambiguous.
func targetCandidateResults(ctx context.Context, store *semdb.Store, filters search.Filters, candidates []search.TargetCandidate) []search.RankedResult {
	if len(candidates) == 0 {
		return nil
	}
	fqns := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if fqn := strings.TrimSpace(candidate.FQN); fqn != "" {
			fqns = append(fqns, fqn)
		}
	}
	anchorsByFQN := map[string][]codeanchor.IntelAnchor{}
	if store != nil && len(fqns) > 0 {
		if found, err := store.IntelAnchorsByFQNsLimited(ctx, fqns, 1); err == nil {
			for fqn, anchors := range found {
				anchorsByFQN[fqn] = anchors
			}
		}
	}
	out := make([]search.RankedResult, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for index, candidate := range candidates {
		evidence := []search.Evidence{{Type: "target_candidate", RawScore: 1, Source: "target_resolution", Details: map[string]string{"reason": candidate.Reason}}}
		score := 1 - float64(index)*0.01
		var result search.Candidate
		if anchors := anchorsByFQN[strings.TrimSpace(candidate.FQN)]; len(anchors) > 0 {
			anchor := anchors[0]
			result = search.Candidate{Handle: knowledge.AnchorHandle(anchor.AnchorID), Type: "anchor", Path: anchor.Path, Title: anchor.Symbol, Symbol: anchor.Symbol, FQN: anchor.FQN, Kind: anchor.Kind, ChunkIndex: -1, AnchorID: anchor.AnchorID}
		} else if path := strings.TrimSpace(candidate.Path); path != "" {
			result = search.Candidate{Handle: knowledge.FileHandle(path), Type: "code", Path: path, Title: filepath.Base(path), Symbol: candidate.Symbol, FQN: candidate.FQN, Kind: candidate.Kind, ChunkIndex: -1}
		} else {
			continue
		}
		if !filters.AllowsCandidateType(result.Type) || !filters.AllowsPath(result.Path) || !filters.AllowsTestPath(result.Path) || !filters.AllowsSymbol(result.Symbol, result.FQN) {
			continue
		}
		key := result.Handle.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result.Owner = result.Handle
		result.Evidence = evidence
		out = append(out, search.RankedResult{Candidate: result, FinalScore: score})
	}
	return out
}
