package actions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallDefaultIgnoreWritesFile(t *testing.T) {
	tmp := t.TempDir()

	vault := &mocks.VaultManager{}
	vault.On("Path").Return(tmp, nil)

	path, err := InstallDefaultIgnore(vault, InstallIgnoreOptions{})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tmp, ".rhizome", "ignore"), path)

	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	for _, pattern := range []string{"node_modules/", ".git/"} {
		assert.Contains(t, string(data), pattern)
	}
}

func TestInstallDefaultIgnoreRespectsExistingFile(t *testing.T) {
	tmp := t.TempDir()
	existing := filepath.Join(tmp, ".rhizome", "ignore")
	require.NoError(t, os.MkdirAll(filepath.Dir(existing), 0o755))
	require.NoError(t, os.WriteFile(existing, []byte("custom\n"), 0o644))

	vault := &mocks.VaultManager{}
	vault.On("Path").Return(tmp, nil)

	_, err := InstallDefaultIgnore(vault, InstallIgnoreOptions{})
	assert.Error(t, err)
	before, readErr := os.ReadFile(existing)
	require.NoError(t, readErr)
	require.Equal(t, "custom\n", string(before))

	path, err := InstallDefaultIgnore(vault, InstallIgnoreOptions{Force: true})
	require.NoError(t, err)
	assert.Equal(t, existing, path)

	data, readErr := os.ReadFile(existing)
	require.NoError(t, readErr)
	assert.Contains(t, string(data), "node_modules/")
}
