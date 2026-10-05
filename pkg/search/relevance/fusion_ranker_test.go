package relevance

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFusionRankerBoostsMultiRetrieverCandidates(t *testing.T) {
	base := &WeightedRanker{
		Weights: Weights{
			Fusion:   1.0,
			Lexical:  0.0,
			Semantic: 0.0,
			Graph:    0.0,
			Refs:     0.0,
		},
		MaxPerOwner: 2,
	}
	ranker := &FusionRanker{Base: base}

	results, err := ranker.Rank(context.Background(), search.QuerySpec{Limits: search.Limits{Total: 2}}, []search.Candidate{
		{
			Handle: knowledge.NoteHandle("A.md"),
			Owner:  knowledge.NoteHandle("A.md"),
			Type:   "note",
			Path:   "A.md",
			Evidence: []search.Evidence{
				{Type: "retriever_rank:vector", Details: map[string]string{"rank": "0"}},
				{Type: "retriever_rank:lexical", Details: map[string]string{"rank": "1"}},
			},
		},
		{
			Handle: knowledge.NoteHandle("B.md"),
			Owner:  knowledge.NoteHandle("B.md"),
			Type:   "note",
			Path:   "B.md",
			Evidence: []search.Evidence{
				{Type: "retriever_rank:vector", Details: map[string]string{"rank": "0"}},
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "A.md", results[0].Path)
	assert.Greater(t, results[0].FinalScore, results[1].FinalScore)
}

func TestRankerFallbackPreservesPolicy(t *testing.T) {
	base := &WeightedRanker{Weights: Weights{Lexical: 0.123}, MaxPerOwner: 3}
	ranker := &FusionRanker{Base: base}
	weights := ranker.ApproxChannelWeights(search.QuerySpec{})
	assert.InDelta(t, 0.123, weights[search.EvidenceChannelLexical], 0.0001)
	assert.Equal(t, 3, ranker.ApproxMaxPerOwner(search.QuerySpec{}))
}

func TestFusionScoreCountsEveryRetainedLaneRank(t *testing.T) {
	evidence := []search.Evidence{{
		Type:    "retriever_rank:shared",
		Details: map[string]string{"lane_ranks": "graph=2,lexical=0,vector=1"},
	}}
	want := 1.0/61.0 + 1.0/62.0 + 1.0/63.0
	require.InDelta(t, want, fusionScore(evidence, 60), 1e-12)
}
