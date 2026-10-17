package cmd

import "github.com/atomicobject/rhizome/internal/testkit"

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestGraphIgnoreWritesCanonicalGraphConfig(t *testing.T) {
	commandOptions := newRuntimeOptionsForTest(t)

	rootCmd := newRootCommand(commandEnv{Getwd: os.Getwd, Config: testkit.Env(t)})
	graphIgnoreCmd, _, _ := rootCmd.Find([]string{"graph", "ignore"})

	root := t.TempDir()
	configPath := filepath.Join(root, ".rhizome", "config.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	require.NoError(t, os.WriteFile(configPath, []byte("notes:\n  includes:\n    - \"**/*.md\"\n"), 0o644))

	origVaultName := commandOptions.vaultName
	commandOptions.vaultName = root
	t.Cleanup(func() { commandOptions.vaultName = origVaultName })

	require.NoError(t, graphIgnoreCmd.Flags().Set("vault", root))
	require.NoError(t, graphIgnoreCmd.RunE(graphIgnoreCmd, []string{"archive/**", "drafts/**"}))

	cfg, err := repoconfig.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Empty(t, cfg.Warnings)
	require.Equal(t, []string{"archive/**", "drafts/**"}, cfg.Graph.Ignore)
	require.Equal(t, []string{"**/*.md"}, cfg.Notes.Includes)
}
