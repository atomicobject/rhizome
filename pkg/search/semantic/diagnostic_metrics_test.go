package semantic

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/search/embeddings/sqlite"
	"github.com/stretchr/testify/require"
)

func semanticMetricCount(c *indexingperf.Collector, name string) int64 {
	for _, counter := range c.Snapshot().Counters {
		if counter.Name == name {
			return counter.Total
		}
	}
	return 0
}

func TestNotePlanMetricsDistinguishPositionHashReuseAndNewText(t *testing.T) {
	ctx := context.Background()
	store, err := embsqlite.Open(filepath.Join(t.TempDir(), "notes.db"), 2)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()
	id := embeddings.NoteID("Notes/alpha.md")
	require.NoError(t, store.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{ID: id, Path: string(id), Title: "Alpha", Mtime: time.Now()}))
	first := embeddings.NewChunkInput(0, "first", "Alpha", "First")
	second := embeddings.NewChunkInput(1, "second", "Alpha", "Second")
	require.NoError(t, store.UpsertNoteChunks(ctx, id, []embeddings.ChunkInput{first, second}, []embeddings.Embedding{{1, 2}, {3, 4}}))
	syncer := NoteSyncer{Index: store}
	stable := indexingperf.NewBounded()
	reuse, _, missing, _, err := syncer.planChunkUpdates(indexingperf.WithCollector(ctx, stable), id, []embeddings.ChunkInput{first, second}, map[int]string{0: first.Hash, 1: second.Hash})
	require.NoError(t, err)
	require.Empty(t, reuse)
	require.Empty(t, missing)
	require.Equal(t, int64(2), semanticMetricCount(stable, "noteplan.reuse.same_position"))
	shifted := second
	shifted.Index = 2
	newChunk := embeddings.NewChunkInput(3, "new text", "Alpha", "New")
	c := indexingperf.NewBounded()
	reuse, vectors, missing, texts, err := syncer.planChunkUpdates(indexingperf.WithCollector(ctx, c), id, []embeddings.ChunkInput{first, shifted, newChunk}, map[int]string{0: first.Hash, 1: second.Hash})
	require.NoError(t, err)
	require.Equal(t, []embeddings.ChunkInput{stripChunkText(shifted)}, reuse)
	require.Equal(t, []embeddings.Embedding{{3, 4}}, vectors)
	require.Equal(t, []embeddings.ChunkInput{newChunk}, missing)
	require.Equal(t, []string{"new text"}, texts)
	require.Equal(t, int64(1), semanticMetricCount(c, "noteplan.reuse.same_position"))
	require.Equal(t, int64(1), semanticMetricCount(c, "noteplan.reuse.content_hash"))
	require.Equal(t, int64(1), semanticMetricCount(c, "noteplan.hash_cache.miss"))
	require.Equal(t, int64(1), semanticMetricCount(c, "noteplan.embed.reason.new_hash"))
}
