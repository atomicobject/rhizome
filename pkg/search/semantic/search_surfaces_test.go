package semantic

import (
	"context"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestSearcher_RespectsTypeFilters(t *testing.T) {
	ctx := context.Background()
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	intelStore := setupIntelStore(t, prov)

	searcher := Searcher{CodeProvider: prov, NoteProvider: prov, IntelStore: intelStore}
	results, err := searcher.Search(ctx, SearchRequest{QueryText: "overview documentation", Filters: SearchFilters{Types: []string{"note"}}, K: 10})
	require.NoError(t, err)
	require.NotEmpty(t, results)
	for _, r := range results {
		require.Equal(t, "note", r.Type)
	}

	results, err = searcher.Search(ctx, SearchRequest{QueryText: "helper function", Filters: SearchFilters{Types: []string{"code"}}, K: 10})
	require.NoError(t, err)
	require.NotEmpty(t, results)
	for _, r := range results {
		require.Equal(t, "code", r.Type)
	}
}

func TestSearcherWeightsSupplementalChunksBeforeResultLimit(t *testing.T) {
	ctx := context.Background()
	provider := fixedEmbeddingProvider{"query": {1, 0, 0, 0}}
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	visibleNode := readmodel.NodeRow{
		NodeID: "node-visible", NotePath: "visible.html", NodeRefJSON: `{"notePath":"visible.html","typeName":"_FallbackNote","kind":"NOTE"}`,
		NodeKind: "NOTE", TypeName: ontoschema.FallbackNoteTypeName, Title: "Visible", SourceLocator: "visible.html",
	}
	supplementalNode := readmodel.NodeRow{
		NodeID: "node-supplemental", NotePath: "data.html", NodeRefJSON: `{"notePath":"data.html","typeName":"_FallbackNote","kind":"NOTE"}`,
		NodeKind: "NOTE", TypeName: ontoschema.FallbackNoteTypeName, Title: "Data", SourceLocator: "data.html",
	}
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{"visible.html", "data.html"}, []readmodel.NodeRow{visibleNode, supplementalNode}))

	chunks := []codeanchor.IntelChunk{{
		ChunkID: "visible", OwnerID: visibleNode.NodeID, OwnerType: embeddings.OntologyNodeOwnerType, Ord: 0,
		Granularity: embeddings.GranularityOntologyNodeVisible, Heading: "Visible", ContentHash: "visible",
	}}
	vectors := map[string]embeddings.Embedding{"visible": {0.9, 0.1, 0, 0}}
	for i := 0; i < 60; i++ {
		chunkID := fmt.Sprintf("supplemental-%02d", i)
		chunks = append(chunks, codeanchor.IntelChunk{
			ChunkID: chunkID, OwnerID: supplementalNode.NodeID, OwnerType: embeddings.OntologyNodeOwnerType, Ord: i,
			Granularity: embeddings.GranularityOntologyNodeSupplemental, Heading: "Data", ContentHash: chunkID,
		})
		vectors[chunkID] = embeddings.Embedding{1, 0, 0, 0}
	}
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{visibleNode.NodeID, supplementalNode.NodeID}, chunks))
	require.NoError(t, store.UpsertEmbeddings(ctx, vectors))

	searcher := Searcher{NoteProvider: provider, IntelStore: store}
	results, err := searcher.Search(ctx, SearchRequest{QueryText: "query", Filters: SearchFilters{Types: []string{"note"}}, K: 1})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, visibleNode.NodeID, results[0].NodeID)

	results, err = searcher.Search(ctx, SearchRequest{
		QueryText: "query", Filters: SearchFilters{Types: []string{"note"}, Granularity: []string{embeddings.GranularityOntologyNodeSupplemental}}, K: 1,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, supplementalNode.NodeID, results[0].NodeID)
}

func TestSearcherPartitionsVisibleNotesWithoutDroppingGenericFilters(t *testing.T) {
	ctx := context.Background()
	provider := fixedEmbeddingProvider{"query": {1, 0, 0, 0}}
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceIntelDocSections(ctx, "docs/allowed.md", []codeanchor.IntelDocSection{{
		SectionID: "allowed-section", Path: "docs/allowed.md", Title: "Allowed", Level: 1, Content: "allowed", Fingerprint: "allowed",
	}}, nil, nil))
	require.NoError(t, store.ReplaceIntelDocSections(ctx, "private/denied.md", []codeanchor.IntelDocSection{{
		SectionID: "denied-section", Path: "private/denied.md", Title: "Denied", Level: 1, Content: "denied", Fingerprint: "denied",
	}}, nil, nil))
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"allowed-section", "denied-section"}, []codeanchor.IntelChunk{
		{ChunkID: "allowed-chunk", OwnerID: "allowed-section", OwnerType: "doc_section", Granularity: "section", ContentHash: "allowed"},
		{ChunkID: "denied-chunk", OwnerID: "denied-section", OwnerType: "doc_section", Granularity: "section", ContentHash: "denied"},
	}))
	node := readmodel.NodeRow{NodeID: "node:other", NotePath: "docs/other.md", NodeRefJSON: `{"notePath":"docs/other.md","typeName":"ReferenceDoc","kind":"NOTE"}`, NodeKind: "NOTE", TypeName: "ReferenceDoc", Title: "Other", SourceLocator: "docs/other.md"}
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{node.NotePath}, []readmodel.NodeRow{node}))
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{node.NodeID}, []codeanchor.IntelChunk{{ChunkID: "other-chunk", OwnerID: node.NodeID, OwnerType: embeddings.OntologyNodeOwnerType, Granularity: embeddings.GranularityOntologyNodeVisible, ContentHash: "other"}}))
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{
		"allowed-chunk": {1, 0, 0, 0}, "denied-chunk": {1, 0, 0, 0}, "other-chunk": {1, 0, 0, 0},
	}))

	searcher := Searcher{NoteProvider: provider, IntelStore: store}
	results, err := searcher.Search(ctx, SearchRequest{QueryText: "query", Filters: SearchFilters{
		Types: []string{"note"}, PathPrefixes: []string{"docs"}, Granularity: []string{"section"},
	}, K: 5})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "docs/allowed.md", results[0].Path)
	require.Equal(t, "section", results[0].Granularity)
	require.Equal(t, "note", results[0].Type)
}

func TestPartitionedVisibleNoteSearchMatchesCombinedCandidateWindowAtCrowdingAndTies(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	sections := make([]codeanchor.IntelDocSection, 0, 55)
	chunks := make([]codeanchor.IntelChunk, 0, 57)
	vectors := make(map[string]embeddings.Embedding, 57)
	owners := make([]string, 0, 57)
	for i := 0; i < 55; i++ {
		ownerID := fmt.Sprintf("section-%03d", i)
		chunkID := fmt.Sprintf("chunk-%03d", i)
		sections = append(sections, codeanchor.IntelDocSection{SectionID: ownerID, Path: "docs/crowded.md", Title: ownerID, Level: 1, Content: ownerID, Fingerprint: ownerID})
		chunks = append(chunks, codeanchor.IntelChunk{ChunkID: chunkID, OwnerID: ownerID, OwnerType: "doc_section", Granularity: "section", ContentHash: chunkID})
		owners = append(owners, ownerID)
		vectors[chunkID] = embeddings.Embedding{1, 0, 0, 0}
	}
	require.NoError(t, store.ReplaceIntelDocSections(ctx, "docs/crowded.md", sections, nil, nil))
	nodes := []readmodel.NodeRow{
		{NodeID: "node-cutoff", NotePath: "docs/node.md", NodeRefJSON: `{"notePath":"docs/node.md","typeName":"_FallbackNote","kind":"NOTE"}`, NodeKind: "NOTE", TypeName: ontoschema.FallbackNoteTypeName, Title: "Cutoff", SourceLocator: "docs/node.md"},
		{NodeID: "node-after", NotePath: "docs/after.md", NodeRefJSON: `{"notePath":"docs/after.md","typeName":"_FallbackNote","kind":"NOTE"}`, NodeKind: "NOTE", TypeName: ontoschema.FallbackNoteTypeName, Title: "After", SourceLocator: "docs/after.md"},
	}
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{"docs/node.md", "docs/after.md"}, nodes))
	for _, item := range []struct{ chunkID, ownerID string }{{"chunk-025a", "node-cutoff"}, {"chunk-060", "node-after"}} {
		chunks = append(chunks, codeanchor.IntelChunk{ChunkID: item.chunkID, OwnerID: item.ownerID, OwnerType: "ontology_node", Granularity: embeddings.GranularityOntologyNodeVisible, ContentHash: item.chunkID})
		owners = append(owners, item.ownerID)
		vectors[item.chunkID] = embeddings.Embedding{1, 0, 0, 0}
	}
	require.NoError(t, store.ReplaceIntelChunks(ctx, owners, chunks))
	require.NoError(t, store.UpsertEmbeddings(ctx, vectors))

	query := embeddings.Embedding{1, 0, 0, 0}
	visibleFilters := semdb.EmbeddingSearchFilters{ExcludeGranularity: []string{embeddings.GranularityOntologyNodeSupplemental}}
	combined, _, err := store.SearchEmbeddings(ctx, query, 50, semdb.EmbeddingSearchFilters{
		OwnerTypes: []string{"doc_section", "ontology_node"}, ExcludeGranularity: visibleFilters.ExcludeGranularity,
	})
	require.NoError(t, err)
	docFilters := visibleFilters
	docFilters.ExcludeGranularity = nil
	docFilters.OwnerTypes = []string{"doc_section"}
	docs, _, err := store.SearchEmbeddings(ctx, query, 50, docFilters)
	require.NoError(t, err)
	visibleFilters.OwnerTypes = []string{"ontology_node"}
	ontologyNodes, _, err := store.SearchEmbeddings(ctx, query, 50, visibleFilters)
	require.NoError(t, err)
	split := mergeVisibleNoteCandidates(docs, ontologyNodes, 50)

	require.Equal(t, scoredChunkIDsForSemanticTest(combined), scoredChunkIDsForSemanticTest(split))
	require.Len(t, split, 50)
	require.Equal(t, "chunk-000", split[0].ChunkID)
	require.Equal(t, "chunk-025a", split[26].ChunkID)
	require.Equal(t, "chunk-048", split[49].ChunkID)
	require.NotContains(t, scoredChunkIDsForSemanticTest(split), "chunk-049")
	require.NotContains(t, scoredChunkIDsForSemanticTest(split), "chunk-060")

	diverse := interleaveScoredChunksByOwner(split, 3)
	require.Equal(t, []string{"chunk-000", "chunk-025a", "chunk-001"}, scoredChunkIDsForSemanticTest(diverse))
}

func scoredChunkIDsForSemanticTest(items []semdb.ScoredChunk) []string {
	out := make([]string, len(items))
	for i := range items {
		out[i] = items[i].ChunkID
	}
	return out
}
