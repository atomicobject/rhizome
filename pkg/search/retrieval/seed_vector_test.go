package retrieval

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
)

func TestSeedVectorRetrieverUsesOntologyChunksForTypedNoteSeeds(t *testing.T) {
	ctx := context.Background()
	intelStore, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	node := codeanchor.IntelOntologyNode{
		NodeID:        "node:typed",
		NotePath:      "docs/Typed.MD",
		NodeRefJSON:   `{"notePath":"docs/Typed.MD","nodeId":"typed","typeName":"ReferenceDoc"}`,
		SourceLocator: "docs/Typed.MD",
		NodeKind:      "ROOT",
		TypeName:      "ReferenceDoc",
		Title:         "Typed",
	}
	require.NoError(t, intelStore.ReplaceOntologyNodes(ctx, []string{node.NotePath}, []codeanchor.IntelOntologyNode{node}))
	require.NoError(t, intelStore.ReplaceIntelChunks(ctx, []string{node.NodeID}, []codeanchor.IntelChunk{{
		ChunkID:     "typed-node-chunk",
		OwnerID:     node.NodeID,
		OwnerType:   "ontology_node",
		Ord:         0,
		Granularity: semantic.GranularityOntologyNodeBody,
		Breadcrumb:  "Typed > Body",
		Heading:     "Typed",
		ContentHash: "typed-hash",
		UpdatedAt:   1,
	}}))
	require.NoError(t, intelStore.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{"typed-node-chunk": {1, 0, 0, 0}}))

	retriever := SeedVectorRetriever{Semantic: &semantic.Searcher{IntelStore: intelStore}}
	results, err := retriever.Retrieve(ctx, search.QuerySpec{
		Seeds:  []knowledge.Handle{knowledge.NoteHandle(node.NotePath)},
		Limits: search.Limits{Total: 5},
	})
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.Equal(t, node.NotePath, results[0].NoteID)
	require.Equal(t, "note", results[0].Type)
	require.Equal(t, node.NodeID, results[0].NodeID)
	require.Equal(t, node.NodeRefJSON, results[0].NodeRefJSON)
	require.Equal(t, node.ParentNodeID, results[0].ParentNodeID)
	require.Equal(t, knowledge.KindNodeChunk, results[0].Handle.Kind)
}

func TestSeedVectorRetrieverFallsBackToDocSectionsForPartialTypedNoteIndex(t *testing.T) {
	ctx := context.Background()
	intelStore, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	notePath := "docs/typed.md"
	require.NoError(t, intelStore.ReplaceOntologyNodes(ctx, []string{notePath}, []codeanchor.IntelOntologyNode{{
		NodeID:        "node:typed",
		NotePath:      notePath,
		NodeRefJSON:   `{"notePath":"docs/typed.md","nodeId":"typed","typeName":"ReferenceDoc"}`,
		SourceLocator: notePath,
		NodeKind:      "ROOT",
		TypeName:      "ReferenceDoc",
		Title:         "Typed",
	}}))
	require.NoError(t, intelStore.ReplaceIntelDocSections(ctx, notePath, []codeanchor.IntelDocSection{{
		SectionID:   "section:typed",
		Path:        notePath,
		Title:       "Typed",
		Level:       1,
		Content:     "semantic fallback",
		Fingerprint: "typed-fp",
	}}, nil, nil))
	require.NoError(t, intelStore.ReplaceIntelChunks(ctx, []string{"section:typed"}, []codeanchor.IntelChunk{{
		ChunkID:     "typed-doc-chunk",
		OwnerID:     "section:typed",
		OwnerType:   "doc_section",
		Ord:         0,
		Granularity: "section",
		Breadcrumb:  "Typed",
		Heading:     "Typed",
		ContentHash: "typed-doc-hash",
	}}))
	require.NoError(t, intelStore.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{"typed-doc-chunk": {1, 0, 0, 0}}))

	retriever := SeedVectorRetriever{Semantic: &semantic.Searcher{IntelStore: intelStore}}
	results, err := retriever.Retrieve(ctx, search.QuerySpec{
		Seeds:  []knowledge.Handle{knowledge.NoteHandle(notePath)},
		Limits: search.Limits{Total: 5},
	})
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.Equal(t, notePath, results[0].NoteID)
}

func TestSeedVectorRetrieverSkipsStaleOntologyChunksWithoutCatalogIdentity(t *testing.T) {
	ctx := context.Background()
	intelStore, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	notePath := "docs/typed.md"
	require.NoError(t, intelStore.ReplaceIntelDocSections(ctx, notePath, []codeanchor.IntelDocSection{{
		SectionID:   "section:typed",
		Path:        notePath,
		Title:       "Typed",
		Level:       1,
		Content:     "seed section",
		Fingerprint: "typed-fp",
	}}, nil, nil))
	require.NoError(t, intelStore.ReplaceIntelChunks(ctx, []string{"section:typed", "stale-node"}, []codeanchor.IntelChunk{
		{
			ChunkID:     "typed-doc-chunk",
			OwnerID:     "section:typed",
			OwnerType:   "doc_section",
			Ord:         0,
			Granularity: "section",
			Breadcrumb:  "Typed",
			Heading:     "Typed",
			ContentHash: "typed-doc-hash",
		},
		{
			ChunkID:     "stale-node-chunk",
			OwnerID:     "stale-node",
			OwnerType:   "ontology_node",
			Ord:         0,
			Granularity: semantic.GranularityOntologyNodeBody,
			Breadcrumb:  "Stale",
			Heading:     "Stale",
			ContentHash: "stale-hash",
		},
	}))
	require.NoError(t, intelStore.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{
		"typed-doc-chunk":  {1, 0, 0, 0},
		"stale-node-chunk": {1, 0, 0, 0},
	}))

	retriever := SeedVectorRetriever{Semantic: &semantic.Searcher{IntelStore: intelStore}}
	results, err := retriever.Retrieve(ctx, search.QuerySpec{
		Seeds:  []knowledge.Handle{knowledge.NoteHandle(notePath)},
		Limits: search.Limits{Total: 5},
	})
	require.NoError(t, err)
	require.NotEmpty(t, results)
	for _, result := range results {
		require.NotEqual(t, "stale-node", result.NodeID)
		require.NotEqual(t, knowledge.KindNodeChunk, result.Handle.Kind)
	}
}

func TestSeedVectorRetrieverKeepsMultipleOntologyNodesFromSameNote(t *testing.T) {
	ctx := context.Background()
	intelStore, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	nodes := []codeanchor.IntelOntologyNode{
		{
			NodeID:        "node:root",
			NotePath:      "docs/typed.md",
			NodeRefJSON:   `{"notePath":"docs/typed.md","typeName":"ReferenceDoc","kind":"NOTE"}`,
			SourceLocator: "docs/typed.md",
			NodeKind:      "NOTE",
			TypeName:      "ReferenceDoc",
			Title:         "Typed",
		},
		{
			NodeID:        "node:child",
			NotePath:      "docs/typed.md",
			NodeRefJSON:   `{"notePath":"docs/typed.md","nodeId":"child","typeName":"Section","kind":"SECTION"}`,
			SourceLocator: "docs/typed.md#child",
			NodeKind:      "SECTION",
			TypeName:      "Section",
			ParentNodeID:  "node:root",
			Title:         "Child",
		},
	}
	require.NoError(t, intelStore.ReplaceOntologyNodes(ctx, []string{"docs/typed.md"}, nodes))
	require.NoError(t, intelStore.ReplaceIntelChunks(ctx, []string{"node:root", "node:child"}, []codeanchor.IntelChunk{
		{
			ChunkID:     "root-node-chunk",
			OwnerID:     "node:root",
			OwnerType:   "ontology_node",
			Ord:         0,
			Granularity: semantic.GranularityOntologyNodeBody,
			ContentHash: "root-hash",
		},
		{
			ChunkID:     "child-node-chunk",
			OwnerID:     "node:child",
			OwnerType:   "ontology_node",
			Ord:         0,
			Granularity: semantic.GranularityOntologyNodeBody,
			ContentHash: "child-hash",
		},
	}))
	require.NoError(t, intelStore.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{
		"root-node-chunk":  {1, 0, 0, 0},
		"child-node-chunk": {1, 0, 0, 0},
	}))

	retriever := SeedVectorRetriever{Semantic: &semantic.Searcher{IntelStore: intelStore}}
	results, err := retriever.Retrieve(ctx, search.QuerySpec{
		Seeds:  []knowledge.Handle{knowledge.NoteHandle("docs/typed.md")},
		Limits: search.Limits{Total: 10},
	})
	require.NoError(t, err)
	got := map[string]bool{}
	for _, result := range results {
		if result.Handle.Kind == knowledge.KindNodeChunk {
			got[result.NodeID] = true
		}
	}
	require.True(t, got["node:root"])
	require.True(t, got["node:child"])
}
