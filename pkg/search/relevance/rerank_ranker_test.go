package relevance

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/rerank"
	"github.com/stretchr/testify/require"
)

type stubBaseRanker struct{ results []search.RankedResult }

func (s *stubBaseRanker) Rank(context.Context, search.QuerySpec, []search.Candidate) ([]search.RankedResult, error) {
	out := make([]search.RankedResult, len(s.results))
	copy(out, s.results)
	return out, nil
}

type stubReranker struct {
	scores []float64
	err    error
	seen   []rerank.Document
	calls  int
}

func (s *stubReranker) Rerank(_ context.Context, _ string, docs []rerank.Document) ([]float64, error) {
	s.calls++
	s.seen = docs
	if s.err != nil {
		return nil, s.err
	}
	return s.scores, nil
}

func rerankFixture() []search.RankedResult {
	result := func(path string, score float64) search.RankedResult {
		return search.RankedResult{
			Candidate: search.Candidate{
				Handle: knowledge.NoteHandle(path),
				Owner:  knowledge.NoteHandle(path),
				Path:   path,
				Title:  strings.TrimSuffix(path, ".md"),
				Evidence: []search.Evidence{
					{Type: "intel_fts_match", RawScore: 0.2, Details: map[string]string{"snippet": "weak " + path}},
					{Type: "intel_doc_match", RawScore: 0.9, Details: map[string]string{"snippet": "best " + path}},
				},
			},
			FinalScore: score,
		}
	}
	return []search.RankedResult{result("a.md", 1.0), result("b.md", 0.9), result("c.md", 0.8), result("d.md", 0.7)}
}

func TestRerankingRankerBlendsTopNAndLeavesTheTailInPlace(t *testing.T) {
	reranker := &stubReranker{scores: []float64{0, 1, 0}}
	fixture := rerankFixture()
	fixture[0].Evidence[1].Details["snippet"] = "best a.md " + strings.Repeat("😀", 1600)
	r := &RerankingRanker{Base: &stubBaseRanker{results: fixture}, Reranker: reranker, Model: "rerank-2.5-lite", TopN: 3, Weight: 0.5}

	results, err := r.Rank(context.Background(), search.QuerySpec{Text: "ranking"}, nil)
	require.NoError(t, err)
	require.Len(t, results, 4)

	// maxBase is 1.0, so blended = 0.5*base + 0.5*rerank.
	require.Equal(t, []string{"b.md", "a.md", "c.md", "d.md"}, []string{results[0].Path, results[1].Path, results[2].Path, results[3].Path})
	require.InDelta(t, 0.95, results[0].FinalScore, 1e-9)
	require.InDelta(t, 0.5, results[1].FinalScore, 1e-9)
	require.InDelta(t, 0.4, results[2].FinalScore, 1e-9)

	// The tail keeps its base score and gains no rerank evidence.
	require.InDelta(t, 0.7, results[3].FinalScore, 1e-9)
	require.False(t, hasEvidence(results[3].Candidate, "rerank_score"))

	require.True(t, hasEvidence(results[0].Candidate, "rerank_score"))
	for _, ev := range results[0].Evidence {
		if ev.Type == "rerank_score" {
			require.Equal(t, 1.0, ev.RawScore)
			require.Equal(t, "rerank", ev.Source)
			require.Equal(t, "rerank-2.5-lite", ev.Details["model"])
		}
	}

	// Documents are sent in base order and carry identity plus the strongest snippet.
	require.Len(t, reranker.seen, 3)
	require.Equal(t,
		[]string{knowledge.NoteHandle("a.md").String(), knowledge.NoteHandle("b.md").String(), knowledge.NoteHandle("c.md").String()},
		[]string{reranker.seen[0].ID, reranker.seen[1].ID, reranker.seen[2].ID})
	require.Contains(t, reranker.seen[0].Text, "a.md")
	require.Contains(t, reranker.seen[0].Text, "best a.md")
	require.NotContains(t, reranker.seen[0].Text, "weak a.md")
	require.True(t, utf8.ValidString(reranker.seen[0].Text))
	require.LessOrEqual(t, len([]rune(reranker.seen[0].Text)), 1200)
	require.Contains(t, reranker.seen[1].Text, "b.md")
}

func TestRerankingRankerKeepsBaseOrderOnError(t *testing.T) {
	reranker := &stubReranker{err: errors.New("boom")}
	r := &RerankingRanker{Base: &stubBaseRanker{results: rerankFixture()}, Reranker: reranker, TopN: 3}

	results, err := r.Rank(context.Background(), search.QuerySpec{Text: "ranking"}, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"a.md", "b.md", "c.md", "d.md"}, []string{results[0].Path, results[1].Path, results[2].Path, results[3].Path})
	require.Equal(t, rerankFixture(), results)
	for _, result := range results {
		require.False(t, hasEvidence(result.Candidate, "rerank_score"))
	}
	response, err := (&search.Service{Retrievers: []search.Retriever{auditRetriever{}}, Ranker: r}).Search(context.Background(), search.QuerySpec{Text: "ranking"})
	require.NoError(t, err)
	require.Equal(t, rerankFixture(), response.Results)
	require.Contains(t, response.Warnings, search.Warning{Code: "rerank_unavailable", Kind: "degraded", Source: "rerank", Message: "reranking failed; keeping base ranking order"})
}

type auditRetriever struct{}

func (auditRetriever) Name() string { return "audit" }
func (auditRetriever) Retrieve(context.Context, search.QuerySpec) ([]search.Candidate, error) {
	return nil, nil
}

func TestRerankingRankerKeepsBaseOrderOnScoreCountMismatch(t *testing.T) {
	reranker := &stubReranker{scores: []float64{0.9}}
	r := &RerankingRanker{Base: &stubBaseRanker{results: rerankFixture()}, Reranker: reranker, TopN: 3}

	results, err := r.Rank(context.Background(), search.QuerySpec{Text: "ranking"}, nil)
	require.NoError(t, err)
	require.Equal(t, rerankFixture(), results)
	for _, result := range results {
		require.False(t, hasEvidence(result.Candidate, "rerank_score"))
	}
	response, err := (&search.Service{Retrievers: []search.Retriever{auditRetriever{}}, Ranker: r}).Search(context.Background(), search.QuerySpec{Text: "ranking"})
	require.NoError(t, err)
	require.Equal(t, rerankFixture(), response.Results)
	require.Contains(t, response.Warnings, search.Warning{Code: "rerank_unavailable", Kind: "degraded", Source: "rerank", Message: "reranking failed; keeping base ranking order"})
}

func TestRerankingRankerSkipsSingleResult(t *testing.T) {
	reranker := &stubReranker{scores: []float64{1}}
	r := &RerankingRanker{Base: &stubBaseRanker{results: rerankFixture()[:1]}, Reranker: reranker}

	results, err := r.Rank(context.Background(), search.QuerySpec{Text: "ranking"}, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Zero(t, reranker.calls)
}

func TestRerankingRankerRequiresBase(t *testing.T) {
	r := &RerankingRanker{}
	_, err := r.Rank(context.Background(), search.QuerySpec{}, nil)
	require.Error(t, err)
}
