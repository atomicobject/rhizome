package semantic

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"path/filepath"
	"runtime"
	"testing"
	"time"

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

func TestAggregateIntelChunkMatchesReturnsWhenEveryWorkerFails(t *testing.T) {
	intelStore, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	// Hold the only connection so every worker parks inside SearchEmbeddings
	// with a chunk in hand while the producer fills the work buffer.
	db := intelStore.DB()
	db.SetMaxOpenConns(1)
	held, err := db.Conn(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = held.Close() })

	workers := runtime.GOMAXPROCS(0)
	chunks := make([]embeddings.StoredChunk, 4*workers+4)
	for i := range chunks {
		chunks[i] = embeddings.StoredChunk{Index: i, Embedding: embeddings.Embedding{1, 0, 0, 0}}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, _, err := AggregateIntelChunkMatches(ctx, intelStore, chunks, 10, "", 3, 0, false, 0, nil)
		done <- err
	}()

	require.Eventually(t, func() bool { return db.Stats().WaitCount >= int64(workers) }, 5*time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond) // let the producer reach its send on the full buffer
	cancel()                          // every worker's SearchEmbeddings now fails and the worker exits

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("AggregateIntelChunkMatches deadlocked after every worker failed")
	}
}
