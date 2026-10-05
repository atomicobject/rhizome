package semantic

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

type countedSectionSearchStore struct {
	*semdb.Store
	reads map[string]int
}

func (s countedSectionSearchStore) IntelDocSectionIDsByPath(ctx context.Context, path string) ([]string, error) {
	s.reads[path]++
	return s.Store.IntelDocSectionIDsByPath(ctx, path)
}

func TestSearcherSectionHandlesFollowSourceOrder(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	path := "notes/ordered.md"
	sections := []codeanchor.IntelDocSection{
		{SectionID: "later", Path: path, Title: "Later", Level: 1, StartByte: 20, EndByte: 30, Content: "later body", Fingerprint: "later"},
		{SectionID: "tie-z", Path: path, Title: "Tie Z", Level: 1, StartByte: 10, EndByte: 20, Content: "z body", Fingerprint: "z"},
		{SectionID: "first", Path: path, Title: "First", Level: 1, StartByte: 0, EndByte: 10, Content: "first body", Fingerprint: "first"},
		{SectionID: "tie-a", Path: path, Title: "Tie A", Level: 1, StartByte: 10, EndByte: 20, Content: "a body", Fingerprint: "a"},
	}
	require.NoError(t, store.ReplaceIntelDocSections(ctx, path, sections, nil, nil))
	chunks := []codeanchor.IntelChunk{
		{ChunkID: "later-chunk", OwnerID: "later", OwnerType: "doc_section", Ord: 0, Granularity: "section", Heading: "Later", ContentHash: "later"},
		{ChunkID: "a-chunk", OwnerID: "tie-a", OwnerType: "doc_section", Ord: 7, Granularity: "section", Heading: "Tie A", ContentHash: "a"},
		{ChunkID: "z-chunk", OwnerID: "tie-z", OwnerType: "doc_section", Ord: 8, Granularity: "section", Heading: "Tie Z", ContentHash: "z"},
	}
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"later", "tie-a", "tie-z"}, chunks))
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{
		"later-chunk": {1, 0, 0, 0}, "a-chunk": {1, 1, 0, 0}, "z-chunk": {1, 2, 0, 0},
	}))
	counted := countedSectionSearchStore{Store: store, reads: make(map[string]int)}
	searcher := Searcher{IntelStore: counted, NoteProvider: fixedEmbeddingProvider{"query": {1, 0, 0, 0}}}
	results, err := searcher.Search(ctx, SearchRequest{QueryText: "query", Filters: SearchFilters{Types: []string{"note"}}, K: 3})
	require.NoError(t, err)
	require.Len(t, results, 3)
	for i, ordinal := range []int{3, 1, 2} {
		require.Equal(t, ordinal, results[i].ChunkIndex)
		require.Equal(t, knowledge.NoteChunkHandle(path, ordinal).String(), results[i].Handle)
		require.Equal(t, path, results[i].Path)
	}
	require.Equal(t, 1, counted.reads[path], "multiple hits in a note share one ordered section read")
	// Cache lifetime is one search request, so a later search reads again.
	_, err = searcher.Search(ctx, SearchRequest{QueryText: "query", K: 1})
	require.NoError(t, err)
	require.Equal(t, 2, counted.reads[path])
}

type retiredSectionSearchStore struct{ *semdb.Store }

func (s retiredSectionSearchStore) SearchEmbeddings(ctx context.Context, query embeddings.Embedding, limit int, filters semdb.EmbeddingSearchFilters) ([]semdb.ScoredChunk, int, error) {
	found, total, err := s.Store.SearchEmbeddings(ctx, query, limit, filters)
	if err != nil {
		return nil, 0, err
	}
	if len(found) > 0 && found[0].OwnerType == "doc_section" {
		// Reproduce a section retired after retrieval, before handle hydration.
		if err := s.ReplaceIntelDocSections(ctx, found[0].Path, nil, nil, nil); err != nil {
			return nil, 0, err
		}
	}
	return found, total, nil
}

func TestSearcherMissingSectionKeepsNoteLevelFallback(t *testing.T) {
	ctx := context.Background()
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store := setupIntelStore(t, provider)
	searcher := Searcher{IntelStore: retiredSectionSearchStore{store}, NoteProvider: provider}
	results, err := searcher.Search(ctx, SearchRequest{QueryText: "overview documentation", Filters: SearchFilters{Types: []string{"note"}}, K: 1})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, -1, results[0].ChunkIndex)
	require.Equal(t, knowledge.NoteHandle("docs/README.md").String(), results[0].Handle)
}

func BenchmarkResultsFromScoredChunksSectionHydration(b *testing.B) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(b.TempDir(), "intel.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	const notes, sections, bytesPerSection = 25, 64, 1024
	scored := make([]semdb.ScoredChunk, notes)
	for i := range notes {
		path := fmt.Sprintf("notes/%02d.md", i)
		parts := make([]codeanchor.IntelDocSection, sections)
		for j := range sections {
			id := fmt.Sprintf("section-%02d-%02d", i, j)
			parts[j] = codeanchor.IntelDocSection{
				SectionID: id, Path: path, Title: id, Level: 1,
				StartByte: int64(j * bytesPerSection), EndByte: int64((j + 1) * bytesPerSection),
				Content: strings.Repeat("x", bytesPerSection), Fingerprint: id,
			}
		}
		if err := store.ReplaceIntelDocSections(ctx, path, parts, nil, nil); err != nil {
			b.Fatal(err)
		}
		scored[i] = semdb.ScoredChunk{ChunkID: parts[63].SectionID, OwnerID: parts[63].SectionID, OwnerType: "doc_section", Path: path, Score: 1}
	}
	searcher := Searcher{IntelStore: store}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		results, err := searcher.resultsFromScoredChunks(ctx, scored)
		if err != nil {
			b.Fatal(err)
		}
		if len(results) != notes || results[0].ChunkIndex != 63 {
			b.Fatalf("unexpected hydration: %d results", len(results))
		}
	}
}
