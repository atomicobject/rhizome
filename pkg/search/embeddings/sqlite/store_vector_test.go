package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

func TestSearchChunksByVector_SQLiteVecRankingAndAllowList(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "vector-search.db"), 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	now := time.Unix(1000, 0)
	require.NoError(t, store.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{
		ID:    "note-1",
		Title: "Note 1",
		Path:  "notes/note-1.md",
		Mtime: now,
		Size:  1,
	}))
	require.NoError(t, store.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{
		ID:    "note-2",
		Title: "Note 2",
		Path:  "notes/note-2.md",
		Mtime: now,
		Size:  1,
	}))

	require.NoError(t, store.UpsertNoteChunks(ctx, "note-1", []embeddings.ChunkInput{
		embeddings.NewChunkInput(0, "chunk one", "n1", "h1"),
	}, []embeddings.Embedding{{1, 0, 0, 0}}))
	require.NoError(t, store.UpsertNoteChunks(ctx, "note-2", []embeddings.ChunkInput{
		embeddings.NewChunkInput(0, "chunk two", "n2", "h2"),
	}, []embeddings.Embedding{{0, 1, 0, 0}}))

	hits, skipped, err := store.SearchChunksByVector(ctx, embeddings.Embedding{0.9, 0.1, 0, 0}, 2)
	require.NoError(t, err)
	require.Equal(t, 0, skipped)
	require.Len(t, hits, 2)
	require.Equal(t, embeddings.NoteID("note-1"), hits[0].NoteID)
	require.Greater(t, hits[0].Score, hits[1].Score)

	allowed, skipped, err := store.searchChunksByVector(ctx, embeddings.Embedding{0.9, 0.1, 0, 0}, 10, map[string]struct{}{"note-2": {}})
	require.NoError(t, err)
	require.Equal(t, 0, skipped)
	require.Len(t, allowed, 1)
	require.Equal(t, embeddings.NoteID("note-2"), allowed[0].NoteID)
}

func TestSearchFiltersStaleGenerationWhenLazyRowsPreserved(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "visible-generation.db"), 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	now := time.Unix(1000, 0)
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{Provider: "test", Model: "det", Dimensions: 4}))
	require.NoError(t, store.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{ID: "current", Title: "Fresh Topic", Path: "current.md", Mtime: now, Size: 1}))
	require.NoError(t, store.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{ID: "stale", Title: "Fresh Topic Stale", Path: "stale.md", Mtime: now, Size: 1}))
	require.NoError(t, store.UpsertNoteChunks(ctx, "current", []embeddings.ChunkInput{embeddings.NewChunkInput(0, "fresh current", "current", "h")}, []embeddings.Embedding{{1, 0, 0, 0}}))
	require.NoError(t, store.UpsertNoteChunks(ctx, "stale", []embeddings.ChunkInput{embeddings.NewChunkInput(0, "fresh stale", "stale", "h")}, []embeddings.Embedding{{0.95, 0.05, 0, 0}}))

	_, err = store.IncrementSyncGeneration(ctx)
	require.NoError(t, err)
	require.NoError(t, store.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{ID: "current", Title: "Fresh Topic", Path: "current.md", Mtime: now, Size: 1}))

	uncommittedHits, _, err := store.SearchChunksByVector(ctx, embeddings.Embedding{1, 0, 0, 0}, 10)
	require.NoError(t, err)
	require.Len(t, uncommittedHits, 2, "uncommitted generations must not hide the last visible index")

	pruned, err := store.PruneStaleNotes(ctx, 0.9)
	require.NoError(t, err)
	require.Zero(t, pruned, "test setup should lazy-preserve the stale row")
	require.NoError(t, store.CommitSyncGeneration(ctx))

	vectorHits, _, err := store.SearchChunksByVector(ctx, embeddings.Embedding{1, 0, 0, 0}, 10)
	require.NoError(t, err)
	require.Len(t, vectorHits, 1)
	require.Equal(t, embeddings.NoteID("current"), vectorHits[0].NoteID)

	textHits, err := store.SearchNotesByText(ctx, "Fresh Topic", 10)
	require.NoError(t, err)
	require.Len(t, textHits, 1)
	require.Equal(t, embeddings.NoteID("current"), textHits[0].ID)
}

func TestSearchChunksByVector_MigratesAndSyncsVecMirror(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "vec-mirror.db"), 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.EnsureSchema(ctx))
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{Provider: "test", Model: "det", Dimensions: 4}))
	vecTable := chunkVecTableName(4)
	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, vecTable).Scan(&count))
	require.Zero(t, count, "metadata validation must leave vector mirror lazy")

	now := time.Unix(1000, 0)
	require.NoError(t, store.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{
		ID:    "note-1",
		Title: "Note 1",
		Path:  "notes/note-1.md",
		Mtime: now,
		Size:  1,
	}))
	require.NoError(t, store.UpsertNoteChunks(ctx, "note-1", []embeddings.ChunkInput{
		embeddings.NewChunkInput(0, "chunk one", "n1", "h1"),
	}, []embeddings.Embedding{{1, 0, 0, 0}}))

	_, _, err = store.SearchChunksByVector(ctx, embeddings.Embedding{1, 0, 0, 0}, 5)
	require.NoError(t, err)

	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+vecTable).Scan(&count))
	require.Equal(t, 1, count)

	require.NoError(t, store.UpsertNoteChunks(ctx, "note-1", []embeddings.ChunkInput{
		embeddings.NewChunkInput(0, "chunk one", "n1", "h1"),
		embeddings.NewChunkInput(1, "chunk two", "n1", "h2"),
	}, []embeddings.Embedding{{1, 0, 0, 0}, {0, 1, 0, 0}}))
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+vecTable).Scan(&count))
	require.Equal(t, 2, count)
}

func TestSearchChunksByVector_DoesNotDependOnEmbeddingCache(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "vec-cache-independent.db"), 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	now := time.Unix(1000, 0)
	require.NoError(t, store.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{
		ID:    "note-1",
		Title: "Note 1",
		Path:  "notes/note-1.md",
		Mtime: now,
		Size:  1,
	}))
	require.NoError(t, store.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{
		ID:    "note-2",
		Title: "Note 2",
		Path:  "notes/note-2.md",
		Mtime: now,
		Size:  1,
	}))
	require.NoError(t, store.UpsertNoteChunks(ctx, "note-1", []embeddings.ChunkInput{
		embeddings.NewChunkInput(0, "chunk one", "n1", "h1"),
	}, []embeddings.Embedding{{1, 0, 0, 0}}))
	require.NoError(t, store.UpsertNoteChunks(ctx, "note-2", []embeddings.ChunkInput{
		embeddings.NewChunkInput(0, "chunk two", "n2", "h2"),
	}, []embeddings.Embedding{{0, 1, 0, 0}}))

	_, err = store.db.ExecContext(ctx, `DELETE FROM `+tableEmbeddingCache)
	require.NoError(t, err)

	hits, skipped, err := store.SearchChunksByVector(ctx, embeddings.Embedding{0.95, 0.05, 0, 0}, 2)
	require.NoError(t, err)
	require.Equal(t, 0, skipped)
	require.Len(t, hits, 2)
	require.Equal(t, embeddings.NoteID("note-1"), hits[0].NoteID)
	require.Greater(t, hits[0].Score, hits[1].Score)
}

func TestSearchChunksByVector_AfterRebuild_RecreatesVecMirrorLazily(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "vec-rebuild.db"), 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	now := time.Unix(1000, 0)
	require.NoError(t, store.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{
		ID:    "note-1",
		Title: "Note 1",
		Path:  "notes/note-1.md",
		Mtime: now,
		Size:  1,
	}))
	require.NoError(t, store.UpsertNoteChunks(ctx, "note-1", []embeddings.ChunkInput{
		embeddings.NewChunkInput(0, "chunk one", "n1", "h1"),
	}, []embeddings.Embedding{{1, 0, 0, 0}}))

	require.NoError(t, store.rebuildDomainPreservingEmbeddings(ctx))

	vecTable := chunkVecTableName(4)
	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table' AND name = ?
	`, vecTable).Scan(&count))
	require.Equal(t, 0, count, "rebuild should not eagerly create vec mirrors")

	hits, skipped, err := store.SearchChunksByVector(ctx, embeddings.Embedding{0.95, 0.05, 0, 0}, 1)
	require.NoError(t, err)
	require.Equal(t, 0, skipped)
	require.Len(t, hits, 1)
	require.Equal(t, embeddings.NoteID("note-1"), hits[0].NoteID)

	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+vecTable).Scan(&count))
	require.Equal(t, 1, count)
}

func TestSyncNoteChunksBatchUpdatesAndDeletesStaleRows(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "note-sync.db"), 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	now := time.Unix(1000, 0)
	require.NoError(t, store.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{
		ID:    "note-1",
		Title: "Note 1",
		Path:  "notes/note-1.md",
		Mtime: now,
		Size:  1,
	}))

	require.NoError(t, store.UpsertNoteChunks(ctx, "note-1", []embeddings.ChunkInput{
		embeddings.NewChunkInput(0, "chunk zero", "n1", "h0"),
		embeddings.NewChunkInput(1, "chunk one", "n1", "h1"),
	}, []embeddings.Embedding{{1, 0, 0, 0}, {0, 1, 0, 0}}))

	require.NoError(t, store.SyncNoteChunksBatch(ctx, []embeddings.NoteChunkSync{{
		NoteID: "note-1",
		Chunks: []embeddings.ChunkInput{
			embeddings.NewChunkInput(1, "chunk one updated", "n1", "h1b"),
			embeddings.NewChunkInput(2, "chunk two", "n1", "h2"),
		},
		Embeddings:  []embeddings.Embedding{{0, 1, 1, 0}, {0, 0, 1, 1}},
		KeepIndices: []int{1, 2},
	}}))

	chunks, err := store.NoteChunks(ctx, "note-1")
	require.NoError(t, err)
	require.Len(t, chunks, 2)
	require.Equal(t, 1, chunks[0].Index)
	require.Equal(t, 2, chunks[1].Index)
	require.Equal(t, embeddings.Embedding{0, 1, 1, 0}, chunks[0].Embedding)
	require.Equal(t, embeddings.Embedding{0, 0, 1, 1}, chunks[1].Embedding)
	require.Equal(t, "h1b", chunks[0].Heading)
	require.Equal(t, "h2", chunks[1].Heading)
}

func TestSyncNoteChunksBatchDeletesAllRowsWhenKeepIndicesEmpty(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "note-sync-empty.db"), 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	now := time.Unix(1000, 0)
	require.NoError(t, store.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{
		ID:    "note-1",
		Title: "Note 1",
		Path:  "notes/note-1.md",
		Mtime: now,
		Size:  1,
	}))
	require.NoError(t, store.UpsertNoteChunks(ctx, "note-1", []embeddings.ChunkInput{
		embeddings.NewChunkInput(0, "chunk zero", "n1", "h0"),
	}, []embeddings.Embedding{{1, 0, 0, 0}}))

	require.NoError(t, store.SyncNoteChunksBatch(ctx, []embeddings.NoteChunkSync{{
		NoteID:      "note-1",
		KeepIndices: nil,
	}}))

	chunks, err := store.NoteChunks(ctx, "note-1")
	require.NoError(t, err)
	require.Len(t, chunks, 0)
}

func TestSyncNoteChunksBatchRequiresExistingNoteMetadata(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "note-sync-missing.db"), 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	err = store.SyncNoteChunksBatch(ctx, []embeddings.NoteChunkSync{{
		NoteID: "missing-note",
		Chunks: []embeddings.ChunkInput{
			embeddings.NewChunkInput(0, "chunk zero", "n1", "h0"),
		},
		Embeddings:  []embeddings.Embedding{{1, 0, 0, 0}},
		KeepIndices: []int{0},
	}})
	require.Error(t, err)
	require.ErrorContains(t, err, "note metadata missing for missing-note")
}

func TestSyncNoteChunksBatchPreservesExistingCacheRows(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "note-sync-cache.db"), 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	now := time.Unix(1000, 0)
	require.NoError(t, store.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{
		ID:    "note-1",
		Title: "Note 1",
		Path:  "notes/note-1.md",
		Mtime: now,
		Size:  1,
	}))

	require.NoError(t, store.CacheEmbedding(ctx, "h0", embeddings.Embedding{1, 0, 0, 0}))

	var createdBefore int64
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT created_at FROM `+tableEmbeddingCache+` WHERE content_hash = ?
	`, "h0").Scan(&createdBefore))

	require.NoError(t, store.SyncNoteChunksBatch(ctx, []embeddings.NoteChunkSync{{
		NoteID: "note-1",
		Chunks: []embeddings.ChunkInput{
			embeddings.NewChunkInput(0, "chunk zero", "n1", "h0"),
		},
		Embeddings:  []embeddings.Embedding{{9, 9, 9, 9}},
		KeepIndices: []int{0},
	}}))

	var createdAfter int64
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT created_at FROM `+tableEmbeddingCache+` WHERE content_hash = ?
	`, "h0").Scan(&createdAfter))
	require.Equal(t, createdBefore, createdAfter)

	cached, ok, err := store.EmbeddingByHash(ctx, "h0")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, embeddings.Embedding{1, 0, 0, 0}, cached)
}
