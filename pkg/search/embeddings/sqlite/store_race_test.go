package sqlite

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreDimensionsConcurrentAccess(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "index.db")
	store, err := Open(indexPath, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	info := embeddings.NoteFileInfo{
		ID:    embeddings.NoteID("note"),
		Title: "note",
		Path:  "note.md",
		Mtime: time.Now(),
		Size:  1,
	}
	require.NoError(t, store.UpsertNoteMeta(ctx, info))

	embedding := embeddings.Embedding{0.1, 0.2, 0.3}
	chunks := []embeddings.ChunkInput{{
		Index: 0,
		Hash:  "chunk-hash",
		Text:  "body",
	}}
	chunkVecs := []embeddings.Embedding{embedding}

	var wg sync.WaitGroup
	errCh := make(chan error, 20)

	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			errCh <- store.UpsertNoteChunks(ctx, info.ID, chunks, chunkVecs)
		}()
		go func() {
			defer wg.Done()
			errCh <- store.CacheEmbedding(ctx, "cache-hash", embedding)
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}

	assert.Equal(t, len(embedding), store.Dimensions(), "dimensions should be set exactly once and remain stable")
}
