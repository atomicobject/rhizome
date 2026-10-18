package repoconfig_test

import (
	"github.com/atomicobject/rhizome/internal/testkit"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestSaveCliConfig_WritesPrivatePermissions(t *testing.T) {
	configRoot := filepath.Join(t.TempDir(), "config")
	configFile := filepath.Join(configRoot, "config.yml")
	env := testkit.Env(t)
	env.CLIConfigFile = configFile

	err := repoconfig.SaveCliConfig(env, repoconfig.CliConfig{
		Env: map[string]string{
			"OPENAI_API_KEY": "secret",
		},
	})
	require.NoError(t, err)

	dirInfo, err := os.Stat(configRoot)
	require.NoError(t, err)
	dirPerm := dirInfo.Mode().Perm()
	expectedDirPerm := os.FileMode(0o700)
	if runtime.GOOS == "windows" {
		require.Equal(t, expectedDirPerm, dirPerm&expectedDirPerm)
	} else {
		require.Equal(t, expectedDirPerm, dirPerm)
	}

	fileInfo, err := os.Stat(configFile)
	require.NoError(t, err)
	filePerm := fileInfo.Mode().Perm()
	expectedFilePerm := os.FileMode(0o600)
	if runtime.GOOS == "windows" {
		require.Equal(t, expectedFilePerm, filePerm&expectedFilePerm)
	} else {
		require.Equal(t, expectedFilePerm, filePerm)
	}
}

func TestSaveCliConfig_UpgradesExistingFileToPrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits")
	}
	configRoot := filepath.Join(t.TempDir(), "config")
	configFile := filepath.Join(configRoot, "config.yml")
	env := testkit.Env(t)
	env.CLIConfigFile = configFile
	require.NoError(t, os.MkdirAll(configRoot, 0o700))
	require.NoError(t, os.WriteFile(configFile, []byte("defaultVault: old\n"), 0o644))
	require.NoError(t, os.Chmod(configFile, 0o644))

	require.NoError(t, repoconfig.SaveCliConfig(env, repoconfig.CliConfig{DefaultVaultName: "new"}))
	info, err := os.Stat(configFile)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	content, err := os.ReadFile(configFile)
	require.NoError(t, err)
	require.Contains(t, string(content), "default_vault_name: new")
}
