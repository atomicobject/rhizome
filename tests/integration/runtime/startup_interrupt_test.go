//go:build integration && !windows

package integration

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

func TestStartInterruptCancelsRecoveryWaitWithoutRemovingTheWriterLock(t *testing.T) {
	t.Parallel()
	vault := newRuntimeVault(t, "")
	lockPath := filepath.Join(vault.root, ".rhizome", "index.lock")
	release, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { _ = release() })
	started := vault.start(t, "start", "--open=false", "--port", "0")
	require.Eventually(t, func() bool { return indexlock.CheckPriority(lockPath) }, 15*time.Second, 25*time.Millisecond)
	process, err := os.FindProcess(started.pid)
	require.NoError(t, err)
	require.NoError(t, process.Signal(os.Interrupt))
	select {
	case <-started.exited:
	case <-time.After(15 * time.Second):
		t.Fatal("Ctrl-C did not cancel startup recovery waiting for another writer")
	}
	_, acquired, err = indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.False(t, acquired, "canceling startup must not remove another writer's lock")
	require.False(t, indexlock.CheckPriority(lockPath))
	unlockRuntime, acquired, err := indexlock.TryAcquire(appruntime.LockPath(vault.root))
	require.NoError(t, err)
	require.True(t, acquired, "canceled startup must release runtime ownership")
	require.NoError(t, unlockRuntime())
}

func TestStartRestartsImmediatelyAfterInterrupt(t *testing.T) {
	t.Parallel()
	vault := newRuntimeVault(t, "")
	for round := 0; round < 3; round++ {
		started := vault.start(t, "start", "--open=false", "--port", "0")
		require.Equal(t, started.pid, vault.requireLiveRuntime(t).PID)
		process, err := os.FindProcess(started.pid)
		require.NoError(t, err)
		require.NoError(t, process.Signal(os.Interrupt))
		select {
		case <-started.exited:
		case <-time.After(30 * time.Second):
			t.Fatalf("round %d did not stop after Ctrl-C", round)
		}
		_, err = appruntime.ReadManifest(vault.root)
		require.Error(t, err, "round %d left a runtime manifest after exit", round)
	}
}

func TestInterruptAfterVaultAliasDisappearsClearsStartupRecords(t *testing.T) {
	t.Parallel()
	vault := newRuntimeVault(t, "")
	alias := filepath.Join(t.TempDir(), "vault-alias")
	require.NoError(t, os.Symlink(vault.root, alias))
	movedRoot := filepath.Join(t.TempDir(), "moved-vault")
	instanceID := appruntime.InstanceID(vault.root)
	registryPath := filepath.Join(vault.home, ".config", "rhizome", "instances", instanceID)

	started := vault.start(t, "serve", "--vault", alias, "--open=false", "--port", "0")
	require.Equal(t, started.pid, vault.requireLiveRuntime(t).PID)
	require.FileExists(t, registryPath+".pending.json")
	require.NoError(t, os.Rename(vault.root, movedRoot))
	process, err := os.FindProcess(started.pid)
	require.NoError(t, err)
	// Root monitoring may already have initiated shutdown; either path must
	// remove the same registry records after the original alias stops resolving.
	_ = process.Signal(os.Interrupt)
	select {
	case <-started.exited:
	case <-time.After(30 * time.Second):
		t.Fatal("runtime did not exit after its vault alias disappeared")
	}
	require.NoFileExists(t, registryPath+".pending.json")
	require.NoFileExists(t, registryPath+".json")
}
