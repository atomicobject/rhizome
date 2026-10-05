package cmd

import (
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/stretchr/testify/require"
)

func TestOpenOptionalIntelStoreForAnalysis_SkipsMissingVaultPath(t *testing.T) {
	t.Parallel()

	store, cleanup, err := openOptionalIntelStoreForAnalysis("/definitely-missing-test-vault")
	require.NoError(t, err)
	require.Nil(t, store)
	require.Nil(t, cleanup)
}

func TestOpenOptionalIntelStoreForAnalysis_OpensStoreForRealVault(t *testing.T) {
	t.Parallel()

	vaultPath := t.TempDir()
	storePath := filepath.Join(vaultPath, ".rhizome", "db.sqlite")
	store, err := semdb.Open(storePath)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, cleanup, err := openOptionalIntelStoreForAnalysis(vaultPath)
	require.NoError(t, err)
	require.NotNil(t, store)
	require.NotNil(t, cleanup)
	cleanup()
}
