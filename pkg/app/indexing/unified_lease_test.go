package indexing

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestUnifiedCoreRejectsForeignVaultDefinitionBeforeStoreMutation(t *testing.T) {
	t.Parallel()
	owned, foreign := t.TempDir(), t.TempDir()
	lease, acquired, err := indexlock.TryAcquireLease(owned, indexlock.AcquireOptions{Role: "test/authority"})
	require.NoError(t, err)
	require.True(t, acquired)
	defer func() { require.NoError(t, lease.Release()) }()
	err = RunUnifiedCore(context.Background(), lease, UnifiedOptions{VaultPath: owned, VaultDef: obsidian.VaultDefinition{Root: foreign}, NoteMetadata: testkit.NoteMetadataIndexer(t)})
	require.ErrorContains(t, err, "vault definition lease")
	_, err = os.Stat(filepath.Join(owned, ".rhizome", "db.sqlite"))
	require.ErrorIs(t, err, os.ErrNotExist)
}
