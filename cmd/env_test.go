package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommandVaultUsesContextObsidianConfigFile(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "obsidian.json")
	require.NoError(t, os.WriteFile(configPath, []byte(`{
		"vaults": {
			"isolated": {
				"path": "/tmp/rhizome-command-env-vault"
			}
		}
	}`), 0o644))

	ctx := contextWithCommandEnv(context.Background(), commandEnv{
		ObsidianConfigFile: func() (string, error) {
			return configPath, nil
		},
	})

	vaultPath, err := commandVault(ctx, "rhizome-command-env-vault").Path()
	require.NoError(t, err)
	require.Equal(t, "/tmp/rhizome-command-env-vault", vaultPath)
}
