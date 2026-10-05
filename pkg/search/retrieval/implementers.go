package retrieval

import (
	"context"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
)

// ImplementersRetriever returns only implementers backed by a current exact
// package type proof. Invalidated or incomplete derived state is hidden by the
// store query.
type ImplementersRetriever struct {
	Store *semdb.Store
	Limit int
}

func (r *ImplementersRetriever) Name() string { return "implementers" }

func (r *ImplementersRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	if r.Store == nil {
		return nil, nil
	}
	if !spec.Filters.AllowsCandidateType("code") {
		return nil, nil
	}
	limit := r.Limit
	if limit <= 0 {
		limit = 20
	}
	var targetIDs []string
	for _, seed := range spec.Seeds {
		if seed.Kind == knowledge.KindAnchor && strings.TrimSpace(seed.ID) != "" {
			targetIDs = append(targetIDs, seed.ID)
		}
	}
	if spec.ResolvedTarget != nil && strings.TrimSpace(spec.ResolvedTarget.FQN) != "" {
		ids, err := r.Store.IntelAnchorIDsByFQN(ctx, spec.ResolvedTarget.FQN, 4)
		if err != nil {
			return nil, err
		}
		targetIDs = append(targetIDs, ids...)
	}
	targetIDs = dedupeStrings(targetIDs)
	if len(targetIDs) == 0 {
		return nil, nil
	}
	found, err := r.Store.ImplementerAnchorsByTargetIDsFiltered(ctx, targetIDs, spec.Filters.PathPrefixes, limit)
	if err != nil {
		return nil, err
	}
	out := make([]search.Candidate, 0, limit)
	seen := make(map[string]struct{}, limit)
	for round := 0; len(out) < limit; round++ {
		hasEntries := false
		for _, targetID := range targetIDs {
			anchors := found[targetID]
			if round >= len(anchors) {
				continue
			}
			hasEntries = true
			anchor := anchors[round]
			if !spec.Filters.AllowsPath(anchor.Path) {
				continue
			}
			handle := knowledge.AnchorHandle(anchor.AnchorID)
			if _, duplicate := seen[handle.String()]; duplicate {
				continue
			}
			seen[handle.String()] = struct{}{}
			out = append(out, search.Candidate{
				Handle: handle, Owner: handle, Type: "anchor", Path: anchor.Path,
				Title: anchor.Symbol, Symbol: anchor.Symbol, FQN: anchor.FQN,
				Kind: anchor.Kind, AnchorID: anchor.AnchorID, ChunkIndex: -1,
				Evidence: []search.Evidence{{
					Type: "implements_edge", Source: "implementers", RawScore: 1,
					Details: map[string]string{"target": targetID},
				}},
			})
			if len(out) >= limit {
				break
			}
		}
		if !hasEntries {
			break
		}
	}
	return out, nil
}
