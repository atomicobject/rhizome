// rerank_ranker.go decorates a base Ranker with an optional cross-encoder
// reranking pass over the head of the ranked list. The stage is additive: any
// error, deadline, or short result list leaves the base order untouched.
package relevance

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/rerank"
)

const (
	defaultRerankTopN   = 30
	defaultRerankWeight = 0.3
	// rerankDocumentLimit caps the text sent per result; reranker inputs are
	// built from data already on the candidate, never from file reads.
	rerankDocumentLimit = 1200
)

// RerankingRanker blends cross-encoder scores into the top of the base ranking.
type RerankingRanker struct {
	Base     search.Ranker
	Reranker rerank.Reranker
	Model    string  // recorded on rerank evidence
	TopN     int     // results reranked, default 30
	Weight   float64 // blend weight for the rerank score, default 0.5
}

func (r *RerankingRanker) Rank(ctx context.Context, spec search.QuerySpec, candidates []search.Candidate) ([]search.RankedResult, error) {
	if r.Base == nil {
		return nil, errors.New("missing base ranker")
	}
	results, err := r.Base.Rank(ctx, spec, candidates)
	if err != nil || r.Reranker == nil || len(results) < 2 {
		return results, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	n := r.topN()
	if n > len(results) {
		n = len(results)
	}
	if n < 2 {
		return results, nil
	}

	// Results the caller named exactly (path, title, symbol, declaration, or an
	// explicit seed) keep their base positions: a cross-encoder reading a
	// truncated snippet must not outvote identity evidence.
	movable := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if !hasIdentityEvidence(results[i].Evidence) {
			movable = append(movable, i)
		}
	}
	if len(movable) < 2 {
		return results, nil
	}
	docs := make([]rerank.Document, len(movable))
	maxBase := 0.0
	for k, i := range movable {
		docs[k] = rerank.Document{ID: results[i].Handle.String(), Text: rerankDocumentText(results[i].Candidate)}
		if results[i].FinalScore > maxBase {
			maxBase = results[i].FinalScore
		}
	}
	scores, rerankErr := r.Reranker.Rerank(ctx, spec.Text, docs)
	if rerankErr != nil || len(scores) != len(movable) {
		search.AddRuntimeWarning(ctx, search.Warning{
			Code:    "rerank_unavailable",
			Kind:    "degraded",
			Source:  "rerank",
			Message: "reranking failed; keeping base ranking order",
		})
		return results, nil
	}

	// Copy the movable results before mutating so equal blended scores keep
	// base order, then write them back into the same slots.
	top := make([]search.RankedResult, len(movable))
	weight := r.weight()
	for k, i := range movable {
		top[k] = results[i]
		top[k].FinalScore = (1-weight)*top[k].FinalScore + weight*scores[k]*maxBase
		evidence := top[k].Evidence
		top[k].Evidence = append(evidence[:len(evidence):len(evidence)], search.Evidence{
			Type:     "rerank_score",
			RawScore: scores[k],
			Source:   "rerank",
			Details:  map[string]string{"model": r.Model},
		})
	}
	sort.SliceStable(top, func(i, j int) bool { return top[i].FinalScore > top[j].FinalScore })
	for k, i := range movable {
		results[i] = top[k]
	}
	return results, nil
}

// hasIdentityEvidence reports whether a result carries evidence that the caller
// named it exactly.
func hasIdentityEvidence(evidence []search.Evidence) bool {
	for _, ev := range evidence {
		switch ev.Type {
		case "path_exact", "note_title_exact", "symbol_exact", "definition_anchor", "explicit_seed":
			return true
		}
	}
	return false
}

func (r *RerankingRanker) ApproxChannelWeights(spec search.QuerySpec) map[search.EvidenceChannel]float64 {
	if provider, ok := r.Base.(search.ApproxScoreProvider); ok {
		return provider.ApproxChannelWeights(spec)
	}
	return search.ApproxChannelWeightsForIntent(spec.Intent)
}

func (r *RerankingRanker) ApproxMaxPerOwner(spec search.QuerySpec) int {
	if provider, ok := r.Base.(search.ApproxScoreProvider); ok {
		return provider.ApproxMaxPerOwner(spec)
	}
	return 1
}

func (r *RerankingRanker) topN() int {
	if r.TopN > 0 {
		return r.TopN
	}
	return defaultRerankTopN
}

func (r *RerankingRanker) weight() float64 {
	if r.Weight > 0 && r.Weight <= 1 {
		return r.Weight
	}
	return defaultRerankWeight
}

// rerankDocumentText renders what the retrieval stage already knows about a
// result: identity, location, and its best available snippet.
func rerankDocumentText(c search.Candidate) string {
	symbol := strings.TrimSpace(c.FQN)
	if symbol == "" {
		symbol = strings.TrimSpace(c.Symbol)
	}
	if symbol != "" && strings.TrimSpace(c.Kind) != "" {
		symbol = strings.TrimSpace(c.Kind) + " " + symbol
	}
	var parts []string
	for _, part := range []string{
		strings.TrimSpace(c.Title),
		strings.TrimSpace(c.Path),
		symbol,
		strings.TrimSpace(c.Heading),
		strings.TrimSpace(c.Breadcrumb),
		bestEvidenceSnippet(c.Evidence),
	} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	text := strings.Join(parts, "\n")
	if runes := []rune(text); len(runes) > rerankDocumentLimit {
		text = string(runes[:rerankDocumentLimit])
	}
	return text
}

// bestEvidenceSnippet returns the snippet from the strongest evidence that has one.
func bestEvidenceSnippet(evidence []search.Evidence) string {
	best := ""
	bestScore := 0.0
	for _, ev := range evidence {
		if ev.Details == nil {
			continue
		}
		snippet := strings.TrimSpace(ev.Details["snippet"])
		if snippet == "" {
			continue
		}
		if best == "" || ev.RawScore > bestScore {
			best = snippet
			bestScore = ev.RawScore
		}
	}
	return best
}
