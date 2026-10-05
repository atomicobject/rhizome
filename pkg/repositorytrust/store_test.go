package repositorytrust

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStoreTrustIsCanonicalAndPerCheckout(t *testing.T) {
	store := Store{Dir: filepath.Join(t.TempDir(), "trust")}
	checkout := t.TempDir()
	otherWorktree := t.TempDir()
	alias := checkout
	symlinkAlias := filepath.Join(t.TempDir(), "checkout")
	if err := os.Symlink(checkout, symlinkAlias); err == nil {
		alias = symlinkAlias
	}

	trusted, err := store.Trusted(checkout)
	require.NoError(t, err)
	require.False(t, trusted)

	canonical, err := store.Trust(alias)
	require.NoError(t, err)
	expectedCanonical, err := CanonicalCheckout(checkout)
	require.NoError(t, err)
	require.Equal(t, expectedCanonical, canonical)

	trusted, err = store.Trusted(checkout)
	require.NoError(t, err)
	require.True(t, trusted)
	trusted, err = store.Trusted(otherWorktree)
	require.NoError(t, err)
	require.False(t, trusted)

	canonical, removed, err := store.Untrust(alias)
	require.NoError(t, err)
	require.True(t, removed)
	require.Equal(t, expectedCanonical, canonical)
	trusted, err = store.Trusted(checkout)
	require.NoError(t, err)
	require.False(t, trusted)
}

func TestStoreRejectsMismatchedRecordContents(t *testing.T) {
	store := Store{Dir: filepath.Join(t.TempDir(), "trust")}
	checkout := t.TempDir()
	_, err := store.Trust(checkout)
	require.NoError(t, err)

	records, err := os.ReadDir(store.Dir)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.NoError(t, os.WriteFile(filepath.Join(store.Dir, records[0].Name()), []byte("another checkout\n"), 0o600))

	trusted, err := store.Trusted(checkout)
	require.NoError(t, err)
	require.False(t, trusted)
}

func TestDefaultStoreIgnoresHomeChangesAfterProcessStart(t *testing.T) {
	if processUserHomeErr != nil || processUserHome == "" {
		t.Skip("process user home unavailable")
	}
	t.Setenv("HOME", filepath.Join(t.TempDir(), "repository-controlled-home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "repository-controlled-config"))

	store, err := DefaultStore()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(processUserHome, ".config", "rhizome", storeDirectoryName), store.Dir)
}
