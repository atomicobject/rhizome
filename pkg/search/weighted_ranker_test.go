package search

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestScoreCandidate_PrefersOntologyStructuralThenAmbientThenGenericGraph(t *testing.T) {
	weights := Weights{Graph: 0.6, OntologyStructural: 1.0, OntologyAmbient: 0.8}

	structural := ScoreCandidate(Candidate{
		Evidence: []Evidence{{Type: "ontology_relation_structural", RawScore: 1.25}},
	}, weights)
	ambient := ScoreCandidate(Candidate{
		Evidence: []Evidence{{Type: "ontology_relation_ambient", RawScore: 1.05}},
	}, weights)
	generic := ScoreCandidate(Candidate{
		Evidence: []Evidence{{Type: "graph_proximity", RawScore: 1.0}},
	}, weights)

	require.Greater(t, structural, ambient)
	require.Greater(t, ambient, generic)
}

func TestWeightedRankerKeepsPrecisionEvidenceAheadOfStrongerSupportingScore(t *testing.T) {
	ranker := WeightedRanker{Weights: Weights{Semantic: 1, Refs: 0.1}, MaxPerOwner: 2}
	results, err := ranker.Rank(context.Background(), QuerySpec{Intent: IntentGoToDef, Limits: Limits{Total: 2}}, []Candidate{
		{SourceRef: SourceRef{Path: "definition.go"}, Handle: knowledge.AnchorHandle("definition"), Owner: knowledge.AnchorHandle("definition"), Evidence: []Evidence{{Type: "definition_anchor", RawScore: 1}}},
		{SourceRef: SourceRef{Path: "distractor.md"}, Handle: knowledge.NoteHandle("distractor.md"), Owner: knowledge.NoteHandle("distractor.md"), Evidence: []Evidence{{Type: "note_vector_similarity", RawScore: 1}}},
	})
	require.NoError(t, err)
	require.Equal(t, "definition.go", results[0].Path)
}

func TestScoreCandidate_IncludesGraphEdgeConfidence(t *testing.T) {
	weights := Weights{Graph: 1}

	score := ScoreCandidate(Candidate{
		Evidence: []Evidence{{Type: "graph_edge_confidence", RawScore: 0.72}},
	}, weights)

	require.Equal(t, 0.72, score)
}

func TestScoreCandidate_CorroboratesLexicalEvidenceWithinChannel(t *testing.T) {
	weights := Weights{Lexical: 1}

	score := ScoreCandidate(Candidate{
		Evidence: []Evidence{
			{Type: "intel_fts_match", RawScore: 0.4},
			{Type: "note_title_match", RawScore: 0.35},
		},
	}, weights)

	require.Greater(t, score, 0.4)
	require.LessOrEqual(t, score, 1.0)
}
