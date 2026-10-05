package obsidian

import (
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestOpenIntelStoreFromConfigIfVaultPresent_SkipsMissingVaultPath(t *testing.T) {
	store, cleanup, err := OpenIntelStoreFromConfigIfVaultPresent("/definitely-missing-test-vault", codeanchor.DefaultConfig("/definitely-missing-test-vault"), false)
	require.NoError(t, err)
	require.Nil(t, store)
	require.Nil(t, cleanup)
}

func TestOpenIntelStoreForWriteFromConfigIfVaultPresent_SkipsMissingVaultPath(t *testing.T) {
	store, cleanup, err := OpenIntelStoreForWriteFromConfigIfVaultPresent("/definitely-missing-test-vault", codeanchor.DefaultConfig("/definitely-missing-test-vault"))
	require.NoError(t, err)
	require.Nil(t, store)
	require.Nil(t, cleanup)
}

func TestOpenIntelStoreForWriteFromConfigIfVaultPresent_OpensForRealVault(t *testing.T) {
	vaultPath := t.TempDir()
	cfg := codeanchor.DefaultConfig(vaultPath)
	cfg.IndexPath = filepath.Join(vaultPath, ".rhizome", "db.sqlite")

	store, cleanup, err := OpenIntelStoreForWriteFromConfigIfVaultPresent(vaultPath, cfg)
	require.NoError(t, err)
	require.NotNil(t, store)
	require.NotNil(t, cleanup)
	cleanup()
}
