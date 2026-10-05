package search

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeEvidence_MapsKnownTypes(t *testing.T) {
	ev, err := NormalizeEvidence(Evidence{Type: "definition_anchor", RawScore: 1.1})
	require.NoError(t, err)
	require.Equal(t, EvidenceChannelRefs, ev.Channel)
	require.InDelta(t, 1.0, ev.Score, 0.001)
}

func TestNormalizeEvidenceScoresExactImplementationProof(t *testing.T) {
	ev, err := NormalizeEvidence(Evidence{Type: "implements_edge", RawScore: 1})
	require.NoError(t, err)
	require.Equal(t, EvidenceChannelRefs, ev.Channel)
	require.Equal(t, 1.0, ev.Score)
}

func TestNormalizeEvidence_MapsRationaleFTSAsLexical(t *testing.T) {
	ev, err := NormalizeEvidence(Evidence{Type: "rationale_fts_match", RawScore: 0.7})
	require.NoError(t, err)
	require.Equal(t, EvidenceChannelLexical, ev.Channel)
	require.InDelta(t, 0.7, ev.Score, 0.001)
}

func TestNormalizeEvidence_MapsRetrieverRankAsNonCompetingMetadata(t *testing.T) {
	ev, err := NormalizeEvidence(Evidence{Type: "retriever_rank:vector", Score: 1})
	require.NoError(t, err)
	require.Equal(t, EvidenceChannelNone, ev.Channel)
	require.Equal(t, 0.0, ev.Score)
	require.Equal(t, 0.0, EvidenceScore(ev))

	scores := AggregateEvidenceScoresForRanking([]Evidence{ev})
	require.Empty(t, scores)
}

func TestNormalizeEvidence_FailsUnknownTypes(t *testing.T) {
	_, err := NormalizeEvidence(Evidence{Type: "totally_unknown_signal", RawScore: 0.5})
	require.Error(t, err)
}

func TestAggregateEvidenceScoresForRanking_CorroboratesWithinChannel(t *testing.T) {
	scores := AggregateEvidenceScoresForRanking([]Evidence{
		{Type: "intel_fts_match", RawScore: 0.40},
		{Type: "note_title_match", RawScore: 0.35},
	})
	require.Greater(t, scores[EvidenceChannelLexical], 0.40)
	require.LessOrEqual(t, scores[EvidenceChannelLexical], 1.0)
}

func TestNormalizeEvidence_MapsInjectedGraphSignals(t *testing.T) {
	tests := []string{
		"graph_hits_authority",
		"graph_hits_hub",
		"graph_edge_confidence",
		"graph_anchor_pagerank",
		"same_community",
	}
	for _, typ := range tests {
		ev, err := NormalizeEvidence(Evidence{Type: typ, RawScore: 0.7})
		require.NoError(t, err, typ)
		require.Equal(t, EvidenceChannelGraph, ev.Channel, typ)
	}
}

func TestApproxChannelWeightsForIntent_DiffersByIntent(t *testing.T) {
	goToDef := ApproxChannelWeightsForIntent(IntentGoToDef)
	overview := ApproxChannelWeightsForIntent(IntentOverview)
	require.Greater(t, goToDef[EvidenceChannelRefs], overview[EvidenceChannelRefs])
	require.Greater(t, overview[EvidenceChannelSemantic], goToDef[EvidenceChannelSemantic])
}
