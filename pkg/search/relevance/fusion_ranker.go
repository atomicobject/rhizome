package relevance

import (
	"context"
	"strconv"
	"strings"

	"github.com/atomicobject/rhizome/pkg/search"
)

type FusionRanker struct {
	Base search.Ranker
	K    int
}

func (r *FusionRanker) ApproxChannelWeights(spec search.QuerySpec) map[search.EvidenceChannel]float64 {
	if provider, ok := r.Base.(search.ApproxScoreProvider); ok {
		return provider.ApproxChannelWeights(spec)
	}
	return search.ApproxChannelWeightsForIntent(spec.Intent)
}

func (r *FusionRanker) ApproxMaxPerOwner(spec search.QuerySpec) int {
	if provider, ok := r.Base.(search.ApproxScoreProvider); ok {
		return provider.ApproxMaxPerOwner(spec)
	}
	return 1
}

// Rank converts per-retriever local ranks into a small reciprocal-rank-fusion
// signal, then delegates final scoring to the base ranker.
//
// Fusion is additive evidence, not a replacement ranker: it rewards agreement
// across retrievers while still letting intent weights, specificity, graph
// scores, and owner diversity make the final ordering decision.
func (r *FusionRanker) Rank(ctx context.Context, spec search.QuerySpec, candidates []search.Candidate) ([]search.RankedResult, error) {
	if r.Base == nil {
		return nil, nil
	}
	if len(candidates) == 0 {
		return r.Base.Rank(ctx, spec, candidates)
	}
	k := r.K
	if k <= 0 {
		k = 60
	}
	enriched := make([]search.Candidate, len(candidates))
	copy(enriched, candidates)
	for i, candidate := range enriched {
		score := fusionScore(candidate.Evidence, k)
		if score <= 0 {
			continue
		}
		enriched[i].Evidence = append(enriched[i].Evidence, search.MustNormalizeEvidence(search.Evidence{
			Type:     "rank_fusion",
			RawScore: score,
			Source:   "fusion",
		}))
	}
	return r.Base.Rank(ctx, spec, enriched)
}

func fusionScore(evidence []search.Evidence, k int) float64 {
	total := 0.0
	for _, ev := range evidence {
		if !strings.HasPrefix(ev.Type, "retriever_rank:") {
			continue
		}
		if ranks := strings.TrimSpace(ev.Details["lane_ranks"]); ranks != "" {
			for _, item := range strings.Split(ranks, ",") {
				rankText := strings.TrimSpace(item)
				if _, after, ok := strings.Cut(rankText, "="); ok {
					rankText = after
				}
				rank, err := strconv.Atoi(rankText)
				if err == nil && rank >= 0 {
					total += 1.0 / float64(k+rank+1)
				}
			}
			continue
		}
		rankText := ""
		if ev.Details != nil {
			rankText = ev.Details["rank"]
		}
		rank, err := strconv.Atoi(rankText)
		if err != nil || rank < 0 {
			continue
		}
		total += 1.0 / float64(k+rank+1)
	}
	return total
}
