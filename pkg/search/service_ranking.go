package search

import (
	"context"
	"errors"
	"time"
)

// rankCandidates enriches source evidence and preserves the pure scorer on deadline fallback.
func (s *Service) rankCandidates(ctx context.Context, spec QuerySpec, candidates []Candidate) ([]RankedResult, error) {
	timings := TimingsFromContext(ctx)
	for _, enricher := range s.Enrichers {
		if ctx.Err() != nil {
			break
		}
		enriched, err := enricher.Adjust(ctx, spec, candidates)
		if err != nil {
			recordSearchFallback(ctx, "rank", err)
			AddRuntimeWarning(ctx, Warning{Code: "ranking_degraded", Kind: "ranking_error", Source: "ranking", Message: "Optional ranking evidence was unavailable; keeping source-owned evidence."})
			continue
		}
		candidates = enriched
	}

	var results []RankedResult
	scorer := s.scorer()
	if ctx.Err() != nil {
		recordSearchFallback(ctx, "rank", ctx.Err())
		candidates = classifyCandidates(candidates)
		results, _ = scorer.Rank(ctx, spec, candidates)
	} else {
		candidates = classifyCandidates(candidates)
		startRank := time.Now()
		ranked, rankErr := s.Ranker.Rank(ctx, spec, candidates)
		if rankErr != nil && (errors.Is(rankErr, context.DeadlineExceeded) || errors.Is(rankErr, context.Canceled)) {
			results, _ = scorer.Rank(ctx, spec, candidates)
			recordSearchFallback(ctx, "rank", rankErr)
			if timings != nil {
				timings.Add(TimingEvent{
					Name:     "rank",
					Kind:     "stage",
					Started:  startRank,
					Duration: time.Since(startRank),
					Status:   "timeout",
					Err:      TimingError(rankErr),
				})
				timings.Add(TimingEvent{
					Name:     "rank_fallback",
					Kind:     "stage",
					Started:  time.Now(),
					Duration: 0,
					Status:   "partial",
				})
			}
		} else if rankErr != nil {
			return nil, rankErr
		} else {
			results = ranked
			if timings != nil {
				timings.Add(TimingEvent{
					Name:     "rank",
					Kind:     "stage",
					Started:  startRank,
					Duration: time.Since(startRank),
					Status:   "ok",
				})
			}
		}
	}

	return results, nil
}
