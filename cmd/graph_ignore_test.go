package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestGraphIgnoreWritesCanonicalGraphConfig(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".rhizome", "config.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	require.NoError(t, os.WriteFile(configPath, []byte("notes:\n  includes:\n    - \"**/*.md\"\n"), 0o644))

	origVaultName := vaultName
	vaultName = root
	t.Cleanup(func() { vaultName = origVaultName })

	require.NoError(t, graphIgnoreCmd.RunE(graphIgnoreCmd, []string{"archive/**", "drafts/**"}))

	cfg, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Empty(t, cfg.Warnings)
	require.Equal(t, []string{"archive/**", "drafts/**"}, cfg.Graph.Ignore)
	require.Equal(t, []string{"**/*.md"}, cfg.Notes.Includes)
}
