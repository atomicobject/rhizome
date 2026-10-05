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

func TestFixtureCleanupStopsDetachedStartupBeforeManifest(t *testing.T) {
	t.Parallel()
	vault := newRuntimeVault(t, "")
	lockPath := filepath.Join(vault.root, ".rhizome", "index.lock")
	release, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	defer func() { _ = release() }()

	started := vault.start(t, "serve", "--headless", "--open=false", "--port", "0")
	vault.rememberRuntimePID(started.pid)
	require.Eventually(t, func() bool {
		owner, live := appruntime.LockOwner(vault.root)
		return live && owner == started.pid && indexlock.CheckPriority(lockPath)
	}, 15*time.Second, 25*time.Millisecond, "the child should own election while recovery waits for the writer")
	require.NoFileExists(t, appruntime.ManifestPath(vault.root))

	vault.stopRuntime()
	select {
	case <-started.exited:
	case <-time.After(5 * time.Second):
		t.Fatal("fixture cleanup left a manifest-less runtime running")
	}
	require.NoFileExists(t, appruntime.ManifestPath(vault.root))
}
