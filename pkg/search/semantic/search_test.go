package semantic

import (
	"context"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

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

func TestSearcher_ReturnsOntologyNodeMetadata(t *testing.T) {
	ctx := context.Background()
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	intelPath := filepath.Join(t.TempDir(), "intel.db")
	intelStore, err := semdb.Open(intelPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	node := readmodel.NodeRow{
		NodeID:                "node:story",
		NotePath:              "docs/spec.md",
		NodeRefJSON:           `{"notePath":"docs/spec.md","fragment":"^story-a","nodeId":"story","typeName":"UserStory","kind":"EMBEDDED"}`,
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
	require.NoError(t, intelStore.ReplaceOntologyNodes(ctx, []string{"docs/spec.md"}, []readmodel.NodeRow{node}))
	require.NoError(t, intelStore.ReplaceIntelChunks(ctx, []string{node.NodeID}, []codeanchor.IntelChunk{{
		ChunkID:     "node-chunk",
		OwnerID:     node.NodeID,
		OwnerType:   "ontology_node",
		Ord:         0,
		Granularity: embeddings.GranularityOntologyNodeBody,
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
	require.Equal(t, node.SourceLocator, ontology.NodeSourceLocator(*results[0].NodeRef))
	require.Equal(t, node.TypeName, results[0].NodeRef.TypeName)
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

	node := readmodel.NodeRow{
		NodeID:        "node:fallback",
		NotePath:      "notes/research.md",
		NodeRefJSON:   `{"notePath":"notes/research.md","typeName":"_FallbackNote","kind":"NOTE"}`,
		NodeKind:      "NOTE",
		TypeName:      ontoschema.FallbackNoteTypeName,
		Title:         "Research",
		SourceLocator: "notes/research.md",
		UpdatedAt:     1,
	}
	require.NoError(t, intelStore.ReplaceOntologyNodes(ctx, []string{node.NotePath}, []readmodel.NodeRow{node}))
	require.NoError(t, intelStore.ReplaceIntelChunks(ctx, []string{node.NodeID}, []codeanchor.IntelChunk{{
		ChunkID:     "fallback-body",
		OwnerID:     node.NodeID,
		OwnerType:   "ontology_node",
		Ord:         1,
		Granularity: embeddings.GranularityOntologyNodeBody,
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
	require.Equal(t, ontoschema.FallbackNoteTypeName, results[0].NodeRef.TypeName)
	require.Equal(t, node.NotePath, results[0].NoteID)
	require.Contains(t, results[0].Handle, "nodechunk:")
}

func TestSearcher_ReturnsFallbackSectionAsNodeBackedEvidence(t *testing.T) {
	ctx := context.Background()
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	intelStore, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	node := readmodel.NodeRow{
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
	require.NoError(t, intelStore.ReplaceOntologyNodes(ctx, []string{node.NotePath}, []readmodel.NodeRow{node}))
	require.NoError(t, intelStore.ReplaceIntelChunks(ctx, []string{node.NodeID}, []codeanchor.IntelChunk{{
		ChunkID:     "fallback-section-body",
		OwnerID:     node.NodeID,
		OwnerType:   "ontology_node",
		Ord:         1,
		Granularity: embeddings.GranularityOntologyNodeBody,
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
	require.Equal(t, ontoschema.FallbackSectionTypeName, results[0].NodeRef.TypeName)
	require.Equal(t, "SECTION", string(results[0].NodeRef.Kind))
	require.Equal(t, node.ParentNodeID, results[0].ParentNodeID)
	require.Equal(t, node.SourceLocator, ontology.NodeSourceLocator(*results[0].NodeRef))
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
		Granularity: embeddings.GranularityOntologyNodeBody,
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
			node := readmodel.NodeRow{NodeID: tc.id, NotePath: tc.path, NodeRefJSON: tc.raw, NodeKind: "NOTE", TypeName: "ReferenceDoc", Title: tc.name, SourceLocator: tc.path, UpdatedAt: 1}
			require.NoError(t, intelStore.ReplaceOntologyNodes(ctx, []string{node.NotePath}, []readmodel.NodeRow{node}))
			chunkID := tc.id + ":body"
			require.NoError(t, intelStore.ReplaceIntelChunks(ctx, []string{node.NodeID}, []codeanchor.IntelChunk{{ChunkID: chunkID, OwnerID: node.NodeID, OwnerType: "ontology_node", Ord: 0, Granularity: embeddings.GranularityOntologyNodeBody, ContentHash: "body-hash", UpdatedAt: 1}}))
			require.NoError(t, intelStore.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{chunkID: embedSingle(t, prov, "ontology reference")}))
			searcher := Searcher{NoteProvider: prov, IntelStore: intelStore}
			results, err := searcher.Search(ctx, SearchRequest{QueryText: "ontology reference", Filters: SearchFilters{Types: []string{"note"}}, K: 5})
			require.NoError(t, err)
			require.Len(t, results, 1)
			require.Equal(t, tc.path, results[0].Path)
			require.Equal(t, tc.id, results[0].NodeID)
			require.Nil(t, results[0].NodeRef)
		})
	}
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

type failingAnchorIdentityStore struct{ *semdb.Store }

func (f failingAnchorIdentityStore) IntelAnchorsByIDs(context.Context, []string) (map[string]codeanchor.IntelAnchor, error) {
	return nil, fmt.Errorf("synthetic identity lookup failure")
}

func TestSearcherDoesNotHideAnchorIdentityLookupFailure(t *testing.T) {
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store := setupIntelStore(t, provider)
	searcher := Searcher{CodeProvider: provider, IntelStore: failingAnchorIdentityStore{store}}
	_, err := searcher.Search(t.Context(), SearchRequest{QueryText: "helper function", Filters: SearchFilters{Types: []string{"code"}}, K: 10})
	require.ErrorContains(t, err, "synthetic identity lookup failure")
}

func TestSearcherSkipsUnknownChunkOwner(t *testing.T) {
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store := setupIntelStore(t, provider)
	results, err := (&Searcher{IntelStore: store}).resultsFromScoredChunks(t.Context(), []semdb.ScoredChunk{{OwnerID: "future-owner", OwnerType: "future-type", Path: "unknown.md", Score: 1}})
	require.NoError(t, err)
	require.Empty(t, results, "unknown persisted owner kinds must not become malformed handles")
}
