//go:build integration

package integration

import (
	"path/filepath"
	"testing"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

func TestStopCancelsStartingOwnerWhileRecoveryWaits(t *testing.T) {
	t.Parallel()
	vault := newRuntimeVault(t, "")
	lockPath := filepath.Join(vault.root, ".rhizome", "index.lock")
	release, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	defer func() { _ = release() }()
	started := vault.start(t, "start", "--open=false", "--port", "0")
	require.Eventually(t, func() bool {
		pid, live := appruntime.LockOwner(vault.root)
		return live && pid == started.pid
	}, 15*time.Second, 25*time.Millisecond)
	require.NoFileExists(t, appruntime.ManifestPath(vault.root))
	result := vault.run(nil, "stop")
	require.NoError(t, result.err, result.stdout+result.stderr)
	require.Contains(t, result.stdout+result.stderr, "stopped")
	select {
	case <-started.exited:
	case <-time.After(15 * time.Second):
		t.Fatal("starting runtime remained alive while the writer held index.lock")
	}
	require.NoFileExists(t, appruntime.ManifestPath(vault.root))
	_, acquired, err = indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.False(t, acquired, "stop must not remove another writer's lock")
}

func TestStartWaitsForAWriterThenRestartsCleanly(t *testing.T) {
	t.Parallel()
	vault := newRuntimeVault(t, "")
	lockPath := filepath.Join(vault.root, ".rhizome", "index.lock")
	release, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { _ = release() })

	started := vault.start(t, "start", "--open=false", "--port", "0")
	select {
	case <-started.exited:
		t.Fatal("start exited while another writer was finishing")
	case <-time.After(time.Second):
	}
	require.NoError(t, release())
	require.Equal(t, started.pid, vault.requireLiveRuntime(t).PID)

	stopped := vault.run(nil, "stop")
	require.NoError(t, stopped.err, stopped.stderr)
	select {
	case <-started.exited:
	case <-time.After(30 * time.Second):
		t.Fatal("start did not finish shutdown")
	}

	restarted := vault.start(t, "start", "--open=false", "--port", "0")
	require.Equal(t, restarted.pid, vault.requireLiveRuntime(t).PID)
}

func TestStopAllCancelsAttachedStartupBeforeManifest(t *testing.T) {
	t.Parallel()
	vault := newRuntimeVault(t, "")
	lockPath := filepath.Join(vault.root, ".rhizome", "index.lock")
	release, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	defer func() { _ = release() }()
	started := vault.start(t, "start", "--open=false", "--port", "0")
	require.Eventually(t, func() bool {
		pid, live := appruntime.LockOwner(vault.root)
		// Recovery requests priority only after the owner is registered for stop --all.
		return live && pid == started.pid && indexlock.CheckPriority(lockPath)
	}, 15*time.Second, 25*time.Millisecond)
	require.NoFileExists(t, appruntime.ManifestPath(vault.root))
	result := vault.run(nil, "stop", "--all")
	require.NoError(t, result.err, result.stdout+result.stderr)
	require.Contains(t, result.stdout+result.stderr, "stopped")
	select {
	case <-started.exited:
	case <-time.After(15 * time.Second):
		t.Fatal("stop --all left the starting attached owner alive")
	}
}
