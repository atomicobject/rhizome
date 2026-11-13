package embedindex

import (
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/stretchr/testify/require"
)

func TestNoteDerivedCleanupKeepsDroppedMembers(t *testing.T) {
	index, err := embsqlite.OpenNotes(t.Context(), embsqlite.File(filepath.Join(t.TempDir(), "notes.db")), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: 8})
	require.NoError(t, err)
	defer index.Close()
	for _, id := range []embeddings.NoteID{"deleted.md", "superseded.md"} {
		require.NoError(t, index.UpsertNoteMeta(t.Context(), embeddings.NoteFileInfo{ID: id, Path: string(id), Title: string(id)}))
	}
	s := NoteSyncer{Index: index}
	plan := NotePlan{state: notePlanState{incremental: true, deleteIDs: []embeddings.NoteID{"deleted.md", "superseded.md"}}}
	require.NoError(t, s.FinishDerived(t.Context(), plan, []string{"deleted.md"}))
	notes, err := index.ListNotes(t.Context())
	require.NoError(t, err)
	require.Len(t, notes, 1)
	require.Equal(t, embeddings.NoteID("superseded.md"), notes[0].ID)
}
