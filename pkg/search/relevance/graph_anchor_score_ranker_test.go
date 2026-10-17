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

func TestGraphAnchorAdjuster_InsertsEvidence(t *testing.T) {
	store := &stubAnchorStore{scores: map[string]float64{"a1": 0.9}}

	r := GraphAnchorAdjuster{Store: store}
	cands := []search.Candidate{{SourceRef: search.SourceRef{AnchorID: "a1"}}}

	res, err := rankGraphEvidence(context.Background(), search.QuerySpec{}, cands, &r)
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

func rankGraphEvidence(ctx context.Context, spec search.QuerySpec, candidates []search.Candidate, adjuster search.CandidateAdjuster) ([]search.RankedResult, error) {
	enriched, err := adjuster.Adjust(ctx, spec, candidates)
	if err != nil {
		return nil, err
	}
	return (&search.WeightedRanker{}).Rank(ctx, spec, enriched)
}
