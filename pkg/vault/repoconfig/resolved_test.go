package repoconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolvedConfigurationKeepsOneDocumentAcrossSubsystems(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0755))
	configPath := filepath.Join(root, ".rhizome", "config.yml")
	require.NoError(t, os.WriteFile(configPath, []byte("indexPath: indexes/shared.sqlite\nbudgetChars: 12345\ncode:\n  enabled: true\nnoteEmbeddings:\n  enabled: true\n  provider: voyage\ncodeEmbeddings:\n  enabled: true\n"), 0644))
	resolved, err := Load(root)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, []byte("broken: [yaml"), 0644))
	require.Equal(t, filepath.Join(resolved.VaultPath, "indexes", "shared.sqlite"), resolved.Code.IndexPath)
	require.Equal(t, resolved.Code.IndexPath, resolved.NoteEmbeddings.IndexPath)
	code, explicit := resolved.CodeEmbeddingsFor(resolved.NoteEmbeddings)
	require.True(t, explicit)
	require.Equal(t, resolved.Code.IndexPath, code.IndexPath)
	require.Equal(t, "voyage", code.Provider)
	require.Equal(t, 12345, resolved.BudgetChars)
	require.Error(t, resolved.ValidateEmbeddingsProvider())
	require.NoError(t, resolved.ValidateFor(root))
	require.Error(t, resolved.ValidateFor(t.TempDir()))
	_, err = Load(root)
	require.Error(t, err)
}
