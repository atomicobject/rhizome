package repoconfig_test

import (
	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestVaultDefaultName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                 string
		git, local, ancestor bool
		configured, want     string
	}{
		{name: "explicit name", configured: "named", want: "named"},
		{name: "preferences", want: "preferred"},
		{name: "local directory", local: true, want: "local"},
		{name: "ancestor inside git", git: true, ancestor: true, want: "ancestor"},
		{name: "ancestor outside git", ancestor: true, want: "preferred"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := testkit.Env(t)
			root := testkit.Vault(t, nil)
			child := filepath.Join(root, "child")
			require.NoError(t, os.MkdirAll(child, 0755))
			if tc.git {
				require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), 0755))
			}
			env.WorkingDir = env.HomeDir
			if tc.ancestor {
				env.WorkingDir = child
			}
			if tc.local {
				env.WorkingDir = root
			}
			require.NoError(t, repoconfig.SaveCliConfig(env, repoconfig.CliConfig{DefaultVaultName: "preferred"}))
			vault := repoconfig.Vault{Env: env, Name: tc.configured}
			actual, err := vault.DefaultName()
			require.NoError(t, err)
			want := tc.want
			if want == "local" {
				want = root
			}
			if want == "ancestor" {
				want = paths.ResolveSymlinks(root).String()
			}
			require.Equal(t, want, actual)
		})
	}
}

func TestVaultDefaultNameInvalidPreferences(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, content, want string }{
		{"missing", "", repoconfig.RhizomeConfigReadError},
		{"malformed", "default_vault_name: [oops\n", repoconfig.RhizomeConfigParseError},
		{"empty", "default_vault_name: ''\n", repoconfig.RhizomeConfigParseError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := testkit.Env(t)
			if tc.content != "" {
				require.NoError(t, repoconfig.SaveCliConfig(env, repoconfig.CliConfig{}))
				_, file, err := env.CliPath()
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(file, []byte(tc.content), 0600))
			}
			_, err := (&repoconfig.Vault{Env: env}).DefaultName()
			require.EqualError(t, err, tc.want)
		})
	}
}

func TestVaultSetDefaultName(t *testing.T) {
	t.Parallel()
	env := testkit.Env(t)
	vault := repoconfig.Vault{Env: env}
	require.NoError(t, vault.SetDefaultName("vault-name"))
	cfg, err := repoconfig.LoadCliConfig(env, false)
	require.NoError(t, err)
	require.Equal(t, "vault-name", cfg.DefaultVaultName)
	require.Equal(t, "vault-name", vault.Name)
	root := testkit.Vault(t, nil)
	require.NoError(t, vault.SetDefaultName(root))
	cfg, err = repoconfig.LoadCliConfig(env, false)
	require.NoError(t, err)
	require.Equal(t, paths.ResolveSymlinks(root).String(), cfg.DefaultVaultName)
}
