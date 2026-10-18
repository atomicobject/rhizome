package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStoredLinkCandidatesRetainFoldedExactAndAmbiguousClaimants(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "note-candidates.sqlite"))
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{State: NoteMetadataState{Ready: true, LoadedAt: 1}, Notes: []NoteMetadataRow{{Path: "a/Foo.md"}, {Path: "b/foo.md"}, {Path: "notes/Cafe\u0301.md"}, {Path: "other.md"}}}))
	got, err := store.StoredNoteLinkCandidatePaths(ctx, []string{"FOO", "CAFÉ"})
	require.NoError(t, err)
	require.Equal(t, []string{"a/Foo.md", "b/foo.md", "notes/Cafe\u0301.md"}, got)
}
