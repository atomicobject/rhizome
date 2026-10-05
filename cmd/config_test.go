package cmd

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/teamkeys"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func withTempCliConfig(t *testing.T) {
	t.Helper()
	// Redirect both the high-level cli config helper (used by Save/LoadCliConfig)
	// and the lower-level CliPath used by config.ResolveValue, which reads
	// ~/.config/rhizome/config.yml directly via os.UserHomeDir / os.UserConfigDir.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpHome, ".config"))
	// Pin the env vars credential resolution looks at, so other tests in the
	// package that leak them don't bleed into ours.
	t.Setenv("ATOMIC_RHIZOME_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("RHIZOME_OPENAI_API_KEY", "")
	t.Setenv("VOYAGE_API_KEY", "")
	t.Setenv("RHIZOME_VOYAGE_API_KEY", "")
	teamkeys.ResetCache()

	original := obsidian.CliConfigPath
	configRoot := filepath.Join(tmpHome, ".config", "rhizome")
	configFile := filepath.Join(configRoot, "config.yml")
	obsidian.CliConfigPath = func() (string, string, error) {
		return configRoot, configFile, nil
	}
	t.Cleanup(func() {
		obsidian.CliConfigPath = original
		teamkeys.ResetCache()
	})
}

func runConfigSetEnv(t *testing.T, args ...string) error {
	t.Helper()
	cmd := configSetEnvCmd
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	return cmd.RunE(cmd, args)
}

func TestConfigSetEnvWritesValueAndOverwrites(t *testing.T) {
	withTempCliConfig(t)

	require.NoError(t, runConfigSetEnv(t, "ATOMIC_RHIZOME_KEY", "abc"))

	cfg, err := obsidian.LoadCliConfig(false)
	require.NoError(t, err)
	require.Equal(t, "abc", cfg.Env["ATOMIC_RHIZOME_KEY"])

	require.NoError(t, runConfigSetEnv(t, "ATOMIC_RHIZOME_KEY", "xyz"))

	cfg, err = obsidian.LoadCliConfig(false)
	require.NoError(t, err)
	require.Equal(t, "xyz", cfg.Env["ATOMIC_RHIZOME_KEY"])
}

func TestConfigSetEnvEmptyValueClears(t *testing.T) {
	withTempCliConfig(t)
	require.NoError(t, obsidian.SaveCliConfig(obsidian.CliConfig{
		Env: map[string]string{"ATOMIC_RHIZOME_KEY": "abc"},
	}))

	require.NoError(t, runConfigSetEnv(t, "ATOMIC_RHIZOME_KEY", ""))

	cfg, err := obsidian.LoadCliConfig(false)
	require.NoError(t, err)
	_, ok := cfg.Env["ATOMIC_RHIZOME_KEY"]
	require.False(t, ok, "empty value should remove the key, got: %v", cfg.Env)
}

func TestConfigSetEnvRejectsEmptyKey(t *testing.T) {
	withTempCliConfig(t)
	err := runConfigSetEnv(t, "   ", "value")
	require.Error(t, err)
}
