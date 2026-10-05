package relevance

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/stretchr/testify/require"
)

type stubAnchorStore struct {
	scores map[string]float64
	err    error
}

func (s *stubAnchorStore) AnchorScoresByIDs(ctx context.Context, anchorIDs []string) (map[string]float64, error) {
	return s.scores, s.err
}

func TestGraphAnchorScoreRanker_InsertsEvidence(t *testing.T) {
	base := &WeightedRanker{}
	store := &stubAnchorStore{scores: map[string]float64{"a1": 0.9}}

	r := GraphAnchorScoreRanker{Base: base, Store: store}
	cands := []search.Candidate{{
		AnchorID: "a1",
	}}

	res, err := r.Rank(context.Background(), search.QuerySpec{}, cands)
	require.NoError(t, err)
	require.Len(t, res, 1)
	found := false
	for _, ev := range res[0].Evidence {
		if ev.Type == "graph_anchor_pagerank" {
			found = true
			require.InEpsilon(t, 0.9, ev.RawScore, 0.0001)
		}
	}
	require.True(t, found, "expected pagerank evidence")
}
