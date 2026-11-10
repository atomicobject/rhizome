//go:build integration

package integration

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

func TestImmediateStopWaitsForNonwaitingRuntimeSpawn(t *testing.T) {
	vault := newRuntimeVault(t, "")
	t.Setenv("HOME", vault.home)
	t.Setenv("USERPROFILE", vault.home)
	ctx := vaultconfig.WithEnv(t.Context(), vaultconfig.Env{HomeDir: vault.home})

	release, acquired, err := indexlock.TryAcquire(filepath.Join(vault.root, ".rhizome", "index.lock"))
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { _ = release() })

	var output bytes.Buffer
	result, err := appruntime.Ensure(ctx, appruntime.EnsureOptions{
		VaultPath: vault.root, Executable: vault.binary, Autostart: true, Wait: false, Budget: time.Millisecond,
		Logf: func(format string, args ...any) { _, _ = fmt.Fprintf(&output, format+"\n", args...) },
	})
	require.NoError(t, err, output.String())
	require.True(t, result.Spawned)
	vault.rememberSpawnedRuntimePIDs(output.String())
	require.Len(t, vault.activeRuntimePIDs(), 1, "the real runtime child must be alive before stop")

	require.NoError(t, appruntime.Stop(ctx, appruntime.StopOptions{
		VaultPath: vault.root, Out: &output, Grace: appruntime.StopGrace,
	}), output.String())
	require.Empty(t, vault.activeRuntimePIDs(), "stop must return only after its child exits: %s", output.String())
	require.NoFileExists(t, appruntime.ManifestPath(vault.root))
	require.NoError(t, release())
	require.NoError(t, os.RemoveAll(vault.root), "the stopped runtime must release the repository for cleanup")
	require.NoDirExists(t, vault.root)
}
