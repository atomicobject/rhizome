package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func writeLegacyLock(t *testing.T, vaultPath string, pid int) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, ".rhizome", "watcher.lock"), []byte(fmt.Sprintf(`{"pid":%d,"host":"x"}`, pid)), 0o644))
}

func TestLegacyOwnerSeesOnlyLiveProcesses(t *testing.T) {
	vaultPath := t.TempDir()
	_, live := LegacyOwner(vaultPath)
	require.False(t, live, "no legacy lock")

	writeLegacyLock(t, vaultPath, os.Getpid())
	pid, live := LegacyOwner(vaultPath)
	require.True(t, live)
	require.Equal(t, os.Getpid(), pid)
	require.ErrorIs(t, LegacyOwnerError(vaultPath), ErrLegacyRuntime)

	writeLegacyLock(t, vaultPath, 2147483000)
	_, live = LegacyOwner(vaultPath)
	require.False(t, live, "dead pid is not an owner")
	require.NoError(t, LegacyOwnerError(vaultPath))

	// A reused PID cannot pass for the old serve once its heartbeat is stale.
	writeLegacyLock(t, vaultPath, os.Getpid())
	stale := time.Now().Add(-10 * time.Minute)
	require.NoError(t, os.Chtimes(filepath.Join(vaultPath, ".rhizome", "watcher.lock"), stale, stale))
	_, live = LegacyOwner(vaultPath)
	require.False(t, live, "a lock without a recent heartbeat is not an owner")
}

func TestRemoveLegacyFilesKeepsALiveOwnersLock(t *testing.T) {
	vaultPath := t.TempDir()
	writeLegacyLock(t, vaultPath, os.Getpid())
	dir := filepath.Join(vaultPath, ".rhizome")
	for _, name := range []string{"serve-dev.json", "cache.dirty.log", "cache.dirty.log.1"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644))
	}
	RemoveLegacyFiles(vaultPath)
	require.FileExists(t, filepath.Join(dir, "watcher.lock"), "a live legacy owner keeps its lock")
	for _, name := range []string{"serve-dev.json", "cache.dirty.log", "cache.dirty.log.1"} {
		require.NoFileExists(t, filepath.Join(dir, name))
	}

	writeLegacyLock(t, vaultPath, 2147483000)
	RemoveLegacyFiles(vaultPath)
	require.NoFileExists(t, filepath.Join(dir, "watcher.lock"))
}

func TestEnsureRefusesToSpawnBesideALegacyRuntime(t *testing.T) {
	vaultPath := t.TempDir()
	writeLegacyLock(t, vaultPath, os.Getpid())
	_, err := Ensure(context.Background(), EnsureOptions{VaultPath: vaultPath, Autostart: true, Wait: true})
	require.ErrorIs(t, err, ErrLegacyRuntime)
	require.NoFileExists(t, SpawnLockPath(vaultPath), "no spawn lease is taken")
}

func TestTerminateHeadlessOnlySignalsARuntimeThatStillHoldsTheLock(t *testing.T) {
	vaultPath := t.TempDir()
	self := InstanceManifest{PID: os.Getpid(), Mode: ModeHeadless, VaultPath: vaultPath}
	// No runtime lock names this PID: the process is gone (or reused), so no
	// signal is sent. If it were, this test process would die.
	require.NoError(t, TerminateHeadless(vaultPath, self))
	require.ErrorContains(t, TerminateHeadless(vaultPath, InstanceManifest{PID: os.Getpid(), Mode: ModeAttached}), "only headless runtimes are terminated", "attached runtimes are never terminated")
}

func TestLockOwnerAndExitingDetection(t *testing.T) {
	vaultPath := t.TempDir()
	_, live := LockOwner(vaultPath)
	require.False(t, live, "no lock, no owner")
	require.False(t, OwnerIsExiting(vaultPath))

	// A live owner with no manifest is a runtime between closing its listener
	// and releasing the lock, except when the owner is this very process.
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(LockPath(vaultPath), []byte(fmt.Sprintf(`{"pid":%d}`, os.Getpid())), 0o644))
	pid, live := LockOwner(vaultPath)
	require.True(t, live)
	require.Equal(t, os.Getpid(), pid)
	require.False(t, OwnerIsExiting(vaultPath), "our own lock is never 'exiting'")

	require.NoError(t, os.WriteFile(LockPath(vaultPath), []byte(`{"pid":2147483000}`), 0o644))
	_, live = LockOwner(vaultPath)
	require.False(t, live, "dead pid is not an owner")
	require.False(t, OwnerIsExiting(vaultPath))
}

func TestOwnerIsExitingWhenALiveLockHolderNoLongerAnswers(t *testing.T) {
	// The parent process stands in for a runtime that is alive but past its
	// listener: a foreign live PID holding the lock.
	vaultPath := newTestVault(t)
	lock, err := json.Marshal(map[string]any{"pid": os.Getppid()})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(LockPath(vaultPath), lock, 0o600))
	require.True(t, OwnerIsExiting(vaultPath), "no manifest: the owner already removed it on the way out")

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	refused := "http://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	require.NoError(t, WriteManifest(vaultPath, InstanceManifest{
		InstanceID: InstanceID(vaultPath), RunID: "run", PID: os.Getppid(), VaultPath: vaultPath,
		Mode: ModeHeadless, HTTPURL: refused, ControlToken: "token",
	}))
	require.True(t, OwnerIsExiting(vaultPath), "nobody listening at the manifest address")
}
