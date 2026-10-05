package runtime

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/require"
)

func TestRegistryWriteListAndRemove(t *testing.T) {
	home := t.TempDir()
	origHome := vaultconfig.UserHomeDirectory
	vaultconfig.UserHomeDirectory = func() (string, error) { return home, nil }
	defer func() { vaultconfig.UserHomeDirectory = origHome }()

	registry, err := NewRegistry()
	require.NoError(t, err)

	manifest := InstanceManifest{
		InstanceID: "vault-a",
		VaultName:  "Vault A",
		VaultPath:  "/tmp/vault-a",
		PID:        os.Getpid(),
		Version:    "test",
		HTTPHost:   "127.0.0.1",
		HTTPPort:   43123,
		HTTPURL:    "http://127.0.0.1:43123",
		StartedAt:  time.Now().UTC(),
	}

	require.NoError(t, registry.Write(manifest))

	listed, err := registry.List()
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, manifest.InstanceID, listed[0].InstanceID)
	require.False(t, listed[0].UpdatedAt.IsZero())

	require.NoError(t, registry.Remove(manifest.InstanceID))
	listed, err = registry.List()
	require.NoError(t, err)
	require.Empty(t, listed)
}

func TestRegistryWriteReplacesExistingManifestWithoutTempLeak(t *testing.T) {
	home := t.TempDir()
	origHome := vaultconfig.UserHomeDirectory
	vaultconfig.UserHomeDirectory = func() (string, error) { return home, nil }
	defer func() { vaultconfig.UserHomeDirectory = origHome }()

	registry, err := NewRegistry()
	require.NoError(t, err)

	manifest := InstanceManifest{
		InstanceID: "vault-a",
		VaultName:  "Vault A",
		VaultPath:  "/tmp/vault-a",
		PID:        os.Getpid(),
		Version:    "test",
		HTTPHost:   "127.0.0.1",
		HTTPPort:   43123,
		HTTPURL:    "http://127.0.0.1:43123",
		StartedAt:  time.Now().UTC(),
	}

	require.NoError(t, registry.Write(manifest))
	manifest.Ready = true
	require.NoError(t, registry.Write(manifest))

	entries, err := os.ReadDir(filepath.Join(home, ".config", "rhizome", "instances"))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "vault-a.json", entries[0].Name())
}

func TestRegistryRemoveCleansManifestTemps(t *testing.T) {
	home := t.TempDir()
	origHome := vaultconfig.UserHomeDirectory
	vaultconfig.UserHomeDirectory = func() (string, error) { return home, nil }
	defer func() { vaultconfig.UserHomeDirectory = origHome }()

	registry, err := NewRegistry()
	require.NoError(t, err)

	target := registry.Path("vault-a")
	require.NoError(t, os.WriteFile(target, []byte("{}"), 0o644))
	require.NoError(t, os.WriteFile(target+".tmp", []byte("{}"), 0o644))
	require.NoError(t, os.WriteFile(target+".123.tmp", []byte("{}"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(target), "vault-b.json.tmp"), []byte("{}"), 0o644))

	require.NoError(t, registry.Remove("vault-a"))

	entries, err := os.ReadDir(filepath.Join(home, ".config", "rhizome", "instances"))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "vault-b.json.tmp", entries[0].Name())
}

func TestRegistryPruneStaleRemovesDeadProcessManifest(t *testing.T) {
	home := t.TempDir()
	origHome := vaultconfig.UserHomeDirectory
	vaultconfig.UserHomeDirectory = func() (string, error) { return home, nil }
	defer func() { vaultconfig.UserHomeDirectory = origHome }()

	registry, err := NewRegistry()
	require.NoError(t, err)

	manifest := InstanceManifest{
		InstanceID: "stale-vault",
		VaultName:  "Stale Vault",
		VaultPath:  "/tmp/stale-vault",
		PID:        999999,
		Version:    "test",
		HTTPHost:   "127.0.0.1",
		HTTPPort:   43124,
		HTTPURL:    "http://127.0.0.1:43124",
		StartedAt:  time.Now().Add(-10 * time.Minute).UTC(),
		UpdatedAt:  time.Now().Add(-10 * time.Minute).UTC(),
	}
	require.NoError(t, registry.Write(manifest))

	require.NoError(t, registry.PruneStale())

	_, err = os.Stat(filepath.Join(home, ".config", "rhizome", "instances", manifest.InstanceID+".json"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestFormatStartupIncludesURLAndState(t *testing.T) {
	manifest := InstanceManifest{
		VaultName: "Docs",
		VaultPath: "/tmp/docs",
		PID:       1234,
		HTTPURL:   "http://127.0.0.1:4123",
	}

	warming := FormatStartup(manifest, true)
	require.Contains(t, warming, "Rhizome serve")
	require.Contains(t, warming, "vault: Docs")
	require.Contains(t, warming, "path:  /tmp/docs")
	require.Contains(t, warming, "pid:   1234")
	require.Contains(t, warming, "url:   http://127.0.0.1:4123")
	require.Contains(t, warming, "state: warming")

	ready := FormatStartup(manifest, false)
	require.Contains(t, ready, "state: ready")
}
