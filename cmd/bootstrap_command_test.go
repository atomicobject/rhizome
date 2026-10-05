package cmd

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestBootstrapCommandPassesOptionsAndResets(t *testing.T) {
	oldVault := vaultName
	t.Cleanup(func() { vaultName = oldVault })
	for _, name := range []string{"Claude", "Codex"} {
		t.Run(name, func(t *testing.T) {
			var got bootstrap.CodexParams
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			runnerErr := errors.New("runner failed")
			command := newBootstrapCommand(name, func(actual context.Context, params bootstrap.CodexParams) error {
				require.Same(t, ctx, actual)
				got = params
				return runnerErr
			})
			command.SetOut(io.Discard)
			command.SetErr(io.Discard)
			command.SetArgs([]string{"  investigate", "this  ", "-v", "selected", "-q", "one", "--query", "two", "-f", "a.go", "--file", "notes", "--profile", "thinking", "--model", "custom", "--no-auto", "--debug-all", "--budget-chars", "1234"})
			require.ErrorIs(t, command.ExecuteContext(ctx), runnerErr)
			require.Equal(t, bootstrap.CodexParams{Task: "investigate this", Queries: []string{"one", "two"}, Files: []string{"a.go", "notes"}, Profile: "thinking", Model: "custom", NoAuto: true, BudgetChars: 1234, VaultName: "selected", Debug: true, DebugAll: true}, got)
			debug, err := command.Flags().GetBool("debug")
			require.NoError(t, err)
			require.True(t, debug, "debug-all updates the bound debug flag")

			resetFlags(command)
			command.SetArgs([]string{"next", "--vault", "other"})
			require.ErrorIs(t, command.ExecuteContext(ctx), runnerErr)
			require.Equal(t, bootstrap.CodexParams{Task: "next", Queries: []string{}, Files: []string{}, Profile: "instant", VaultName: "other"}, got)
		})
	}
}

func TestBootstrapCommandsKeepIndependentOptions(t *testing.T) {
	oldVault := vaultName
	t.Cleanup(func() { vaultName = oldVault })
	var first, second bootstrap.CodexParams
	claude := newBootstrapCommand("Claude", func(_ context.Context, p bootstrap.CodexParams) error { first = p; return nil })
	codex := newBootstrapCommand("Codex", func(_ context.Context, p bootstrap.CodexParams) error { second = p; return nil })
	claude.SetArgs([]string{"one", "-v", "vault", "-q", "claude-only", "--debug-all"})
	require.NoError(t, claude.Execute())
	codex.SetArgs([]string{"two", "-v", "vault"})
	require.NoError(t, codex.Execute())
	require.Equal(t, []string{"claude-only"}, first.Queries)
	require.True(t, first.Debug)
	require.Equal(t, bootstrap.CodexParams{Task: "two", Profile: "instant", VaultName: "vault"}, second)
}

func TestBootstrapCommandResolvesDefaultVaultBeforeDebugState(t *testing.T) {
	oldVault, oldConfig := vaultName, obsidian.CliConfigPath
	t.Cleanup(func() { vaultName = oldVault; obsidian.CliConfigPath = oldConfig })
	t.Chdir(t.TempDir())
	configErr := errors.New("preferences unavailable")
	reads := 0
	obsidian.CliConfigPath = func() (string, string, error) { reads++; return "", "", configErr }
	var got bootstrap.CodexParams
	calls := 0
	command := newBootstrapCommand("Codex", func(_ context.Context, p bootstrap.CodexParams) error { calls++; got = p; return nil })
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{})
	require.ErrorContains(t, command.Execute(), "requires at least 1 arg(s)")
	require.Zero(t, reads, "argument validation precedes default resolution")
	command.SetArgs([]string{"task", "--debug-all"})
	require.ErrorIs(t, command.Execute(), configErr)
	require.Zero(t, calls)
	debug, err := command.Flags().GetBool("debug")
	require.NoError(t, err)
	require.False(t, debug, "default lookup failure precedes debug-all promotion")

	require.NoError(t, os.Mkdir(".rhizome", 0755))
	require.NoError(t, os.WriteFile(filepath.Join(".rhizome", "config.yml"), []byte("{}\n"), 0644))
	command.SetArgs([]string{"task"})
	require.NoError(t, command.Execute())
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.Equal(t, cwd, got.VaultName)
	require.Empty(t, vaultName, "resolved default does not become the explicit vault flag")
	require.True(t, got.Debug)
	require.True(t, got.DebugAll)
	require.Equal(t, 1, reads)
}
