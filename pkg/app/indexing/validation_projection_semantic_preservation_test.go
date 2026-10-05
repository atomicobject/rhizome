package indexing

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRefreshValidationProjectionSchemaRefreshPreservesOntologySemanticRows(t *testing.T) {
	ctx := context.Background()
	root := writeValidationProjectionVault(t)
	request := ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
	}
	initial, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	nodeID, chunkID := seedValidationProjectionOntologySemanticRows(t, initial)
	require.NoError(t, initial.Close())

	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type ExampleNote @node(paths: ["notes/*.md"]) {
  name: String!
  summary: String
}
`), 0o644))
	request.ExactPaths = &ValidationProjectionPaths{Changed: []paths.NotePath{"notes/one.md"}}
	refreshed, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	defer func() { _ = refreshed.Close() }()

	assertValidationProjectionOntologySemanticRows(t, refreshed, nodeID, chunkID)
}

func TestRefreshValidationProjectionDeletePreservesOntologySemanticRows(t *testing.T) {
	ctx := context.Background()
	root := writeValidationProjectionVault(t)
	request := ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
	}
	initial, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	nodeID, chunkID := seedValidationProjectionOntologySemanticRows(t, initial)
	require.NoError(t, initial.Close())

	require.NoError(t, os.Remove(filepath.Join(root, "notes", "one.md")))
	request.ExactPaths = &ValidationProjectionPaths{Deleted: []paths.NotePath{"notes/one.md"}}
	refreshed, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	defer func() { _ = refreshed.Close() }()

	assertValidationProjectionOntologySemanticRows(t, refreshed, nodeID, chunkID)
	nodes, err := refreshed.Runtime.Store.OntologyNodesByIDs(ctx, []string{nodeID})
	require.NoError(t, err)
	require.Empty(t, nodes, "validation catalog deletion must still land")
}

func seedValidationProjectionOntologySemanticRows(t *testing.T, result *ValidationProjectionResult) (string, string) {
	t.Helper()
	ctx := context.Background()
	nodes, err := result.Runtime.Store.OntologyNodesByPaths(ctx, []string{"notes/one.md"})
	require.NoError(t, err)
	require.NotEmpty(t, nodes)
	node := nodes[0]
	chunkID := "validation-projection-node-chunk"
	require.NoError(t, result.Runtime.Store.ReplaceIntelChunks(ctx, []string{node.NodeID}, []codeanchor.IntelChunk{{
		ChunkID:     chunkID,
		OwnerID:     node.NodeID,
		OwnerType:   "ontology_node",
		Ord:         0,
		Granularity: "node_body",
		ContentHash: "chunk-hash",
		StartByte:   node.StartByte,
		EndByte:     node.EndByte,
		UpdatedAt:   17,
	}}))
	require.NoError(t, result.Runtime.Store.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{
		chunkID: {1, 0, 0},
	}))
	require.NoError(t, result.Runtime.Store.UpsertOntologyNodeEmbeddingStates(ctx, []codeanchor.IntelOntologyNodeEmbeddingState{{
		ChunkID:                  chunkID,
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
		UpdatedAt:                19,
	}}))
	return node.NodeID, chunkID
}

func assertValidationProjectionOntologySemanticRows(t *testing.T, result *ValidationProjectionResult, nodeID, chunkID string) {
	t.Helper()
	ctx := context.Background()
	chunks, err := result.Runtime.Store.IntelChunksByOwners(ctx, []string{nodeID})
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	require.Equal(t, chunkID, chunks[0].ChunkID)
	states, err := result.Runtime.Store.OntologyNodeEmbeddingStatesByChunkIDs(ctx, []string{chunkID})
	require.NoError(t, err)
	require.Contains(t, states, chunkID)
	var embeddingCount int
	require.NoError(t, result.Runtime.Store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_embeddings WHERE chunk_id = ?`, chunkID).Scan(&embeddingCount))
	require.Equal(t, 1, embeddingCount)
}
