package indexlock

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func holdPriorityGuard(t *testing.T, lockPath string) func() {
	t.Helper()
	guard, err := os.OpenFile(PriorityPath(lockPath)+".guard.lock", os.O_CREATE|os.O_RDWR, 0o600)
	require.NoError(t, err)
	require.NoError(t, tryLockGuardFile(guard))
	unlock := sync.OnceFunc(func() {
		require.NoError(t, unlockGuardFile(guard))
		require.NoError(t, guard.Close())
	})
	t.Cleanup(unlock)
	return unlock
}

func TestPriorityCheckReadsRecordsWhenTheGuardIsBusy(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "index.lock")
	cleanup, err := RequestPriority(lockPath)
	require.NoError(t, err)
	t.Cleanup(cleanup.Close)
	path := onlyPriorityRequestPath(t, lockPath)
	unlock := holdPriorityGuard(t, lockPath)
	require.True(t, CheckPriority(lockPath), "a complete published request remains visible")
	old := time.Now().Add(-PriorityStaleAfter - time.Second)
	require.NoError(t, os.Chtimes(path, old, old))
	require.False(t, CheckPriority(lockPath), "stale records do not invent priority")
	require.FileExists(t, path, "busy scans must not prune against an in-flight refresh")
	cleanup.Close()
	require.NoFileExists(t, path, "cleanup must not wait for the scanning guard")
	require.False(t, CheckPriority(lockPath), "an empty busy directory has no waiter")
	unlock()
}

func TestRequestPriorityPublishesWhenTheGuardIsBusy(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "index.lock")
	unlock := holdPriorityGuard(t, lockPath)
	cleanup, err := RequestPriority(lockPath)
	require.NoError(t, err, "unrelated refresh or pruning cannot reject a new waiter")
	t.Cleanup(cleanup.Close)
	require.True(t, CheckPriority(lockPath))
	unlock()
	require.True(t, CheckPriority(lockPath))
	cleanup.Close()
	require.False(t, CheckPriority(lockPath))
}

func TestPriorityPruningPreservesUnrelatedFiles(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "index.lock")
	dir := PriorityPath(lockPath)
	require.NoError(t, os.Mkdir(dir, 0o755))
	old := time.Now().Add(-PriorityStaleAfter - time.Second)
	for _, name := range []string{"notes.md", "unrelated.json", ".unrelated.tmp"} {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte("keep this file"), 0o644))
		require.NoError(t, os.Chtimes(path, old, old))
	}
	require.False(t, CheckPriority(lockPath))
	for _, name := range []string{"notes.md", "unrelated.json", ".unrelated.tmp"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		require.Equal(t, "keep this file", string(raw))
	}
}

func TestPriorityDirectoryRejectsLegacyFilesAndSymlinks(t *testing.T) {
	for _, kind := range []string{"legacy-file", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			lockPath := filepath.Join(root, "index.lock")
			priorityDir := PriorityPath(lockPath)
			if kind == "legacy-file" {
				require.NoError(t, os.WriteFile(priorityDir, []byte("legacy marker"), 0o644))
			} else {
				target := filepath.Join(root, "unrelated")
				require.NoError(t, os.Mkdir(target, 0o755))
				note := filepath.Join(target, "notes.md")
				require.NoError(t, os.WriteFile(note, []byte("keep this note"), 0o644))
				old := time.Now().Add(-PriorityStaleAfter - time.Second)
				require.NoError(t, os.Chtimes(note, old, old))
				if err := os.Symlink(target, priorityDir); err != nil && runtime.GOOS == "windows" {
					t.Skipf("symlink unavailable: %v", err)
				} else {
					require.NoError(t, err)
				}
				t.Cleanup(func() { require.FileExists(t, note) })
			}
			require.False(t, CheckPriority(lockPath))
			cleanup, err := RequestPriority(lockPath)
			if cleanup != nil {
				t.Cleanup(cleanup.Close)
			}
			require.Nil(t, cleanup)
			require.ErrorIs(t, err, syscall.ENOTDIR)
			if kind == "legacy-file" {
				raw, err := os.ReadFile(priorityDir)
				require.NoError(t, err)
				require.Equal(t, "legacy marker", string(raw))
			}
		})
	}
}
