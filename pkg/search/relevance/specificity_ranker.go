package relevance

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/queryframe"
)

type SpecificityRanker struct {
	Base search.Ranker
}

func (r *SpecificityRanker) Rank(ctx context.Context, spec search.QuerySpec, candidates []search.Candidate) ([]search.RankedResult, error) {
	if r.Base == nil {
		return nil, nil
	}
	if len(candidates) == 0 {
		return r.Base.Rank(ctx, spec, candidates)
	}
	frame := queryframe.Extract(spec.Text)
	enriched := make([]search.Candidate, len(candidates))
	for i, candidate := range candidates {
		enriched[i] = queryframe.EnrichCandidate(frame, candidate)
	}
	return r.Base.Rank(ctx, spec, enriched)
}
