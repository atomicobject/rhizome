package operations_test

import (
	"github.com/atomicobject/rhizome/pkg/app/validationproduct"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"

	"github.com/stretchr/testify/require"
)

type namespaceVault struct{ path string }

func (v namespaceVault) DefaultName() (string, error) { return "", nil }
func (v namespaceVault) SetDefaultName(string) error  { return nil }
func (v namespaceVault) Path() (string, error)        { return v.path, nil }
func (v namespaceVault) Definition() (obsidian.VaultDefinition, error) {
	return obsidian.VaultDefinition{Path: v.path}, nil
}

func namespaceTestMetadata(t *testing.T) notemeta.Indexer {
	t.Helper()
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	return indexer
}

func namespaceTestRefresher(t *testing.T, root string) validationproduct.ValidationProjectionPostApplyRefresher {
	t.Helper()
	return validationproduct.ValidationProjectionPostApplyRefresher{
		VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root},
		NoteMetadata: namespaceTestMetadata(t), NoteReader: &obsidian.Note{},
	}
}
