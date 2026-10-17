package repoconfig_test

import (
	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRelativeVaultSelectionUsesInvocationDirectory(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"first", "second"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := testkit.Env(t)
			root := testkit.Vault(t, nil)
			env.WorkingDir = root
			vault := repoconfig.Vault{Env: env, Name: "."}
			definition, err := vault.Definition()
			require.NoError(t, err)
			want := paths.ResolveSymlinks(root).String()
			require.Equal(t, want, definition.BasePath())
			require.NoError(t, vault.SetDefaultName("."))
			preferences, err := repoconfig.LoadCliConfig(env, false)
			require.NoError(t, err)
			require.Equal(t, want, preferences.DefaultVaultName)
			// A separate invocation can resolve the stored absolute default without
			// sharing either the process directory or a path override.
			next := repoconfig.Vault{Env: env}
			actual, err := next.DefaultName()
			require.NoError(t, err)
			require.Equal(t, root, actual)
		})
	}
}

func TestRelativeVaultSelectionRequiresInvocationDirectory(t *testing.T) {
	t.Parallel()
	env := testkit.Env(t)
	env.WorkingDir = ""
	vault := repoconfig.Vault{Env: env, Name: "."}
	_, err := vault.Definition()
	require.EqualError(t, err, "working directory unavailable")
	require.EqualError(t, vault.SetDefaultName("."), "working directory unavailable")
	_, file, err := env.CliPath()
	require.NoError(t, err)
	require.NoFileExists(t, file)
}
