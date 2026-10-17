package cmd

import "github.com/atomicobject/rhizome/internal/testkit"

import (
	"bytes"
	"context"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"os"

	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func withTempCliConfig(t *testing.T) vaultconfig.Env {
	t.Helper()
	for _, key := range []string{"ATOMIC_RHIZOME_KEY", "OPENAI_API_KEY", "RHIZOME_OPENAI_API_KEY", "VOYAGE_API_KEY", "RHIZOME_VOYAGE_API_KEY"} {
		t.Setenv(key, "")
	}
	return vaultconfig.Env{HomeDir: t.TempDir(), LookupEnv: os.LookupEnv}
}

func runConfigSetEnv(t *testing.T, env vaultconfig.Env, args ...string) error {
	rootCmd := newRootCommand(commandEnv{Getwd: os.Getwd, Config: testkit.Env(t)})
	configSetEnvCmd, _, _ := rootCmd.Find([]string{"config", "set-env"})

	t.Helper()
	cmd := configSetEnvCmd
	cmd.SetContext(vaultconfig.WithEnv(context.Background(), env))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	return cmd.RunE(cmd, args)
}

func TestConfigSetEnvWritesValueAndOverwrites(t *testing.T) {
	env := withTempCliConfig(t)

	require.NoError(t, runConfigSetEnv(t, env, "ATOMIC_RHIZOME_KEY", "abc"))

	cfg, err := repoconfig.LoadCliConfig(env, false)
	require.NoError(t, err)
	require.Equal(t, "abc", cfg.Env["ATOMIC_RHIZOME_KEY"])

	require.NoError(t, runConfigSetEnv(t, env, "ATOMIC_RHIZOME_KEY", "xyz"))

	cfg, err = repoconfig.LoadCliConfig(env, false)
	require.NoError(t, err)
	require.Equal(t, "xyz", cfg.Env["ATOMIC_RHIZOME_KEY"])
}

func TestConfigSetEnvEmptyValueClears(t *testing.T) {
	env := withTempCliConfig(t)
	require.NoError(t, repoconfig.SaveCliConfig(env, repoconfig.CliConfig{
		Env: map[string]string{"ATOMIC_RHIZOME_KEY": "abc"},
	}))

	require.NoError(t, runConfigSetEnv(t, env, "ATOMIC_RHIZOME_KEY", ""))

	cfg, err := repoconfig.LoadCliConfig(env, false)
	require.NoError(t, err)
	_, ok := cfg.Env["ATOMIC_RHIZOME_KEY"]
	require.False(t, ok, "empty value should remove the key, got: %v", cfg.Env)
}

func TestConfigSetEnvRejectsEmptyKey(t *testing.T) {
	env := withTempCliConfig(t)
	err := runConfigSetEnv(t, env, "   ", "value")
	require.Error(t, err)
}
