// graph_anchor_score_ranker.go decorates a base Ranker with anchor PageRank evidence.
// Anchors with higher PageRank (more inbound references) receive a boost.
//
// Docs: [Search (Hub)](docs/hubs/Search (Hub).md), [Code anchors (Hub)](docs/hubs/Code anchors (Hub).md)
package relevance

import (
	"context"
	"errors"

	"github.com/atomicobject/rhizome/pkg/search"
)

type anchorScoreStore interface {
	AnchorScoresByIDs(ctx context.Context, anchorIDs []string) (map[string]float64, error)
}

// GraphAnchorScoreRanker injects anchor PageRank evidence, then delegates to Base.
type GraphAnchorScoreRanker struct {
	Base  search.Ranker
	Store anchorScoreStore
}

func (r *GraphAnchorScoreRanker) Rank(ctx context.Context, spec search.QuerySpec, candidates []search.Candidate) ([]search.RankedResult, error) {
	if r.Base == nil {
		return nil, errors.New("missing base ranker")
	}
	if r.Store == nil || len(candidates) == 0 {
		return r.Base.Rank(ctx, spec, candidates)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var ids []string
	for _, c := range candidates {
		if c.AnchorID != "" {
			ids = append(ids, c.AnchorID)
		}
	}
	if len(ids) == 0 {
		return r.Base.Rank(ctx, spec, candidates)
	}

	scores, err := r.Store.AnchorScoresByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	aug := make([]search.Candidate, 0, len(candidates))
	for _, c := range candidates {
		if c.AnchorID != "" {
			if pr, ok := scores[c.AnchorID]; ok && pr > 0 {
				c.Evidence = append(c.Evidence, search.Evidence{
					Type:     "graph_anchor_pagerank",
					RawScore: clamp01(pr),
					Source:   "graph_anchor_scores",
				})
			}
		}
		aug = append(aug, c)
	}
	return r.Base.Rank(ctx, spec, aug)
}

func (r *GraphAnchorScoreRanker) ApproxChannelWeights(spec search.QuerySpec) map[search.EvidenceChannel]float64 {
	if provider, ok := r.Base.(search.ApproxScoreProvider); ok {
		return provider.ApproxChannelWeights(spec)
	}
	return search.ApproxChannelWeightsForIntent(spec.Intent)
}

func (r *GraphAnchorScoreRanker) ApproxMaxPerOwner(spec search.QuerySpec) int {
	if provider, ok := r.Base.(search.ApproxScoreProvider); ok {
		return provider.ApproxMaxPerOwner(spec)
	}
	return 1
}
