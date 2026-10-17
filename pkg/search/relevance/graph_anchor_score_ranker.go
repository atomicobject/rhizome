// graph_anchor_score_ranker.go decorates a base Ranker with anchor PageRank evidence.
// Anchors with higher PageRank (more inbound references) receive a boost.
//
// Docs: [Search (Hub)](docs/hubs/Search (Hub).md), [Code anchors (Hub)](docs/hubs/Code anchors (Hub).md)
package relevance

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/search"
)

type anchorScoreStore interface {
	AnchorScoresByIDs(ctx context.Context, anchorIDs []string) (map[string]float64, error)
}

// GraphAnchorAdjuster injects anchor PageRank evidence, then delegates to Base.
type GraphAnchorAdjuster struct {
	Store anchorScoreStore
}

func (r *GraphAnchorAdjuster) Adjust(ctx context.Context, spec search.QuerySpec, candidates []search.Candidate) ([]search.Candidate, error) {
	if r.Store == nil || len(candidates) == 0 {
		return candidates, nil
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
		return candidates, nil
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
	return aug, nil
}
