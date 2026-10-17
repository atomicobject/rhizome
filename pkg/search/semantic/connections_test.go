package semantic

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

func TestAggregateIntelChunkMatchesIncludesOntologyNodes(t *testing.T) {
	ctx := context.Background()
	intelStore, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	node := readmodel.NodeRow{
		NodeID:      "node:typed",
		NotePath:    "docs/typed.md",
		NodeRefJSON: `{"notePath":"docs/typed.md","nodeId":"typed","typeName":"ReferenceDoc"}`,
		NodeKind:    "ROOT",
		TypeName:    "ReferenceDoc",
		Title:       "Typed",
	}
	require.NoError(t, intelStore.ReplaceOntologyNodes(ctx, []string{node.NotePath}, []readmodel.NodeRow{node}))
	require.NoError(t, intelStore.ReplaceIntelChunks(ctx, []string{node.NodeID}, []codeanchor.IntelChunk{{
		ChunkID:     "node-chunk",
		OwnerID:     node.NodeID,
		OwnerType:   "ontology_node",
		Ord:         1,
		Granularity: embeddings.GranularityOntologyNodeBody,
		Breadcrumb:  "Typed > Body",
		Heading:     "Typed",
		ContentHash: "hash",
		UpdatedAt:   1,
	}}))
	vec := embeddings.Embedding{1, 0, 0, 0}
	require.NoError(t, intelStore.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{"node-chunk": vec}))

	agg, _, err := AggregateIntelChunkMatches(ctx, intelStore, []embeddings.StoredChunk{{Index: 0, Embedding: vec}}, 10, "", 3, 32, true, 3, nil)
	require.NoError(t, err)
	require.Contains(t, agg, embeddings.NoteID(node.NotePath))
	require.Equal(t, 1, agg[embeddings.NoteID(node.NotePath)].TopHits[0].Match.ChunkIndex)
}
