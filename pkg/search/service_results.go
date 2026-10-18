package search

import (
	"context"
	"errors"
	"sort"
	"time"
)

// prepareResults collapses and shapes representatives before final provider reranking.
// Nothing sorts the returned results after the adjusters have pinned identities.
func (s *Service) prepareResults(ctx context.Context, spec QuerySpec, results []RankedResult) ([]RankedResult, error) {
	timings := TimingsFromContext(ctx)
	if s.Rollupper != nil && ctx.Err() == nil {
		startRollup := time.Now()
		rolled, err := s.Rollupper.Rollup(ctx, spec, results)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				rolled = results
				recordSearchFallback(ctx, "rollup", err)
				if timings != nil {
					timings.Add(TimingEvent{
						Name:     "rollup",
						Kind:     "stage",
						Started:  startRollup,
						Duration: time.Since(startRollup),
						Status:   "timeout",
						Err:      TimingError(err),
					})
				}
			} else {
				return nil, err
			}
		}
		results = rolled
		if timings != nil && err == nil {
			timings.Add(TimingEvent{
				Name:     "rollup",
				Kind:     "stage",
				Started:  startRollup,
				Duration: time.Since(startRollup),
				Status:   "ok",
			})
		}
	}
	// Rankers own the final order, including eligibility tiers such as exact
	// navigation and relationship-qualified precision results. Rollup may create
	// new representatives, so restore those evidence-backed tiers before the
	// numeric score and stable identity tie-break.
	if len(results) > 1 {
		sort.SliceStable(results, func(i, j int) bool {
			leftExact := !IsPrecisionIntent(spec.Intent) && HasEvidence(results[i].Evidence, "note_title_exact", "path_exact")
			rightExact := !IsPrecisionIntent(spec.Intent) && HasEvidence(results[j].Evidence, "note_title_exact", "path_exact")
			if leftExact != rightExact {
				return leftExact
			}
			leftPrimary := HasPrimaryEvidence(spec.Intent, results[i].Evidence)
			rightPrimary := HasPrimaryEvidence(spec.Intent, results[j].Evidence)
			if leftPrimary != rightPrimary {
				return leftPrimary
			}
			if results[i].FinalScore != results[j].FinalScore {
				return results[i].FinalScore > results[j].FinalScore
			}
			return rankedResultKey(results[i]) < rankedResultKey(results[j])
		})
	}

	if s.Shaper != nil && ctx.Err() == nil {
		startShape := time.Now()
		shaped, err := s.Shaper.Shape(ctx, spec, results)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				recordSearchFallback(ctx, "shaping", err)
				if timings != nil {
					timings.Add(TimingEvent{
						Name:     "shape",
						Kind:     "stage",
						Started:  startShape,
						Duration: time.Since(startShape),
						Status:   "timeout",
						Err:      TimingError(err),
					})
				}
			} else {
				return nil, err
			}
		} else {
			results = shaped
			if timings != nil {
				timings.Add(TimingEvent{
					Name:     "shape",
					Kind:     "stage",
					Started:  startShape,
					Duration: time.Since(startShape),
					Status:   "ok",
				})
			}
		}
	}

	for _, adjuster := range s.Adjusters {
		if ctx.Err() != nil {
			break
		}
		adjusted, err := adjuster.Adjust(ctx, spec, results)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				recordSearchFallback(ctx, "adjust", err)
				break
			}
			return nil, err
		}
		results = adjusted
	}
	return results, nil
}
