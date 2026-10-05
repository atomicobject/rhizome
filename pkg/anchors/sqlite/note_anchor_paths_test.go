package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNotePathsByAnchorIDsBatchesLargeLookups(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "test.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// SQLite's default compiled variable ceiling is 32,766. Validation may
	// discover more invalid anchors than that in one pass, so the store must
	// keep each query beneath the repository's stricter batching ceiling.
	anchorIDs := make([]int64, 32_767)
	for i := range anchorIDs {
		anchorIDs[i] = int64(i + 1)
	}

	paths, err := store.NotePathsByAnchorIDs(ctx, anchorIDs)
	require.NoError(t, err)
	require.Empty(t, paths)
}
