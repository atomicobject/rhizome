package cmd

import vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/app/agentapi"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestFreshCommandTreesPreserveInvocationEnvAndDefaults(t *testing.T) {
	first := setupAgentTestVault(t, map[string]string{
		"Shared.md": "# Shared\n\nbody from first vault\n", "FirstOnly.md": "# First only\n",
	})
	second := setupAgentTestVault(t, map[string]string{"Shared.md": "# Shared\n\nbody from second vault\n"})
	// Both trees exist before either parses flags or executes.
	a := newRootCommand(commandEnv{Config: first.env})
	b := newRootCommand(commandEnv{Config: second.env})
	run := func(app *commandInvocation, args ...string) agentapi.FilesResponse {
		ctx, cancel := context.WithCancel(vaultconfig.WithEnv(context.Background(), second.env))
		defer cancel()
		stdout, stderr, err := runCommandInvocation(t, app, ctx, args, true)
		require.NoError(t, err, stderr)
		var response agentapi.FilesResponse
		require.NoError(t, json.Unmarshal([]byte(stdout), &response), stdout)
		return response
	}
	aResult := run(a, "agent", "--vault", first.name, "--session", "shared-session", "files",
		"--input", "Shared.md", "--input", "FirstOnly.md", "--include-content", "true")
	require.Len(t, aResult.Files, 2)
	var shared string
	for _, file := range aResult.Files {
		if file.Path == "Shared.md" {
			shared = file.Content
		}
	}
	require.Contains(t, shared, "body from first vault")

	bResult := run(b, "agent", "--vault", second.name, "--session-id", "shared-session", "files",
		"--input", "Shared.md", "--include-content", "true", "--dedupe=false")
	require.Len(t, bResult.Files, 1)
	require.Equal(t, "Shared.md", bResult.Files[0].Path)
	require.Contains(t, bResult.Files[0].Content, "body from second vault")

	repeated := run(newRootCommand(commandEnv{Config: first.env}), "agent", "--vault", first.name,
		"--session-id", "shared-session", "files", "--input", "Shared.md", "--include-content", "true")
	require.Len(t, repeated.Files, 1)
	require.Equal(t, "Shared.md", repeated.Files[0].Path)
	require.Equal(t, "deduped", repeated.Files[0].ContentOmittedReason)
	explicitFalse := run(newRootCommand(commandEnv{Config: first.env}), "agent", "--vault", first.name,
		"--session", "shared-session", "files", "--input", "Shared.md", "--include-content", "true", "--dedupe=false")
	require.Len(t, explicitFalse.Files, 1)
	require.Contains(t, explicitFalse.Files[0].Content, "body from first vault")
}

func TestFreshGraphTreesOwnSliceFlags(t *testing.T) {
	first := setupAgentTestVault(t, map[string]string{
		"First.md": "# First\n", "Second.md": "# Second\n", "Third.md": "# Third\n", "Fourth.md": "# Fourth\n",
	})
	second := setupAgentTestVault(t, map[string]string{"Other.md": "# Other\n", "Another.md": "# Another\n"})
	a := newRootCommand(commandEnv{Config: first.env})
	b := newRootCommand(commandEnv{Config: second.env})
	run := func(app *commandInvocation, args ...string) string {
		ctx, cancel := context.WithCancel(vaultconfig.WithEnv(context.Background(), second.env))
		defer cancel()
		stdout, stderr, err := runCommandInvocation(t, app, ctx, args, true)
		require.NoError(t, err, stderr)
		return stdout
	}
	output := run(a, "graph", "--vault", first.name, "--min-degree", "0", "--no-color",
		"--exclude", "First.md,Second.md", "--exclude", "Third.md", "degrees")
	require.Contains(t, output, "Nodes: 1")
	require.Contains(t, output, "Fourth.md")
	output = run(b, "graph", "--vault", second.name, "--min-degree", "0", "--no-color", "degrees")
	require.Contains(t, output, "Nodes: 2")
	require.Contains(t, output, "Other.md")
	output = run(newRootCommand(commandEnv{Config: first.env}), "graph", "--vault", first.name, "--min-degree", "0", "--no-color", "degrees")
	require.Contains(t, output, "Nodes: 4")
	require.Contains(t, output, "First.md")
}

func TestFreshCommandTreesPreserveRequiredLocalVault(t *testing.T) {
	vault := setupAgentTestVault(t, nil)
	missing := newRootCommand(commandEnv{Config: vault.env})
	_, _, err := runCommandInvocation(t, missing, vaultconfig.WithEnv(context.Background(), vault.env), []string{"graph", "ignore", "archive/**"}, true)
	require.ErrorContains(t, err, `required flag(s) "vault" not set`)

	supplied := newRootCommand(commandEnv{Config: vault.env})
	stdout, stderr, err := runCommandInvocation(t, supplied, vaultconfig.WithEnv(context.Background(), vault.env), []string{"graph", "ignore", "archive/**", "--vault", vault.name}, true)
	require.NoError(t, err, stderr)
	require.Contains(t, stdout, "Saved graph ignore patterns")
	parentPosition := newRootCommand(commandEnv{Config: vault.env})
	_, stderr, err = runCommandInvocation(t, parentPosition, vaultconfig.WithEnv(context.Background(), vault.env), []string{"graph", "--vault", vault.name, "ignore", "archive/**"}, true)
	require.NoError(t, err, stderr)
	config, err := repoconfig.Load(vault.path)
	require.NoError(t, err)
	require.Equal(t, []string{"archive/**"}, config.Graph.Ignore)
}

func TestCommandInvocationKeepsFactoryEnvAndExecutionCancellation(t *testing.T) {
	env := testkit.Env(t)
	app := newRootCommand(commandEnv{Config: env})
	ctx, cancel := context.WithCancel(context.Background())
	var output bytes.Buffer
	app.SetOut(&output)
	app.SetErr(&output)
	app.SilenceErrors, app.SilenceUsage = true, true
	app.AddCommand(&cobra.Command{Use: "cancel-fixture", RunE: func(command *cobra.Command, _ []string) error {
		require.Equal(t, env, commandEnvFromContext(command.Context()).Config)
		cancel()
		<-command.Context().Done()
		return command.Context().Err()
	}})
	app.SetArgs([]string{"cancel-fixture"})
	require.ErrorIs(t, app.ExecuteContext(ctx), context.Canceled)
	require.True(t, errors.Is(ctx.Err(), context.Canceled))
}
