package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNotePathsByPathPrefixIsOrderedAndBounded(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "notes.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "notes", LoadedAt: 42, Ready: true},
		Notes: []NoteMetadataRow{
			{Path: "pkg/app/mcp/Zeta.md"},
			{Path: "pkg/app/mcp/Alpha.md"},
			{Path: "pkg/app/mcp/nested/Beta.md"},
			{Path: "pkg/app/other/Outside.md"},
			{Path: "pkg/app/m_p/Inside.md"},
			{Path: "pkg/app/map/Outside.md"},
		},
	}))

	paths, err := store.NotePathsByPathPrefix(ctx, "pkg/app/mcp/", 2)
	require.NoError(t, err)
	require.Equal(t, []string{"pkg/app/mcp/Alpha.md", "pkg/app/mcp/Zeta.md"}, paths)

	paths, err = store.NotePathsByPathPrefix(ctx, "pkg/app/m_p", 10)
	require.NoError(t, err)
	require.Equal(t, []string{"pkg/app/m_p/Inside.md"}, paths)
}

func TestNotePathsByPathPrefixRejectsEmptyPrefix(t *testing.T) {
	store, err := Open(currentSchemaTestDBPath(t, "notes.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	paths, err := store.NotePathsByPathPrefix(context.Background(), "", 60)
	require.NoError(t, err)
	require.Empty(t, paths)
}
