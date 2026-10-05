package semantic

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

type fixedEmbeddingProvider map[string]embeddings.Embedding

func (p fixedEmbeddingProvider) EmbedTexts(_ context.Context, texts []string) ([]embeddings.Embedding, error) {
	out := make([]embeddings.Embedding, 0, len(texts))
	for _, text := range texts {
		out = append(out, append(embeddings.Embedding(nil), p[text]...))
	}
	return out, nil
}

func (p fixedEmbeddingProvider) Dimensions() int { return 4 }

func TestSearcher_ReturnsErrorForTypedNilIntelStore(t *testing.T) {
	var intelStore *semdb.Store
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	searcher := Searcher{NoteProvider: prov, IntelStore: intelStore}

	results, err := searcher.Search(context.Background(), SearchRequest{QueryText: "ontology", K: 5})

	require.Nil(t, results)
	require.EqualError(t, err, "semantic search requires intel store")
}

func TestSearcher_SurveyNodesByType_ReturnsErrorForTypedNilIntelStore(t *testing.T) {
	var intelStore *semdb.Store
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	searcher := Searcher{NoteProvider: prov, IntelStore: intelStore}

	results, err := searcher.SurveyNodesByType(context.Background(), SurveyNodesByTypeRequest{
		QueryTerms: []string{"ontology"},
		TypeNames:  []string{"TechnicalSpec"},
		K:          5,
	})

	require.Nil(t, results)
	require.EqualError(t, err, "semantic search requires intel store")
}

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

	visibleNode := codeanchor.IntelOntologyNode{
		NodeID: "node-visible", NotePath: "visible.html", NodeRefJSON: `{"notePath":"visible.html","typeName":"_FallbackNote","kind":"NOTE"}`,
		NodeKind: "NOTE", TypeName: FallbackNoteTypeName, Title: "Visible", SourceLocator: "visible.html",
	}
	supplementalNode := codeanchor.IntelOntologyNode{
		NodeID: "node-supplemental", NotePath: "data.html", NodeRefJSON: `{"notePath":"data.html","typeName":"_FallbackNote","kind":"NOTE"}`,
		NodeKind: "NOTE", TypeName: FallbackNoteTypeName, Title: "Data", SourceLocator: "data.html",
	}
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{"visible.html", "data.html"}, []codeanchor.IntelOntologyNode{visibleNode, supplementalNode}))

	chunks := []codeanchor.IntelChunk{{
		ChunkID: "visible", OwnerID: visibleNode.NodeID, OwnerType: OntologyNodeOwnerType, Ord: 0,
		Granularity: GranularityOntologyNodeVisible, Heading: "Visible", ContentHash: "visible",
	}}
	vectors := map[string]embeddings.Embedding{"visible": {0.9, 0.1, 0, 0}}
	for i := 0; i < 60; i++ {
		chunkID := fmt.Sprintf("supplemental-%02d", i)
		chunks = append(chunks, codeanchor.IntelChunk{
			ChunkID: chunkID, OwnerID: supplementalNode.NodeID, OwnerType: OntologyNodeOwnerType, Ord: i,
			Granularity: GranularityOntologyNodeSupplemental, Heading: "Data", ContentHash: chunkID,
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
		QueryText: "query", Filters: SearchFilters{Types: []string{"note"}, Granularity: []string{GranularityOntologyNodeSupplemental}}, K: 1,
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
	node := codeanchor.IntelOntologyNode{NodeID: "node:other", NotePath: "docs/other.md", NodeRefJSON: `{"notePath":"docs/other.md","typeName":"ReferenceDoc","kind":"NOTE"}`, NodeKind: "NOTE", TypeName: "ReferenceDoc", Title: "Other", SourceLocator: "docs/other.md"}
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{node.NotePath}, []codeanchor.IntelOntologyNode{node}))
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{node.NodeID}, []codeanchor.IntelChunk{{ChunkID: "other-chunk", OwnerID: node.NodeID, OwnerType: OntologyNodeOwnerType, Granularity: GranularityOntologyNodeVisible, ContentHash: "other"}}))
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
	nodes := []codeanchor.IntelOntologyNode{
		{NodeID: "node-cutoff", NotePath: "docs/node.md", NodeRefJSON: `{"notePath":"docs/node.md","typeName":"_FallbackNote","kind":"NOTE"}`, NodeKind: "NOTE", TypeName: FallbackNoteTypeName, Title: "Cutoff", SourceLocator: "docs/node.md"},
		{NodeID: "node-after", NotePath: "docs/after.md", NodeRefJSON: `{"notePath":"docs/after.md","typeName":"_FallbackNote","kind":"NOTE"}`, NodeKind: "NOTE", TypeName: FallbackNoteTypeName, Title: "After", SourceLocator: "docs/after.md"},
	}
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{"docs/node.md", "docs/after.md"}, nodes))
	for _, item := range []struct{ chunkID, ownerID string }{{"chunk-025a", "node-cutoff"}, {"chunk-060", "node-after"}} {
		chunks = append(chunks, codeanchor.IntelChunk{ChunkID: item.chunkID, OwnerID: item.ownerID, OwnerType: "ontology_node", Granularity: GranularityOntologyNodeVisible, ContentHash: item.chunkID})
		owners = append(owners, item.ownerID)
		vectors[item.chunkID] = embeddings.Embedding{1, 0, 0, 0}
	}
	require.NoError(t, store.ReplaceIntelChunks(ctx, owners, chunks))
	require.NoError(t, store.UpsertEmbeddings(ctx, vectors))

	query := embeddings.Embedding{1, 0, 0, 0}
	visibleFilters := semdb.EmbeddingSearchFilters{ExcludeGranularity: []string{GranularityOntologyNodeSupplemental}}
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

func TestSearcher_ReturnsOntologyNodeMetadata(t *testing.T) {
	ctx := context.Background()
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	intelPath := filepath.Join(t.TempDir(), "intel.db")
	intelStore, err := semdb.Open(intelPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	node := codeanchor.IntelOntologyNode{
		NodeID:                "node:story",
		NotePath:              "docs/spec.md",
		NodeRefJSON:           `{"notePath":"docs/spec.md","nodeId":"story","typeName":"UserStory","kind":"EMBEDDED"}`,
		NodeKind:              "EMBEDDED",
		TypeName:              "UserStory",
		ParentNodeID:          "node:stories",
		ParentTypeName:        "UserStoriesSection",
		Title:                 "Story A",
		SourceLocator:         "docs/spec.md#^story-a",
		StartByte:             10,
		EndByte:               80,
		StructuralFingerprint: "struct-story",
		SchemaHash:            "schema-1",
		UpdatedAt:             1,
	}
	require.NoError(t, intelStore.ReplaceOntologyNodes(ctx, []string{"docs/spec.md"}, []codeanchor.IntelOntologyNode{node}))
	require.NoError(t, intelStore.ReplaceIntelChunks(ctx, []string{node.NodeID}, []codeanchor.IntelChunk{{
		ChunkID:     "node-chunk",
		OwnerID:     node.NodeID,
		OwnerType:   "ontology_node",
		Ord:         0,
		Granularity: GranularityOntologyNodeBody,
		Breadcrumb:  "docs/spec.md > User Stories > Story A",
		Heading:     "Story A",
		ContentHash: "chunk-hash",
		StartByte:   10,
		EndByte:     80,
		UpdatedAt:   1,
	}}))
	vec := embedSingle(t, prov, "user story checkout flow")
	require.NoError(t, intelStore.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{"node-chunk": vec}))

	searcher := Searcher{NoteProvider: prov, IntelStore: intelStore}
	results, err := searcher.Search(ctx, SearchRequest{QueryText: "user story checkout flow", Filters: SearchFilters{Types: []string{"note"}}, K: 5})
	require.NoError(t, err)
	require.NotEmpty(t, results)

	require.Equal(t, "ontology_node", results[0].Kind)
	require.Equal(t, node.NodeID, results[0].NodeID)
	require.Equal(t, node.NodeRefJSON, results[0].NodeRefJSON)
	require.Equal(t, node.SourceLocator, results[0].SourceLocator)
	require.Equal(t, node.TypeName, results[0].NodeType)
	require.Equal(t, node.ParentNodeID, results[0].ParentNodeID)
	require.Equal(t, node.NotePath, results[0].Path)
	require.Contains(t, results[0].Handle, "nodechunk:")
}

func TestSearcher_RendersFallbackOntologyNodeAsUntypedNote(t *testing.T) {
	ctx := context.Background()
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	intelStore, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	node := codeanchor.IntelOntologyNode{
		NodeID:        "node:fallback",
		NotePath:      "notes/research.md",
		NodeRefJSON:   `{"notePath":"notes/research.md","typeName":"_FallbackNote","kind":"NOTE"}`,
		NodeKind:      "NOTE",
		TypeName:      FallbackNoteTypeName,
		Title:         "Research",
		SourceLocator: "notes/research.md",
		UpdatedAt:     1,
	}
	require.NoError(t, intelStore.ReplaceOntologyNodes(ctx, []string{node.NotePath}, []codeanchor.IntelOntologyNode{node}))
	require.NoError(t, intelStore.ReplaceIntelChunks(ctx, []string{node.NodeID}, []codeanchor.IntelChunk{{
		ChunkID:     "fallback-body",
		OwnerID:     node.NodeID,
		OwnerType:   "ontology_node",
		Ord:         1,
		Granularity: GranularityOntologyNodeBody,
		Breadcrumb:  "notes/research.md > untyped note > Research",
		Heading:     "Research",
		ContentHash: "fallback-hash",
		UpdatedAt:   1,
	}}))
	vec := embedSingle(t, prov, "pressure valve research")
	require.NoError(t, intelStore.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{"fallback-body": vec}))

	searcher := Searcher{NoteProvider: prov, IntelStore: intelStore}
	results, err := searcher.Search(ctx, SearchRequest{QueryText: "pressure valve research", Filters: SearchFilters{Types: []string{"note"}}, K: 5})
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.Equal(t, "untyped note", results[0].NodeType)
	require.Equal(t, node.NotePath, results[0].NoteID)
	require.Contains(t, results[0].Handle, "nodechunk:")
}

func TestSearcher_ReturnsFallbackSectionAsNodeBackedEvidence(t *testing.T) {
	ctx := context.Background()
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	intelStore, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	node := codeanchor.IntelOntologyNode{
		NodeID:        "node:fallback-section",
		NotePath:      "notes/research.md",
		NodeRefJSON:   `{"notePath":"notes/research.md","fragment":"findings-18","nodeId":"notes/research.md#findings-18","typeName":"_FallbackSection","kind":"SECTION"}`,
		NodeKind:      "SECTION",
		TypeName:      "_FallbackSection",
		ParentNodeID:  "node:fallback-note",
		Title:         "Findings",
		SourceLocator: "notes/research.md#findings-18",
		UpdatedAt:     1,
	}
	require.NoError(t, intelStore.ReplaceOntologyNodes(ctx, []string{node.NotePath}, []codeanchor.IntelOntologyNode{node}))
	require.NoError(t, intelStore.ReplaceIntelChunks(ctx, []string{node.NodeID}, []codeanchor.IntelChunk{{
		ChunkID:     "fallback-section-body",
		OwnerID:     node.NodeID,
		OwnerType:   "ontology_node",
		Ord:         1,
		Granularity: GranularityOntologyNodeBody,
		Breadcrumb:  "notes/research.md > Findings",
		Heading:     "Findings",
		ContentHash: "fallback-section-hash",
		UpdatedAt:   1,
	}}))
	vec := embedSingle(t, prov, "fallback section pressure valve")
	require.NoError(t, intelStore.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{"fallback-section-body": vec}))

	searcher := Searcher{NoteProvider: prov, IntelStore: intelStore}
	results, err := searcher.Search(ctx, SearchRequest{QueryText: "fallback section pressure valve", Filters: SearchFilters{Types: []string{"note"}}, K: 5})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "node:fallback-section", results[0].NodeID)
	require.Equal(t, "untyped note section", results[0].NodeType)
	require.Equal(t, "SECTION", results[0].NodeKind)
	require.Equal(t, node.ParentNodeID, results[0].ParentNodeID)
	require.Equal(t, node.SourceLocator, results[0].SourceLocator)
	require.Equal(t, node.NotePath, results[0].NoteID)
	require.Contains(t, results[0].Handle, "nodechunk:")
	require.NotContains(t, results[0].Handle, "notechunk:")
}

func TestSearcher_SkipsOntologyNodeChunksMissingCatalogIdentity(t *testing.T) {
	ctx := context.Background()
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	intelStore, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	require.NoError(t, intelStore.ReplaceIntelChunks(ctx, []string{"node:stale"}, []codeanchor.IntelChunk{{
		ChunkID:     "stale-node-body",
		OwnerID:     "node:stale",
		OwnerType:   "ontology_node",
		Ord:         1,
		Granularity: GranularityOntologyNodeBody,
		Breadcrumb:  "notes/stale.md",
		Heading:     "Stale",
		ContentHash: "stale-hash",
		UpdatedAt:   1,
	}}))
	vec := embedSingle(t, prov, "stale ontology chunk")
	require.NoError(t, intelStore.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{"stale-node-body": vec}))

	searcher := Searcher{NoteProvider: prov, IntelStore: intelStore}
	results, err := searcher.Search(ctx, SearchRequest{QueryText: "stale ontology chunk", Filters: SearchFilters{Types: []string{"note"}}, K: 5})
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestSearcher_ReturnsCatalogBackedOntologyNodeChunksWithInvalidNodeRef(t *testing.T) {
	for _, tc := range []struct{ name, id, path, raw string }{
		{"valid JSON without node identity", "node:invalid-ref", "notes/invalid.md", `{}`},
		{"malformed JSON", "node:malformed-ref", "notes/malformed.md", `{`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
			intelStore, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
			require.NoError(t, err)
			t.Cleanup(func() { _ = intelStore.Close() })
			node := codeanchor.IntelOntologyNode{NodeID: tc.id, NotePath: tc.path, NodeRefJSON: tc.raw, NodeKind: "NOTE", TypeName: "ReferenceDoc", Title: tc.name, SourceLocator: tc.path, UpdatedAt: 1}
			require.NoError(t, intelStore.ReplaceOntologyNodes(ctx, []string{node.NotePath}, []codeanchor.IntelOntologyNode{node}))
			chunkID := tc.id + ":body"
			require.NoError(t, intelStore.ReplaceIntelChunks(ctx, []string{node.NodeID}, []codeanchor.IntelChunk{{ChunkID: chunkID, OwnerID: node.NodeID, OwnerType: "ontology_node", Ord: 0, Granularity: GranularityOntologyNodeBody, ContentHash: "body-hash", UpdatedAt: 1}}))
			require.NoError(t, intelStore.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{chunkID: embedSingle(t, prov, "ontology reference")}))
			searcher := Searcher{NoteProvider: prov, IntelStore: intelStore}
			results, err := searcher.Search(ctx, SearchRequest{QueryText: "ontology reference", Filters: SearchFilters{Types: []string{"note"}}, K: 5})
			require.NoError(t, err)
			require.Len(t, results, 1)
			require.Equal(t, tc.path, results[0].Path)
			require.Equal(t, tc.id, results[0].NodeID)
			require.Equal(t, tc.raw, results[0].NodeRefJSON)
			require.Nil(t, results[0].NodeRef)
		})
	}
}

func TestSearcher_SurveyNodesByType_UnionsTermsAndDedupesByNode(t *testing.T) {
	ctx := context.Background()
	prov := fixedEmbeddingProvider{
		"alpha": {1, 0, 0, 0},
		"beta":  {0, 1, 0, 0},
	}
	intelStore, err := semdb.Open(filepath.Join(t.TempDir(), "survey.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	specA := codeanchor.IntelOntologyNode{
		NodeID:                "node:spec-a",
		NotePath:              "docs/spec-a.md",
		NodeRefJSON:           `{"notePath":"docs/spec-a.md","nodeId":"spec-a","typeName":"TechnicalSpec","kind":"NOTE"}`,
		NodeKind:              "NOTE",
		TypeName:              "TechnicalSpec",
		Title:                 "Spec A",
		SourceLocator:         "docs/spec-a.md",
		StructuralFingerprint: "spec-a-fp",
		SchemaHash:            "schema",
		UpdatedAt:             1,
	}
	specB := codeanchor.IntelOntologyNode{
		NodeID:                "node:spec-b",
		NotePath:              "docs/spec-b.md",
		NodeRefJSON:           `{"notePath":"docs/spec-b.md","nodeId":"spec-b","typeName":"TechnicalSpec","kind":"NOTE"}`,
		NodeKind:              "NOTE",
		TypeName:              "TechnicalSpec",
		Title:                 "Spec B",
		SourceLocator:         "docs/spec-b.md",
		StructuralFingerprint: "spec-b-fp",
		SchemaHash:            "schema",
		UpdatedAt:             1,
	}
	ref := codeanchor.IntelOntologyNode{
		NodeID:                "node:reference",
		NotePath:              "docs/reference.md",
		NodeRefJSON:           `{"notePath":"docs/reference.md","nodeId":"reference","typeName":"ReferenceDoc","kind":"NOTE"}`,
		NodeKind:              "NOTE",
		TypeName:              "ReferenceDoc",
		Title:                 "Reference",
		SourceLocator:         "docs/reference.md",
		StructuralFingerprint: "ref-fp",
		SchemaHash:            "schema",
		UpdatedAt:             1,
	}
	require.NoError(t, intelStore.ReplaceOntologyNodes(ctx, []string{specA.NotePath, specB.NotePath, ref.NotePath}, []codeanchor.IntelOntologyNode{specA, specB, ref}))
	require.NoError(t, intelStore.ReplaceIntelChunks(ctx, []string{specA.NodeID, specB.NodeID, ref.NodeID}, []codeanchor.IntelChunk{
		{ChunkID: "spec-a-body", OwnerID: specA.NodeID, OwnerType: "ontology_node", Ord: 0, Granularity: GranularityOntologyNodeBody, Heading: "Spec A body", ContentHash: "spec-a-body"},
		{ChunkID: "spec-a-continuation", OwnerID: specA.NodeID, OwnerType: "ontology_node", Ord: 1, Granularity: GranularityOntologyNodeBody, Heading: "Spec A continuation", ContentHash: "spec-a-continuation"},
		{ChunkID: "spec-b-body", OwnerID: specB.NodeID, OwnerType: "ontology_node", Ord: 0, Granularity: GranularityOntologyNodeBody, Heading: "Spec B body", ContentHash: "spec-b-body"},
		{ChunkID: "reference-body", OwnerID: ref.NodeID, OwnerType: "ontology_node", Ord: 0, Granularity: GranularityOntologyNodeBody, Heading: "Reference body", ContentHash: "reference-body"},
	}))
	require.NoError(t, intelStore.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{
		"spec-a-body":         {0.7, 0.3, 0, 0},
		"spec-a-continuation": {1, 0, 0, 0},
		"spec-b-body":         {0, 1, 0, 0},
		"reference-body":      {1, 0, 0, 0},
	}))

	searcher := Searcher{NoteProvider: prov, IntelStore: intelStore}
	results, err := searcher.SurveyNodesByType(ctx, SurveyNodesByTypeRequest{
		QueryTerms: []string{"alpha", "beta"},
		TypeNames:  []string{"TechnicalSpec"},
		K:          10,
	})
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.ElementsMatch(t, []string{"node:spec-a", "node:spec-b"}, []string{results[0].NodeID, results[1].NodeID})
	for _, result := range results {
		require.Equal(t, "note", result.Type)
		require.Equal(t, "TechnicalSpec", result.NodeType)
		require.InDelta(t, 1.0, result.Score, 0.001)
	}

	results, err = searcher.SurveyNodesByType(ctx, SurveyNodesByTypeRequest{
		QueryTerms: []string{"alpha", "beta"},
		TypeNames:  []string{"TechnicalSpec"},
		K:          1,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "node:spec-a", results[0].NodeID)
	require.Equal(t, "Spec A", results[0].Title)
}

func TestInterleaveScoredChunksByOwner_DiversifiesOntologyNodesByNote(t *testing.T) {
	scored := []semdb.ScoredChunk{
		{ChunkID: "a1", OwnerID: "node:a1", OwnerType: "ontology_node", Path: "docs/a.md", Score: 0.99},
		{ChunkID: "a2", OwnerID: "node:a2", OwnerType: "ontology_node", Path: "docs/a.md", Score: 0.98},
		{ChunkID: "a3", OwnerID: "node:a3", OwnerType: "ontology_node", Path: "docs/a.md", Score: 0.97},
		{ChunkID: "b1", OwnerID: "node:b1", OwnerType: "ontology_node", Path: "docs/b.md", Score: 0.96},
	}

	out := interleaveScoredChunksByOwner(scored, 2)

	require.Equal(t, []string{"a1", "b1"}, []string{out[0].ChunkID, out[1].ChunkID})
}

func embedSingle(t *testing.T, prov embeddings.Provider, text string) embeddings.Embedding {
	t.Helper()
	vecs, err := prov.EmbedTexts(context.Background(), []string{text})
	require.NoError(t, err)
	require.Len(t, vecs, 1)
	return vecs[0]
}

func setupIntelStore(t *testing.T, prov embeddings.Provider) *semdb.Store {
	t.Helper()
	ctx := context.Background()
	intelPath := filepath.Join(t.TempDir(), "intel.db")
	intelStore, err := semdb.Open(intelPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	anchors := []codeanchor.IntelAnchor{
		{
			AnchorID:    "anchor1",
			Lang:        codeanchor.LangGo,
			Kind:        "function",
			Path:        "pkg/utils.go",
			Symbol:      "Helper",
			FQN:         "pkg.utils.Helper",
			Fingerprint: "fp1",
		},
	}
	require.NoError(t, intelStore.ReplaceIntelCodeFile(ctx, "pkg/utils.go", anchors, nil, nil))

	sections := []codeanchor.IntelDocSection{
		{
			SectionID:   "section1",
			Path:        "docs/README.md",
			Title:       "Overview",
			Level:       1,
			Content:     "This is the overview of the project",
			Fingerprint: "fp2",
		},
	}
	require.NoError(t, intelStore.ReplaceIntelDocSections(ctx, "docs/README.md", sections, nil, nil))

	chunks := []codeanchor.IntelChunk{
		{ChunkID: "chunk1", OwnerID: "anchor1", OwnerType: "anchor", Ord: 0, Granularity: "symbol", ContentHash: "h1"},
		{ChunkID: "chunk2", OwnerID: "section1", OwnerType: "doc_section", Ord: 0, Granularity: "section", ContentHash: "h2"},
	}
	require.NoError(t, intelStore.ReplaceIntelChunks(ctx, []string{"anchor1", "section1"}, chunks))

	codeVec := embedSingle(t, prov, "helper function utility")
	noteVec := embedSingle(t, prov, "overview documentation")
	embeddingsMap := map[string]embeddings.Embedding{
		"chunk1": codeVec,
		"chunk2": noteVec,
	}
	require.NoError(t, intelStore.UpsertEmbeddings(ctx, embeddingsMap))

	return intelStore
}

func setupIntelStoreWithEmbeddings(t *testing.T, codeVec, noteVec embeddings.Embedding) *semdb.Store {
	t.Helper()
	ctx := context.Background()
	intelPath := filepath.Join(t.TempDir(), "intel.db")
	intelStore, err := semdb.Open(intelPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	anchors := []codeanchor.IntelAnchor{
		{
			AnchorID:    "anchor1",
			Lang:        codeanchor.LangGo,
			Kind:        "function",
			Path:        "pkg/utils.go",
			Symbol:      "Helper",
			FQN:         "pkg.utils.Helper",
			Fingerprint: "fp1",
		},
	}
	require.NoError(t, intelStore.ReplaceIntelCodeFile(ctx, "pkg/utils.go", anchors, nil, nil))

	sections := []codeanchor.IntelDocSection{
		{
			SectionID:   "section1",
			Path:        "docs/README.md",
			Title:       "Overview",
			Level:       1,
			Content:     "This is the overview of the project",
			Fingerprint: "fp2",
		},
	}
	require.NoError(t, intelStore.ReplaceIntelDocSections(ctx, "docs/README.md", sections, nil, nil))

	chunks := []codeanchor.IntelChunk{
		{ChunkID: "chunk1", OwnerID: "anchor1", OwnerType: "anchor", Ord: 0, Granularity: "symbol", ContentHash: "h1"},
		{ChunkID: "chunk2", OwnerID: "section1", OwnerType: "doc_section", Ord: 0, Granularity: "section", ContentHash: "h2"},
	}
	require.NoError(t, intelStore.ReplaceIntelChunks(ctx, []string{"anchor1", "section1"}, chunks))

	embeddingsMap := map[string]embeddings.Embedding{
		"chunk1": codeVec,
		"chunk2": noteVec,
	}
	require.NoError(t, intelStore.UpsertEmbeddings(ctx, embeddingsMap))

	return intelStore
}

func TestSearcher_UsesBothEmbeddingsWhenProvidersDiffer(t *testing.T) {
	for _, shared := range []bool{true, false} {
		name := "different providers"
		if shared {
			name = "shared provider"
		}
		t.Run(name, func(t *testing.T) {
			codeProv := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
			var noteProv embeddings.Provider = embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 12})
			var intelStore *semdb.Store
			if shared {
				noteProv = codeProv
				intelStore = setupIntelStore(t, codeProv)
			} else {
				codeVec := embedSingle(t, codeProv, "helper function utility")
				noteVec := embedSingle(t, noteProv, "overview documentation")
				intelStore = setupIntelStoreWithEmbeddings(t, codeVec, noteVec)
			}
			searcher := Searcher{CodeProvider: codeProv, NoteProvider: noteProv, IntelStore: intelStore}
			results, err := searcher.Search(context.Background(), SearchRequest{QueryText: "overview documentation", K: 10})
			require.NoError(t, err)
			var foundCode, foundNote bool
			for _, result := range results {
				if result.Type == "code" && result.AnchorID == "anchor1" && result.Path == "pkg/utils.go" && result.Symbol == "Helper" {
					foundCode = true
				}
				if result.Type == "note" && result.Path == "docs/README.md" {
					foundNote = true
				}
			}
			require.True(t, foundCode, "code candidate must survive")
			require.True(t, foundNote, "note candidate must survive")
		})
	}
}
