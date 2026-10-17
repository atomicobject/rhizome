package cmd

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"github.com/atomicobject/rhizome/pkg/paths"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestInvocationWorkingDirectoryReachesUpdateRunner(t *testing.T) {
	first, second := testkit.Env(t), testkit.Env(t)
	a, b := newRootCommand(commandEnv{Config: first}), newRootCommand(commandEnv{Config: second})
	processCWD, err := os.Getwd()
	require.NoError(t, err)
	previous := runUpdate
	t.Cleanup(func() { runUpdate = previous })
	var received []appupdate.Options
	runUpdate = func(_ context.Context, options appupdate.Options) (appupdate.Result, error) {
		received = append(received, options)
		return appupdate.Result{}, nil
	}
	_, _, err = runCommandInvocation(t, a, context.Background(), []string{"update", "--pinned", "--yes"}, true)
	require.NoError(t, err)
	_, _, err = runCommandInvocation(t, b, context.Background(), []string{"update", "--set-version", "v0.1.0"}, true)
	require.NoError(t, err)
	require.Len(t, received, 2)
	require.Equal(t, first.WorkingDir, received[0].WorkDir)
	require.True(t, received[0].Pinned)
	require.True(t, received[0].Yes)
	require.Equal(t, second.WorkingDir, received[1].WorkDir)
	require.Equal(t, "v0.1.0", received[1].SetVersion)
	require.False(t, received[1].Pinned)
	require.False(t, received[1].Yes)
	after, err := os.Getwd()
	require.NoError(t, err)
	require.Equal(t, processCWD, after)
}

func TestInvocationWorkingDirectoryAdmitsMCPAtItsOwnVaultRoot(t *testing.T) {
	first, second := setupAgentTestVault(t, nil), setupAgentTestVault(t, nil)
	a, b := newRootCommand(commandEnv{Config: first.env}), newRootCommand(commandEnv{Config: second.env})
	previous := backgroundEnsure
	backgroundEnsure = func(context.Context, obsidian.VaultDefinition) {}
	t.Cleanup(func() { backgroundEnsure = previous })
	input := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"}\n"
	for _, app := range []*commandInvocation{a, b} {
		app.SetIn(strings.NewReader(input))
		stdout, stderr, err := runCommandInvocation(t, app, context.Background(), []string{"mcp", "serve", "--tools", "check_paths"}, true)
		require.NoError(t, err, stderr)
		require.Contains(t, stdout, `"name":"check_paths"`)
	}
	wrong := second.env
	wrong.WorkingDir = first.path
	app := newRootCommand(commandEnv{Config: wrong})
	app.SetIn(strings.NewReader(input))
	_, _, err := runCommandInvocation(t, app, context.Background(), []string{"mcp", "serve", "--vault", second.path, "--tools", "check_paths"}, true)
	require.ErrorContains(t, err, "MCP serve must run from its configured vault root")
	require.ErrorContains(t, err, paths.ResolveSymlinks(second.path).String())
}

func TestInvocationWorkingDirectorySelectsTrustTargetBeforeStoreAccess(t *testing.T) {
	first, second := testkit.Env(t), testkit.Env(t)
	for i, env := range []vaultconfig.Env{first, second} {
		configPath := filepath.Join(env.WorkingDir, ".rhizome", "config.yml")
		require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
		require.NoError(t, os.WriteFile(configPath, []byte("rhizome:\n  binaryManager: unsupported-fixture\n"), 0o644))
		app := newRootCommand(commandEnv{Config: env})
		command := []string{"trust", "untrust"}[i]
		_, _, err := runCommandInvocation(t, app, context.Background(), []string{command}, true)
		resolvedConfigPath := filepath.Join(paths.ResolveSymlinks(env.WorkingDir).String(), ".rhizome", "config.yml")
		require.ErrorContains(t, err, resolvedConfigPath)
		require.ErrorContains(t, err, `unsupported rhizome.binaryManager "unsupported-fixture"`)
	}
}

func TestInvocationWorkingDirectorySelectsDesktopTargetAndOverride(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("desktop command is macOS-only")
	}
	first, second := setupAgentTestVault(t, nil), setupAgentTestVault(t, nil)
	for _, vault := range []agentTestVault{first, second} {
		env := vault.env
		env.LookupEnv = func(key string) (string, bool) { return "Missing.app", key == "RZM_DESKTOP_APP" }
		app := newRootCommand(commandEnv{Config: env})
		_, _, err := runCommandInvocation(t, app, context.Background(), []string{"desktop"}, true)
		require.ErrorContains(t, err, filepath.Join(vault.path, "Missing.app"))
	}
}
