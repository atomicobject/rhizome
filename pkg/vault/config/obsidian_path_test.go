package config_test

import (
	"github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestConfigObsidianPath(t *testing.T) {
	t.Parallel()
	env := config.Env{ConfigDir: "config"}
	file, err := env.ObsidianFile()
	require.NoError(t, err)
	require.Equal(t, filepath.Join("config", "obsidian", "obsidian.json"), file)
	_, err = (config.Env{}).ObsidianFile()
	require.EqualError(t, err, config.UserConfigDirectoryNotFoundErrorMessage)
}
