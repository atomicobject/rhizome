package sqlite

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

func TestNoteChunkWritesRollBackOnDimensionMismatch(t *testing.T) {
	for _, mode := range []string{"single", "batch", "sync"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			store, err := Open(t.TempDir()+"/notes.db", 4)
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })
			require.NoError(t, store.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{ID: "note", Title: "Note", Path: "note.md"}))
			original := []embeddings.ChunkInput{embeddings.NewChunkInput(0, "original", "Note", "Heading")}
			require.NoError(t, store.UpsertNoteChunks(ctx, "note", original, []embeddings.Embedding{{1, 0, 0, 0}}))
			before, err := store.NoteChunks(ctx, "note")
			require.NoError(t, err)

			chunks := []embeddings.ChunkInput{
				embeddings.NewChunkInput(1, "must roll back", "Note", "First"),
				embeddings.NewChunkInput(2, "wrong dimensions", "Note", "Second"),
			}
			vectors := []embeddings.Embedding{{0, 1, 0, 0}, {1, 0}}
			switch mode {
			case "single":
				err = store.UpsertNoteChunks(ctx, "note", chunks, vectors)
			case "batch":
				err = store.UpsertNoteChunksBatch(ctx, []embeddings.NoteChunksUpsert{
					{NoteID: "note", Chunks: chunks[:1], Embeddings: vectors[:1]},
					{NoteID: "note", Chunks: chunks[1:], Embeddings: vectors[1:]},
				})
			case "sync":
				err = store.SyncNoteChunksBatch(ctx, []embeddings.NoteChunkSync{
					{NoteID: "note", Chunks: chunks[:1], Embeddings: vectors[:1], KeepIndices: []int{1}},
					{NoteID: "note", Chunks: chunks[1:], Embeddings: vectors[1:], KeepIndices: []int{2}},
				})
			}
			require.EqualError(t, err, "chunk dimension mismatch: have 2 want 4")
			after, err := store.NoteChunks(ctx, "note")
			require.NoError(t, err)
			require.Equal(t, before, after, "a later invalid item must undo both writes and stale-row deletion")
			_, found, err := store.EmbeddingByHash(ctx, chunks[0].Hash)
			require.NoError(t, err)
			require.False(t, found, "failed writes must not leak cache entries")
			require.Equal(t, 4, store.Dimensions())
		})
	}
}
