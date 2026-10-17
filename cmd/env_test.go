package cmd

import (
	"context"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/paths"
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
		Config: vaultconfig.Env{ObsidianConfigFile: configPath, HomeDir: t.TempDir()},
	})

	vaultPath, err := commandVault(ctx, "rhizome-command-env-vault").Path()
	require.NoError(t, err)
	require.Equal(t, "/tmp/rhizome-command-env-vault", vaultPath)
}

func TestCommandEnvironmentUsesInvocationWorkingDirectory(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"first", "second"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := testkit.Env(t)
			root := testkit.Vault(t, nil)
			env.WorkingDir = root
			ctx := vaultconfig.WithEnv(context.Background(), env)
			cwd, err := commandEnvFromContext(ctx).Getwd()
			require.NoError(t, err)
			require.Equal(t, root, cwd)
			def, err := commandVault(ctx, ".").Definition()
			require.NoError(t, err)
			require.Equal(t, paths.ResolveSymlinks(root).String(), def.BasePath())
		})
	}
}
