package config_test

import (
	"github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestConfigCliPath(t *testing.T) {
	t.Parallel()
	env := config.Env{HomeDir: "home"}
	dir, file, err := env.CliPath()
	require.NoError(t, err)
	require.Equal(t, filepath.Join("home", ".config", "rhizome"), dir)
	require.Equal(t, filepath.Join(dir, "config.yml"), file)
	_, _, err = (config.Env{}).CliPath()
	require.EqualError(t, err, config.UserConfigDirectoryNotFoundErrorMessage)
}
