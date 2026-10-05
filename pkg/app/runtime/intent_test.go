package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

func intentRegistry(t *testing.T) (*Registry, string) {
	t.Helper()
	home := t.TempDir()
	old := vaultconfig.UserHomeDirectory
	vaultconfig.UserHomeDirectory = func() (string, error) { return home, nil }
	t.Cleanup(func() { vaultconfig.UserHomeDirectory = old })
	registry, err := NewRegistry()
	require.NoError(t, err)
	return registry, t.TempDir()
}

func requireSpawnIntent(t *testing.T, registry *Registry, vault, token string) {
	t.Helper()
	created, err := registry.CreateSpawnIntent(context.Background(), vault, token)
	require.NoError(t, err)
	require.True(t, created)
}

func TestIntentReadersWaitForReplacementGate(t *testing.T) {
	registry, vault := intentRegistry(t)
	requireSpawnIntent(t, registry, vault, "spawn-a")
	for _, tc := range []struct {
		name string
		read func(context.Context) error
	}{
		{"one vault", func(ctx context.Context) error {
			_, _, err := registry.StartIntentFor(ctx, vault)
			return err
		}},
		{"all vaults", func(ctx context.Context) error {
			_, err := registry.ListIntents(ctx)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			release, acquired, err := indexlock.TryAcquireWithOptions(registry.intentGatePath(vault), indexlock.AcquireOptions{ProcessLifetime: true})
			require.NoError(t, err)
			require.True(t, acquired)
			t.Cleanup(func() { _ = release() })
			ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
			defer cancel()
			require.ErrorIs(t, tc.read(ctx), context.DeadlineExceeded, "intent read must wait for replacement")
			require.NoError(t, release())
			require.NoError(t, tc.read(context.Background()))
		})
	}
}

func TestListIntentsPreservesEarlierResultsWhenAnIntentGateTimesOut(t *testing.T) {
	registry, firstVault := intentRegistry(t)
	vaults := []string{firstVault, t.TempDir()}
	sort.Slice(vaults, func(i, j int) bool { return InstanceID(vaults[i]) < InstanceID(vaults[j]) })
	for _, vault := range vaults {
		requireSpawnIntent(t, registry, vault, "startup")
	}
	release, acquired, err := indexlock.TryAcquireWithOptions(registry.intentGatePath(vaults[1]), indexlock.AcquireOptions{ProcessLifetime: true})
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { _ = release() })

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	intents, err := registry.ListIntents(ctx)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Len(t, intents, 1)
	require.Equal(t, vaults[0], intents[0].VaultPath)
}

func TestStopCancellationFencesBothElectionAndPublication(t *testing.T) {
	registry, vault := intentRegistry(t)
	ctx := context.Background()
	requireSpawnIntent(t, registry, vault, "spawn-a")
	_, cancelled, err := registry.CancelIntent(ctx, vault, "spawn-a")
	require.NoError(t, err)
	require.True(t, cancelled)
	manifest := InstanceManifest{InstanceID: InstanceID(vault), VaultPath: vault, PID: os.Getpid(), RunID: "spawn-a"}
	require.ErrorIs(t, registry.ElectIntent(ctx, vault, "spawn-a", true, manifest), ErrStartCancelled)
	require.ErrorIs(t, registry.PublishManifestWithIntent(ctx, vault, "spawn-a", manifest), ErrStartCancelled)
	require.NoFileExists(t, ManifestPath(vault))

	requireSpawnIntent(t, registry, vault, "spawn-b")
	require.NoError(t, registry.ElectIntent(ctx, vault, "spawn-b", true, manifest))
	_, cancelled, err = registry.CancelIntent(ctx, vault, "spawn-b")
	require.NoError(t, err)
	require.True(t, cancelled)
	require.ErrorIs(t, registry.PublishManifestWithIntent(ctx, vault, "spawn-b", manifest), ErrStartCancelled)
	require.NoFileExists(t, ManifestPath(vault))
}

func TestCancelIntentCannotCancelAReplacementToken(t *testing.T) {
	registry, vault := intentRegistry(t)
	ctx := context.Background()
	requireSpawnIntent(t, registry, vault, "old")
	requireSpawnIntent(t, registry, vault, "new")
	_, cancelled, err := registry.CancelIntent(ctx, vault, "old")
	require.NoError(t, err)
	require.False(t, cancelled)
	intent, found, err := registry.StartIntentFor(ctx, vault)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "new", intent.Token)
	require.False(t, intent.Cancelled)
}

func TestSpawnIntentCannotReplaceAnElectedAttachedOwner(t *testing.T) {
	registry, vault := intentRegistry(t)
	ctx := context.Background()
	release, acquired, err := indexlock.TryAcquireWithOptions(LockPath(vault), indexlock.AcquireOptions{ProcessLifetime: true})
	require.NoError(t, err)
	require.True(t, acquired)
	defer func() { _ = release() }()
	attached := InstanceManifest{InstanceID: InstanceID(vault), VaultPath: vault, PID: os.Getpid(), RunID: "attached", Mode: ModeAttached}
	require.NoError(t, registry.ElectIntent(ctx, vault, "attached", false, attached))
	created, err := registry.CreateSpawnIntent(ctx, vault, "competitor")
	require.NoError(t, err)
	require.False(t, created)
	intent, found, err := registry.StartIntentFor(ctx, vault)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "attached", intent.Token)
}

func TestPendingIntentRemainsDiscoverableAfterParentDeath(t *testing.T) {
	registry, vault := intentRegistry(t)
	require.NoError(t, registry.writeIntent(StartIntent{VaultPath: vault, Token: "orphan", ParentPID: 999999}))
	require.NoError(t, registry.PruneStale())
	intents, err := registry.ListIntents(context.Background())
	require.NoError(t, err)
	require.Len(t, intents, 1)
	require.Equal(t, "orphan", intents[0].Token)
}

func TestRegistryKeepsOldHeartbeatFromALiveOwner(t *testing.T) {
	registry, vault := intentRegistry(t)
	manifest := InstanceManifest{InstanceID: InstanceID(vault), VaultPath: vault, PID: os.Getpid(), UpdatedAt: time.Now().Add(-time.Hour)}
	data, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(registry.Path(manifest.InstanceID), data, 0o600))
	listed, err := registry.List()
	require.NoError(t, err)
	require.Len(t, listed, 1)
}

func TestSpawnIntentResolvesVaultSymlinkAlias(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("temporary symlink privileges vary on Windows")
	}
	registry, vault := intentRegistry(t)
	alias := filepath.Join(t.TempDir(), "vault-alias")
	require.NoError(t, os.Symlink(vault, alias))
	requireSpawnIntent(t, registry, alias, "alias")
	intent, found, err := registry.StartIntentFor(context.Background(), vault)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "alias", intent.Token)
}

func TestWaitProbeReadyKeepsWaitingWhenAnAttachedServeTakesOverTheIntent(t *testing.T) {
	registry, vault := intentRegistry(t)
	ctx := context.Background()
	requireSpawnIntent(t, registry, vault, "detached")
	manifest := InstanceManifest{InstanceID: InstanceID(vault), VaultPath: vault, PID: os.Getpid(), RunID: "attached"}
	require.NoError(t, registry.ElectIntent(ctx, vault, "attached", false, manifest))

	err := waitProbeReady(ctx, vault, time.Now().Add(3*ensurePoll))
	require.ErrorIs(t, err, ErrNoRuntime)
	require.NotErrorIs(t, err, ErrStartCancelled)

	_, cancelled, err := registry.CancelIntent(ctx, vault, "attached")
	require.NoError(t, err)
	require.True(t, cancelled)
	require.ErrorIs(t, waitProbeReady(ctx, vault, time.Now().Add(3*ensurePoll)), ErrStartCancelled)
}

func TestCancelledStopCannotCancelStartIntent(t *testing.T) {
	registry, vault := intentRegistry(t)
	requireSpawnIntent(t, registry, vault, "starting")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, cancelled, err := registry.CancelIntent(ctx, vault, "starting")
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, cancelled)
	intent, found, err := registry.StartIntentFor(context.Background(), vault)
	require.NoError(t, err)
	require.True(t, found)
	require.False(t, intent.Cancelled, "an abandoned stop must leave startup intact")
}

func TestElectedIntentSurvivesVaultAliasDisappearance(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("temporary symlink privileges vary on Windows")
	}
	for _, spawned := range []bool{false, true} {
		name := "attached"
		if spawned {
			name = "spawned"
		}
		t.Run(name, func(t *testing.T) {
			registry, vault := intentRegistry(t)
			canonical, err := filepath.EvalSymlinks(vault)
			require.NoError(t, err)
			alias := filepath.Join(t.TempDir(), "vault-alias")
			require.NoError(t, os.Symlink(vault, alias))
			ctx := context.Background()
			if spawned {
				requireSpawnIntent(t, registry, alias, "owner")
			}
			manifest := InstanceManifest{InstanceID: InstanceID(alias), VaultPath: alias, PID: os.Getpid(), RunID: "owner"}
			require.NoError(t, registry.ElectIntent(ctx, canonical, "owner", spawned, manifest))
			require.NoError(t, os.Rename(vault, filepath.Join(t.TempDir(), "moved-vault")))

			_, cancelled, err := registry.CancelIntent(ctx, canonical, "owner")
			require.NoError(t, err)
			require.True(t, cancelled)
			require.NoError(t, registry.ClearIntent(ctx, canonical, "owner"))
			require.NoError(t, registry.ClearEarlyRegistration(ctx, canonical, "owner"))
			intents, err := registry.ListIntents(ctx)
			require.NoError(t, err)
			require.Empty(t, intents, "shutdown must remove the elected intent even after its original alias disappears")
			require.NoFileExists(t, registry.Path(manifest.InstanceID))
		})
	}
}
