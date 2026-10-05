package serve

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

func TestDatabaseGuardFailsJobsOnceTheFileIsReplaced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "db.sqlite")
	var reasons []string
	guard := &databaseGuard{path: path, shutdown: func(reason string) { reasons = append(reasons, reason) }}

	require.NoError(t, guard.preflight(lane.KindBootCatchUp), "no database yet is not a replacement")
	require.NoError(t, os.WriteFile(path, []byte("one"), 0o644))
	require.NoError(t, guard.preflight(lane.KindBootCatchUp), "first sighting records the identity")
	require.NoError(t, os.WriteFile(path, []byte("one-updated-in-place"), 0o644))
	require.NoError(t, guard.preflight(lane.KindWatcherBatch), "in-place writes keep the inode")

	require.NoError(t, os.Rename(path, path+".previous"))
	require.NoError(t, os.WriteFile(path, []byte("two"), 0o644))
	require.ErrorIs(t, guard.preflight(lane.KindExplicitIndex), lane.ErrDatabaseReplaced)
	require.ErrorIs(t, guard.preflight(lane.KindWatcherBatch), lane.ErrDatabaseReplaced)
	require.Len(t, reasons, 1, "shutdown is requested once")

	require.NoError(t, os.Remove(path))
	require.ErrorIs(t, guard.preflight(lane.KindWatcherBatch), lane.ErrDatabaseReplaced, "a deleted database is gone too")
}

func TestDatabaseGuardNamesAMissingVaultRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "vault")
	path := filepath.Join(root, "db.sqlite")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(path, []byte("one"), 0o644))
	var reasons []string
	guard := &databaseGuard{path: path, vaultRoot: root, shutdown: func(reason string) { reasons = append(reasons, reason) }}
	require.NoError(t, guard.preflight(lane.KindBootCatchUp))

	require.NoError(t, os.RemoveAll(root))
	require.ErrorIs(t, guard.preflight(lane.KindWatcherBatch), lane.ErrDatabaseReplaced)
	require.Equal(t, []string{"vault root " + root + " is gone"}, reasons)
}

func TestDatabaseGuardRejectsReplacementWhileJobWaitsForWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "db.sqlite")
	require.NoError(t, os.WriteFile(path, []byte("original"), 0o600))
	opened, err := os.Stat(path)
	require.NoError(t, err)
	var reasons []string
	guard := &databaseGuard{path: path, opened: opened, shutdown: func(reason string) { reasons = append(reasons, reason) }}

	lockPath := filepath.Join(dir, "index.lock")
	release, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	defer func() { _ = release() }()
	executor := lane.New(lane.Options{LockPath: lockPath})
	defer executor.Close()
	checked := make(chan struct{})
	var first sync.Once
	executor.SetPreflight(func(kind lane.Kind) error {
		err := guard.preflight(kind)
		first.Do(func() { close(checked) })
		return err
	})
	ran := false
	handle, _, err := executor.Submit(context.Background(), lane.Request{Kind: lane.KindExplicitIndex, Run: func(context.Context, lane.Reporter) error {
		ran = true
		return nil
	}})
	require.NoError(t, err)
	select {
	case <-checked:
	case <-time.After(5 * time.Second):
		t.Fatal("job never reached its initial database check")
	}
	// A separate writer replaces the database after the early check, while
	// this job still cannot acquire the writer lease.
	require.NoError(t, os.Rename(path, path+".previous"))
	require.NoError(t, os.WriteFile(path, []byte("replacement"), 0o600))
	require.NoError(t, release())
	select {
	case <-handle.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("job did not finish after writer released its lease")
	}
	require.ErrorIs(t, handle.Err(), lane.ErrDatabaseReplaced)
	require.False(t, ran, "job must not write through handles for the replaced database")
	require.Len(t, reasons, 1)
	releaseAgain, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired, "failed preflight must release the writer lease")
	require.NoError(t, releaseAgain())
}
