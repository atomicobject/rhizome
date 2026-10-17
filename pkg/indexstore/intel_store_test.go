package indexstore

import (
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestOpenIntelStoreFromConfigIfVaultPresent_SkipsMissingVaultPath(t *testing.T) {
	store, cleanup, err := Open("/definitely-missing-test-vault", Options{Access: ReadOnly, IfVaultPresent: true, RequireEnabled: false})
	require.NoError(t, err)
	require.Nil(t, store)
	require.Nil(t, cleanup)
}

func TestOpenIntelStoreForWriteFromConfigIfVaultPresent_SkipsMissingVaultPath(t *testing.T) {
	store, cleanup, err := Open("/definitely-missing-test-vault", Options{Access: ReadWrite, Create: true, IfVaultPresent: true})
	require.NoError(t, err)
	require.Nil(t, store)
	require.Nil(t, cleanup)
}

func TestOpenIntelStoreForWriteFromConfigIfVaultPresent_OpensForRealVault(t *testing.T) {
	vaultPath := t.TempDir()
	cfg := codeanchor.DefaultConfig(vaultPath)
	cfg.IndexPath = filepath.Join(vaultPath, ".rhizome", "db.sqlite")

	store, cleanup, err := Open(vaultPath, Options{Access: ReadWrite, Config: &cfg, Create: true, IfVaultPresent: true})
	require.NoError(t, err)
	require.NotNil(t, store)
	require.NotNil(t, cleanup)
	cleanup()
}
