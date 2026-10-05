package actions_test

import (
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/indexing"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/config"
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

func namespaceTestRefresher(t *testing.T, root string) indexing.ValidationProjectionPostApplyRefresher {
	t.Helper()
	home := t.TempDir()
	originalHome, originalPreferences := config.UserHomeDirectory, obsidian.CliConfigPath
	config.UserHomeDirectory = func() (string, error) { return home, nil }
	obsidian.CliConfigPath = func() (string, string, error) {
		return home, filepath.Join(home, "config.yml"), nil
	}
	t.Cleanup(func() {
		config.UserHomeDirectory, obsidian.CliConfigPath = originalHome, originalPreferences
	})
	return indexing.ValidationProjectionPostApplyRefresher{
		VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root},
		NoteMetadata: namespaceTestMetadata(t), NoteReader: &obsidian.Note{},
	}
}
