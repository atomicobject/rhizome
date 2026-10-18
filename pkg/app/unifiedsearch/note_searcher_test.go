package unifiedsearch

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/diag"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNoteSearcherReturnsOwningNotePathsOnly(t *testing.T) {
	var received Options
	searcher := NoteSearcher{Runtime: Options{NoteFormats: testNoteFormats(t), VaultPath: t.TempDir()}, runRuntime: func(_ context.Context, opts Options) (Result, error) {
		received = opts
		return Result{IndexGeneration: "g1", Lanes: []search.LaneStatus{{Lane: "note_lexical", Status: search.LaneStateRan}}, Results: []search.RankedResult{{Candidate: search.Candidate{SourceRef: search.SourceRef{Path: "notes/roadmap.md"}, Handle: knowledge.NoteHandle("notes/roadmap.md"), Type: "note", Evidence: []search.Evidence{{Type: "note_lexical", RawScore: .4}}}, FinalScore: .4}, {Candidate: search.Candidate{SourceRef: search.SourceRef{Path: "notes/roadmap.md#spec", NodeRef: &ontology.NodeRef{NotePath: "notes/roadmap.md", NodeID: "SPEC-1", Kind: ontology.NodeKindEmbedded}}, Handle: knowledge.NoteHandle("notes/roadmap.md#spec"), Type: "note", Evidence: []search.Evidence{{Type: "note_vector_similarity", RawScore: .9}}}, FinalScore: .9}}, Warnings: []search.Warning{{Code: "vector_unavailable", Message: "embeddings are unavailable"}, {Code: "vector_unavailable", Message: "embeddings are unavailable"}}}, nil
	}}
	response, err := searcher.SearchNotes(context.Background(), ontologyquery.NoteSearchRequest{Queries: []string{"roadmap", "  "}, NoteTypes: []string{"Project"}, First: 6})
	require.NoError(t, err)
	require.Equal(t, []ontologyquery.NoteSearchHit{{Path: "notes/roadmap.md", Score: .9}}, response.Hits)
	require.Equal(t, []diag.Warning{{Code: "vector_unavailable", Message: "embeddings are unavailable"}}, response.Warnings)
	require.Equal(t, []QueryInput{{Text: "roadmap", Mode: string(search.IntentSearch)}}, received.QueryInputs)
	require.Equal(t, []string{"note"}, received.Filters.Types)
	require.Equal(t, []string{"Project"}, received.Filters.NoteTypes)
	require.True(t, received.UseVector)
	require.True(t, received.UseIntel)
	require.True(t, received.UseRefs)
	require.False(t, received.UseGraph)
}
func TestNoteSearcherAppliesRequestedFirstAsVisibleLimit(t *testing.T) {
	searcher := NoteSearcher{Runtime: Options{NoteFormats: testNoteFormats(t), VaultPath: t.TempDir()}, runRuntime: func(context.Context, Options) (Result, error) {
		return Result{IndexGeneration: "g1", Lanes: []search.LaneStatus{{Lane: "note_lexical", Status: search.LaneStateRan}}, Results: []search.RankedResult{{Candidate: search.Candidate{SourceRef: search.SourceRef{Path: "a.md"}, Handle: knowledge.NoteHandle("a.md"), Type: "note", Evidence: []search.Evidence{{Type: "note_lexical", RawScore: .9}}}, FinalScore: .9}, {Candidate: search.Candidate{SourceRef: search.SourceRef{Path: "b.md"}, Handle: knowledge.NoteHandle("b.md"), Type: "note", Evidence: []search.Evidence{{Type: "note_lexical", RawScore: .8}}}, FinalScore: .8}}}, nil
	}}
	response, err := searcher.SearchNotes(context.Background(), ontologyquery.NoteSearchRequest{Queries: []string{"roadmap"}, First: 1})
	require.NoError(t, err)
	require.Equal(t, []ontologyquery.NoteSearchHit{{Path: "a.md", Score: .9}}, response.Hits)
}
