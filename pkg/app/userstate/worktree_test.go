package userstate

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/stretchr/testify/require"
)

func TestSeedWorktreePreservesWALPreferencesAndIndependentChanges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sourceRoot, targetRoot := t.TempDir(), t.TempDir()
	source := openTestStore(t, sourceRoot)
	scope := testScope("work")
	original, err := source.Patch(ctx, scope, 0, map[string]json.RawMessage{"collapsed": json.RawMessage(`true`)}, nil)
	require.NoError(t, err)
	resetScope := testScope("reset")
	reset, err := source.Reset(ctx, resetScope, 0)
	require.NoError(t, err)
	_, claimedBefore, err := source.ImportIfAbsent(ctx, resetScope, "legacy:claimed", nil)
	require.NoError(t, err)
	require.False(t, claimedBefore)
	seeded, err := SeedWorktree(ctx, sourceRoot, targetRoot)
	require.NoError(t, err)
	require.True(t, seeded)
	target := openTestStore(t, targetRoot)
	copied, err := target.Read(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, original, copied)
	copiedReset, err := target.Read(ctx, resetScope)
	require.NoError(t, err)
	require.Equal(t, reset, copiedReset)
	imported, claimed, err := target.ImportIfAbsent(ctx, resetScope, "legacy", map[string]json.RawMessage{"collapsed": json.RawMessage(`false`)})
	require.NoError(t, err)
	require.False(t, claimed)
	require.Empty(t, imported.Values)
	_, claimedElsewhere, err := target.ImportIfAbsent(ctx, testScope("another"), "legacy:claimed", map[string]json.RawMessage{"density": json.RawMessage(`"old"`)})
	require.NoError(t, err)
	require.False(t, claimedElsewhere, "copied migration claims cannot be reused by another instance")
	_, err = target.Patch(ctx, scope, copied.Revision, map[string]json.RawMessage{"collapsed": json.RawMessage(`false`)}, nil)
	require.NoError(t, err)
	unchanged, err := source.Read(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, original, unchanged)
	_, err = source.Patch(ctx, scope, original.Revision, map[string]json.RawMessage{"density": json.RawMessage(`"compact"`)}, nil)
	require.NoError(t, err)
	independent, err := target.Read(ctx, scope)
	require.NoError(t, err)
	require.JSONEq(t, `false`, string(independent.Values["collapsed"]))
	require.NotContains(t, independent.Values, "density")
	seeded, err = SeedWorktree(ctx, sourceRoot, targetRoot)
	require.NoError(t, err)
	require.False(t, seeded)
	preserved, err := target.Read(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, independent, preserved)
}

func TestSeedWorktreeSkipsMissingSourceAndExistingTarget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sourceRoot, targetRoot := t.TempDir(), t.TempDir()
	seeded, err := SeedWorktree(ctx, sourceRoot, targetRoot)
	require.NoError(t, err)
	require.False(t, seeded)
	_, err = os.Stat(databasePath(targetRoot))
	require.ErrorIs(t, err, os.ErrNotExist)
	source := openTestStore(t, sourceRoot)
	_, err = source.Patch(ctx, testScope("work"), 0, map[string]json.RawMessage{"density": json.RawMessage(`"compact"`)}, nil)
	require.NoError(t, err)
	target := openTestStore(t, targetRoot)
	seeded, err = SeedWorktree(ctx, sourceRoot, targetRoot)
	require.NoError(t, err)
	require.False(t, seeded)
	snapshot, err := target.Read(ctx, testScope("work"))
	require.NoError(t, err)
	require.Empty(t, snapshot.Values)
	seeded, err = SeedWorktree(ctx, sourceRoot, sourceRoot)
	require.NoError(t, err)
	require.False(t, seeded)
}

func TestSeedWorktreeWaitsForInitializationAndHonorsCancellation(t *testing.T) {
	t.Parallel()
	sourceRoot, targetRoot := t.TempDir(), t.TempDir()
	openTestStore(t, sourceRoot)
	release, err := sqliteutil.LockSchemaInit(context.Background(), databasePath(targetRoot))
	require.NoError(t, err)
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	seeded, err := SeedWorktree(ctx, sourceRoot, targetRoot)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.False(t, seeded)
	_, err = os.Stat(databasePath(targetRoot))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestSeedWorktreeFailedSnapshotLeavesNoDestination(t *testing.T) {
	t.Parallel()
	sourceRoot, targetRoot := t.TempDir(), t.TempDir()
	source := openTestStore(t, sourceRoot)
	require.NoError(t, source.Close())
	require.NoError(t, os.WriteFile(databasePath(sourceRoot), []byte("invalid SQLite"), 0600))
	seeded, err := SeedWorktree(context.Background(), sourceRoot, targetRoot)
	require.Error(t, err)
	require.False(t, seeded)
	_, err = os.Stat(databasePath(targetRoot))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestSeedWorktreeFallsBackWhileSourceHasUncommittedChanges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sourceRoot, targetRoot := t.TempDir(), t.TempDir()
	source := openTestStore(t, sourceRoot)
	scope := testScope("work")
	committed, err := source.Patch(ctx, scope, 0, map[string]json.RawMessage{"density": json.RawMessage(`"compact"`)}, nil)
	require.NoError(t, err)
	tx, err := source.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE view_preferences SET value_json='"uncommitted"'`)
	require.NoError(t, err)
	// The held write lock prevents CloneInto; SnapshotInto can read committed state.
	seeded, err := SeedWorktree(ctx, sourceRoot, targetRoot)
	require.NoError(t, err)
	require.True(t, seeded)
	target := openTestStore(t, targetRoot)
	copied, err := target.Read(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, committed, copied)
}
