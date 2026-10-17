package repoconfig_test

import (
	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestVaultPath(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, registry, preferred, want, wantError string }{
		{name: "obsidian", registry: `{"vaults":{"first":{"path":"/path/to/vault1"}}}`, want: "/path/to/vault1"},
		{name: "preferences take precedence", registry: `bad json`, preferred: "/manual/path", want: "/manual/path"},
		{name: "missing registry", wantError: repoconfig.ObsidianConfigReadError},
		{name: "malformed registry", registry: `bad json`, wantError: repoconfig.ObsidianConfigParseError},
		{name: "unknown name", registry: `{"vaults":{}}`, wantError: repoconfig.ObsidianConfigVaultNotFoundError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := testkit.Env(t)
			env.ObsidianConfigFile = filepath.Join(env.HomeDir, "obsidian.json")
			if tc.registry != "" {
				require.NoError(t, os.WriteFile(env.ObsidianConfigFile, []byte(tc.registry), 0600))
			}
			if tc.preferred != "" {
				require.NoError(t, repoconfig.SaveCliConfig(env, repoconfig.CliConfig{Vaults: map[string]obsidian.VaultDefinition{"vault1": {Path: tc.preferred}}}))
			}
			actual, err := (&repoconfig.Vault{Env: env, Name: "vault1"}).Path()
			if tc.wantError != "" {
				require.EqualError(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, actual)
		})
	}
}
func TestVaultPathIsolatedEnvironments(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"first", "second"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := testkit.Env(t)
			env.ObsidianConfigFile = filepath.Join(env.HomeDir, "obsidian.json")
			want := filepath.ToSlash(filepath.Join(env.HomeDir, "same-name"))
			require.NoError(t, os.WriteFile(env.ObsidianConfigFile, []byte(`{"vaults":{"v":{"path":"`+want+`"}}}`), 0600))
			vault := repoconfig.Vault{Env: env, Name: "same-name"}
			actual, err := vault.Path()
			require.NoError(t, err)
			require.Equal(t, want, actual)
			vaults, err := repoconfig.ListObsidianVaults(env)
			require.NoError(t, err)
			require.Equal(t, want, vaults["v"].Path)
		})
	}
}
