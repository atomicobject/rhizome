package cmd

import (
	"bytes"
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"os"
	"path/filepath"
	"testing"

	actions "github.com/atomicobject/rhizome/pkg/app/operations"

	"github.com/stretchr/testify/require"
)

func TestRenameHeadingCommandReportsActualPublication(t *testing.T) {
	rootCmd := newRootCommand(commandEnv{Getwd: os.Getwd, Config: testkit.Env(t)})
	renameHeadingCmd, _, _ := rootCmd.Find([]string{"note", "rename-heading"})

	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "applied"}[changed], func(t *testing.T) {
			vault := setupAgentTestVault(t, map[string]string{
				"target.md": "# Target\n\n## Old Heading\n",
				"ref.md":    "[[target#Old Heading]]\n",
			})
			t.Chdir(vault.path)
			preferences := t.TempDir()
			env.HomeDir = preferences
			env.WorkingDir = vault.path
			env.ObsidianConfigFile = vault.env.ObsidianConfigFile
			t.Cleanup(func() {
				renameHeadingCmd.SetOut(nil)
				renameHeadingCmd.SetErr(nil)
			})
			next := "Old Heading"
			want := "Heading unchanged in target.md; rewrites: 1; upgraded: 0; skipped: 0"
			if changed {
				next = "New Heading"
				want = "Renamed heading in target.md; rewrites: 1; upgraded: 0; skipped: 0"
			}
			stdout, stderr, err := runRootCLI(t, vaultconfig.WithEnv(context.Background(), env), []string{"note", "rename-heading", "target.md", "Old Heading", next, "--apply", "--upgrade-to-block-id", "never", "--fallback", "heading"})
			require.NoError(t, err, stderr)
			require.Empty(t, stderr)
			require.Equal(t, want, stdout)
			for path, expected := range map[string]string{"target.md": "# Target\n\n## " + next + "\n", "ref.md": "[[target#" + next + "]]\n"} {
				got, readErr := os.ReadFile(filepath.Join(vault.path, path))
				require.NoError(t, readErr)
				require.Equal(t, expected, string(got))
			}
		})
	}
}

func TestRenameHeadingApplyResultPreservesCommittedWarning(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, writeRenameHeadingApplyResult(&out, actions.RenameHeadingResult{
		Path: "target.md", Applied: true, Message: "no inbound heading references remain; files were saved, but recovery cleanup failed",
	}))
	require.Equal(t, "Renamed heading in target.md; rewrites: 0; upgraded: 0; skipped: 0\nno inbound heading references remain; files were saved, but recovery cleanup failed\n", out.String())
}
