package search

import (
	"context"
	"slices"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestRankingEligibilityBeforeAndAfterRollup(t *testing.T) {
	candidates := []Candidate{
		{SourceRef: SourceRef{Path: "support.md"}, Handle: knowledge.NoteHandle("support.md"), Owner: knowledge.NoteHandle("support.md"), Evidence: []Evidence{{Type: "note_vector_similarity", RawScore: 1}}},
		{SourceRef: SourceRef{Path: "title.md"}, Handle: knowledge.NoteHandle("title.md"), Owner: knowledge.NoteHandle("title.md"), Evidence: []Evidence{{Type: "note_title_exact", RawScore: 0.1}}},
		{SourceRef: SourceRef{Path: "path.md"}, Handle: knowledge.NoteHandle("path.md"), Owner: knowledge.NoteHandle("path.md"), Evidence: []Evidence{{Type: "path_exact", RawScore: 0.2}}},
		{SourceRef: SourceRef{Path: "definition.go"}, Handle: knowledge.AnchorHandle("definition"), Owner: knowledge.AnchorHandle("definition"), Evidence: []Evidence{{Type: "definition_anchor", RawScore: 0.1}}},
	}
	for _, tc := range []struct {
		intent Intent
		want   []string
	}{
		{IntentSearch, []string{"path.md", "title.md", "support.md", "definition.go"}},
		{IntentGoToDef, []string{"definition.go", "support.md", "path.md", "title.md"}},
	} {
		t.Run(string(tc.intent), func(t *testing.T) {
			spec := QuerySpec{Intent: tc.intent}
			ranker := WeightedRanker{Weights: Weights{Semantic: 1, Lexical: 1, Refs: 0.1}}
			ranked, err := ranker.Rank(context.Background(), spec, candidates)
			require.NoError(t, err)
			paths := func(results []RankedResult) []string {
				var paths []string
				for _, result := range results {
					paths = append(paths, result.Path)
				}
				return paths
			}
			require.Equal(t, tc.want, paths(ranked))

			slices.Reverse(ranked)
			prepared, err := (&Service{}).prepareResults(context.Background(), spec, ranked)
			require.NoError(t, err)
			require.Equal(t, tc.want, paths(prepared))
		})
	}
}
