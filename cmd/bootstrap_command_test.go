package cmd

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestBootstrapCommandPassesOptionsWithFreshDefaults(t *testing.T) {
	for _, name := range []string{"Claude", "Codex"} {
		t.Run(name, func(t *testing.T) {
			var got bootstrap.CodexParams
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			runnerErr := errors.New("runner failed")
			construct := func() *cobra.Command {
				command := newBootstrapCommand(name, func(actual context.Context, params bootstrap.CodexParams) error {
					require.Same(t, ctx, actual)
					require.NotNil(t, params.OnboardInstructions)
					params.OnboardInstructions = nil
					got = params
					return runnerErr
				})
				command.SetOut(io.Discard)
				command.SetErr(io.Discard)
				return command
			}
			command := construct()
			command.SetArgs([]string{"  investigate", "this  ", "-v", "selected", "-q", "one", "--query", "two", "-f", "a.go", "--file", "notes", "--profile", "thinking", "--model", "custom", "--no-auto", "--debug-all", "--budget-chars", "1234"})
			require.ErrorIs(t, command.ExecuteContext(ctx), runnerErr)
			require.Equal(t, bootstrap.CodexParams{Task: "investigate this", Queries: []string{"one", "two"}, Files: []string{"a.go", "notes"}, Profile: "thinking", Model: "custom", NoAuto: true, BudgetChars: 1234, VaultName: "selected", Debug: true, DebugAll: true}, got)
			debug, err := command.Flags().GetBool("debug")
			require.NoError(t, err)
			require.True(t, debug)

			command = construct()
			command.SetArgs([]string{"next", "--vault", "other"})
			require.ErrorIs(t, command.ExecuteContext(ctx), runnerErr)
			require.Equal(t, bootstrap.CodexParams{Task: "next", Profile: "instant", VaultName: "other"}, got)
		})
	}
}

func TestBootstrapCommandsKeepIndependentOptions(t *testing.T) {
	var first, second bootstrap.CodexParams
	claude := newBootstrapCommand("Claude", func(_ context.Context, p bootstrap.CodexParams) error { first = p; return nil })
	codex := newBootstrapCommand("Codex", func(_ context.Context, p bootstrap.CodexParams) error { second = p; return nil })
	claude.SetArgs([]string{"one", "-v", "first", "-q", "claude-only", "--debug-all"})
	require.NoError(t, claude.Execute())
	codex.SetArgs([]string{"two", "-v", "second"})
	require.NoError(t, codex.Execute())
	require.Equal(t, []string{"claude-only"}, first.Queries)
	require.Equal(t, "first", first.VaultName)
	require.True(t, first.Debug)
	require.NotNil(t, first.OnboardInstructions)
	require.NotNil(t, second.OnboardInstructions)
	second.OnboardInstructions = nil
	require.Equal(t, bootstrap.CodexParams{Task: "two", Profile: "instant", VaultName: "second"}, second)
}

func TestBootstrapCommandResolvesDefaultVaultBeforeDebugState(t *testing.T) {
	env := testkit.Env(t)
	env.HomeDir, env.ConfigDir, env.CLIConfigFile = "", "", ""
	env.WorkingDir = t.TempDir()
	ctx := vaultconfig.WithEnv(context.Background(), env)
	var got bootstrap.CodexParams
	calls := 0
	construct := func() *cobra.Command {
		command := newBootstrapCommand("Codex", func(_ context.Context, p bootstrap.CodexParams) error { calls++; got = p; return nil })
		command.SetOut(io.Discard)
		command.SetErr(io.Discard)
		return command
	}
	command := construct()
	command.SetArgs([]string{})
	require.ErrorContains(t, command.ExecuteContext(ctx), "requires at least 1 arg(s)")
	require.Zero(t, calls)
	command = construct()
	command.SetArgs([]string{"task", "--debug-all"})
	require.EqualError(t, command.ExecuteContext(ctx), vaultconfig.UserConfigDirectoryNotFoundErrorMessage)
	require.Zero(t, calls)
	debug, err := command.Flags().GetBool("debug")
	require.NoError(t, err)
	require.False(t, debug, "default lookup failure precedes debug-all promotion")

	require.NoError(t, os.Mkdir(filepath.Join(env.WorkingDir, ".rhizome"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(env.WorkingDir, ".rhizome", "config.yml"), []byte("{}\n"), 0644))
	command = construct()
	command.SetArgs([]string{"task"})
	require.NoError(t, command.ExecuteContext(ctx))
	require.Equal(t, env.WorkingDir, got.VaultName)
	selected, err := command.Flags().GetString("vault")
	require.NoError(t, err)
	require.Empty(t, selected, "resolved default does not change the explicit vault flag")
	require.False(t, got.Debug)
	require.False(t, got.DebugAll)
}

func TestBootstrapCommandComposesBundledOnboardingLoader(t *testing.T) {
	command := newBootstrapCommand("Codex", func(_ context.Context, params bootstrap.CodexParams) error {
		require.NotNil(t, params.OnboardInstructions)
		instructions, err := params.OnboardInstructions()
		require.NoError(t, err)
		require.NotEmpty(t, strings.TrimSpace(instructions))
		return nil
	})
	command.SetArgs([]string{"task", "--vault", "selected"})
	require.NoError(t, command.Execute())
}
