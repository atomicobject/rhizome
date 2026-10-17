package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/embeddings"
)

func TestDeleteNotesNotInBatchesWhenLarge(t *testing.T) {
	tmp := t.TempDir()
	store, err := OpenNotes(context.Background(), File(filepath.Join(tmp, "index.db")), Options{Access: WriteIndex, Dimensions: 0})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	const total = 1500
	const keep = 1400

	var keepIDs []embeddings.NoteID
	for i := 0; i < total; i++ {
		id := embeddings.NoteID(fmt.Sprintf("note-%04d", i))
		info := embeddings.NoteFileInfo{
			ID:    id,
			Title: string(id),
			Path:  string(id) + ".md",
			Mtime: time.Now(),
			Size:  int64(i),
		}
		require.NoError(t, store.UpsertNoteMeta(ctx, info))
		if i < keep {
			keepIDs = append(keepIDs, id)
		}
	}

	require.NoError(t, store.DeleteNotesNotIn(ctx, keepIDs))

	notes, err := store.ListNotes(ctx)
	require.NoError(t, err)
	require.Len(t, notes, keep)
	got := make(map[embeddings.NoteID]struct{}, len(notes))
	for _, note := range notes {
		got[note.ID] = struct{}{}
	}
	for _, id := range keepIDs {
		require.Contains(t, got, id)
	}
	for i := keep; i < total; i++ {
		require.NotContains(t, got, embeddings.NoteID(fmt.Sprintf("note-%04d", i)))
	}
}
