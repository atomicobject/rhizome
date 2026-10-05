package relevance

import (
	"context"
	"sort"

	"github.com/atomicobject/rhizome/pkg/search"
)

type LocalityRanker struct {
	Base search.Ranker
}

func (r *LocalityRanker) Rank(ctx context.Context, spec search.QuerySpec, candidates []search.Candidate) ([]search.RankedResult, error) {
	if r.Base == nil {
		return nil, nil
	}
	results, err := r.Base.Rank(ctx, spec, candidates)
	if err != nil || spec.Intent != search.IntentSubsystemOverview || !spec.HasExplicitSeeds {
		return results, err
	}
	for i := range results {
		results[i].FinalScore = adjustLocalityScore(spec, results[i])
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].FinalScore != results[j].FinalScore {
			return results[i].FinalScore > results[j].FinalScore
		}
		return results[i].Handle.String() < results[j].Handle.String()
	})
	return results, nil
}

func (r *LocalityRanker) ApproxChannelWeights(spec search.QuerySpec) map[search.EvidenceChannel]float64 {
	if provider, ok := r.Base.(search.ApproxScoreProvider); ok {
		return provider.ApproxChannelWeights(spec)
	}
	return search.ApproxChannelWeightsForIntent(spec.Intent)
}

func (r *LocalityRanker) ApproxMaxPerOwner(spec search.QuerySpec) int {
	if provider, ok := r.Base.(search.ApproxScoreProvider); ok {
		return provider.ApproxMaxPerOwner(spec)
	}
	return 1
}

func adjustLocalityScore(spec search.QuerySpec, rr search.RankedResult) float64 {
	score := rr.FinalScore
	if score <= 0 {
		score = 0.001
	}
	proximity := search.SeedPathProximity(rr.Path, spec.ExplicitSeedPaths)
	switch rr.DocClass {
	case search.DocClassModule:
		if proximity > 0 {
			score *= 1.24 + 0.08*float64(proximity)
		}
		if rr.PrimaryDoc {
			score *= 1.12
		}
	case search.DocClassHub, search.DocClassReference:
		if proximity > 0 {
			score *= 1.06
		}
	case search.DocClassRepoGlobal, search.DocClassGenerated:
		if proximity == 0 && !hasDirectRefEvidence(rr.Candidate) {
			score *= 0.52
		}
	}
	if rr.Type == "code" && !search.IsTestPath(rr.Path) {
		if proximity > 0 {
			score *= 1.10 + 0.06*float64(proximity)
		}
		if search.IsEntryPointFile(rr.Path) {
			score *= 1.08
		}
	}
	return score
}

func hasDirectRefEvidence(c search.Candidate) bool {
	for _, ev := range c.Evidence {
		switch ev.Type {
		case "doc_link", "code_ref", "code_anchor", "definition_anchor", "module_doc", "submodule_doc":
			return true
		}
	}
	return false
}
