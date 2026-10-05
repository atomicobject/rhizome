package init

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func mockCliConfig(t *testing.T) {
	t.Helper()
	original := obsidian.CliConfigPath
	dir := t.TempDir()
	file := filepath.Join(dir, "config.yml")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	obsidian.CliConfigPath = func() (string, string, error) {
		return dir, file, nil
	}
	t.Cleanup(func() {
		obsidian.CliConfigPath = original
	})
}

func readCliConfigForTest(t *testing.T) obsidian.CliConfig {
	t.Helper()
	_, path, err := obsidian.CliConfigPath()
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return obsidian.CliConfig{}
	}
	require.NoError(t, err)
	var cfg obsidian.CliConfig
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	return cfg
}
