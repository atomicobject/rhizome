//go:build fts5

package sqlite

import (
	"context"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

func TestReplaceOntologyNodeReadModelPreservingSemantic_FullReplaceKeepsSemanticRows(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-node-preserve-full.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	node := seedOntologyNodeSemanticRows(t, store)
	before := ontologyNodeSemanticRowCounts(t, store)
	node.Title = "Updated catalog title"
	node.UpdatedAt++

	require.NoError(t, store.ReplaceOntologyNodeReadModelPreservingSemantic(ctx, codeanchor.IntelOntologyNodeReadModel{
		FullReplace: true,
		NotePaths:   []string{node.NotePath},
		Nodes:       []codeanchor.IntelOntologyNode{node},
	}))

	require.Equal(t, before, ontologyNodeSemanticRowCounts(t, store))
	nodes, err := store.OntologyNodesByIDs(ctx, []string{node.NodeID})
	require.NoError(t, err)
	require.Equal(t, "Updated catalog title", nodes[node.NodeID].Title)
}

func TestReplaceOntologyNodeReadModelPreservingSemantic_IncrementalDeleteKeepsSemanticRows(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-node-preserve-incremental.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	node := seedOntologyNodeSemanticRows(t, store)
	before := ontologyNodeSemanticRowCounts(t, store)

	require.NoError(t, store.ReplaceOntologyNodeReadModelPreservingSemantic(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{node.NotePath},
	}))

	require.Equal(t, before, ontologyNodeSemanticRowCounts(t, store))
	nodes, err := store.OntologyNodesByIDs(ctx, []string{node.NodeID})
	require.NoError(t, err)
	require.Empty(t, nodes, "catalog deletion must still land while semantic prerequisites remain untouched")
}

func TestReplaceOntologyNodeReadModel_DefaultFullReplaceStillCleansSemanticRows(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-node-clean-full.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	seedOntologyNodeSemanticRows(t, store)
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{FullReplace: true}))

	require.Equal(t, [3]int{}, ontologyNodeSemanticRowCounts(t, store))
}

func TestReplaceOntologyNodeReadModelPreservingSemantic_ReboundRowIDKeepsCanonicalOwnerBinding(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-node-preserve-binding.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	decoy := codeanchor.IntelOntologyNode{
		NodeID:                "node:decoy",
		NotePath:              "docs/decoy.md",
		NodeRefJSON:           `{"notePath":"docs/decoy.md","nodeId":"decoy","typeName":"Decision","kind":"NOTE"}`,
		NodeKind:              "NOTE",
		TypeName:              "Decision",
		Title:                 "Decoy",
		StructuralFingerprint: "decoy-structure",
		SchemaHash:            "schema-hash",
		UpdatedAt:             9,
	}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{decoy.NotePath},
		Nodes:     []codeanchor.IntelOntologyNode{decoy},
	}))
	target := seedOntologyNodeSemanticRows(t, store)
	replacement := decoy
	replacement.NodeID = "node:replacement"
	replacement.NotePath = "docs/replacement.md"
	replacement.NodeRefJSON = `{"notePath":"docs/replacement.md","nodeId":"replacement","typeName":"Decision","kind":"NOTE"}`
	replacement.Title = "Replacement"

	require.NoError(t, store.ReplaceOntologyNodeReadModelPreservingSemantic(ctx, codeanchor.IntelOntologyNodeReadModel{
		FullReplace: true,
		NotePaths:   []string{target.NotePath, replacement.NotePath},
		Nodes:       []codeanchor.IntelOntologyNode{target, replacement},
	}))

	results, skipped, err := store.SearchEmbeddings(ctx, embeddings.Embedding{1, 0, 0}, 1, EmbeddingSearchFilters{
		OwnerTypes: []string{"ontology_node"},
	})
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Len(t, results, 1)
	require.Equal(t, target.NodeID, results[0].OwnerID)
	require.Equal(t, target.NotePath, results[0].Path, "semantic binding must follow canonical node_id, not a recycled integer row id")
}

func seedOntologyNodeSemanticRows(t *testing.T, store *Store) codeanchor.IntelOntologyNode {
	t.Helper()
	ctx := context.Background()
	node := codeanchor.IntelOntologyNode{
		NodeID:                "node:decision",
		NotePath:              "docs/decision.md",
		NodeRefJSON:           `{"notePath":"docs/decision.md","nodeId":"decision","typeName":"Decision","kind":"NOTE"}`,
		NodeKind:              "NOTE",
		TypeName:              "Decision",
		Title:                 "Decision",
		StructuralFingerprint: "node-structure",
		SchemaHash:            "schema-hash",
		UpdatedAt:             10,
	}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{node.NotePath},
		Nodes:     []codeanchor.IntelOntologyNode{node},
	}))
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{node.NodeID}, []codeanchor.IntelChunk{{
		ChunkID:     "node-chunk",
		OwnerID:     node.NodeID,
		OwnerType:   "ontology_node",
		Ord:         0,
		Granularity: "node_body",
		ContentHash: "chunk-hash",
		StartByte:   0,
		EndByte:     8,
		UpdatedAt:   11,
	}}))
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{
		"node-chunk": {1, 0, 0},
	}))
	require.NoError(t, store.UpsertOntologyNodeEmbeddingStates(ctx, []codeanchor.IntelOntologyNodeEmbeddingState{{
		ChunkID:                  "node-chunk",
		NodeID:                   node.NodeID,
		NotePath:                 node.NotePath,
		TypeName:                 node.TypeName,
		NodeKind:                 node.NodeKind,
		EmbeddingSchemaSignature: "schema-signature",
		NodeStructureFingerprint: node.StructuralFingerprint,
		SourceContentHash:        "source-hash",
		ChunkTextHash:            "chunk-hash",
		ChunkGranularity:         "node_body",
		Provider:                 "test",
		Model:                    "deterministic",
		UpdatedAt:                12,
	}}))
	return node
}

func ontologyNodeSemanticRowCounts(t *testing.T, store *Store) [3]int {
	t.Helper()
	ctx := context.Background()
	var counts [3]int
	for i, table := range []string{"intel_chunks", "intel_embeddings", "ontology_node_embedding_state"} {
		require.NoError(t, store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&counts[i]))
	}
	return counts
}
