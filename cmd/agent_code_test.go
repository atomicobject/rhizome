package cmd

import (
	"context"
	"encoding/json"
	"github.com/atomicobject/rhizome/internal/testkit"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestAgentCodeSelectiveCLI(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{"note.md": "# Fixture\nExact evidence.\n"})
	for _, args := range [][]string{
		{"agent", "code", "describe", "--operation", "files", "--vault", vault.name},
		{"agent", "code", "surface", "--vault", vault.name},
	} {
		stdout, _, err := runRootCLI(t, vaultconfig.WithEnv(context.Background(), vault.env), args)
		require.NoError(t, err)
		require.True(t, json.Valid([]byte(stdout)), stdout)
	}
	for _, args := range [][]string{
		{"agent", "code", "describe"},
		{"agent", "code", "describe", "--operation", "not_an_operation"},
		{"agent", "code", "generate", "--operation", "files"},
	} {
		_, _, err := runRootCLI(t, vaultconfig.WithEnv(context.Background(), vault.env), args)
		require.Error(t, err)
	}
}

func TestCodeModeCoversEveryExecutableAgentTask(t *testing.T) {
	rootCmd := newRootCommand(commandEnv{Getwd: os.Getwd, Config: testkit.Env(t)})
	agentCmd, _, _ := rootCmd.Find([]string{"agent"})

	covered := map[string]string{}
	for _, descriptor := range agentapi.CodeOperationDescriptors() {
		for _, path := range descriptor.CodeCLILeaves {
			require.Empty(t, covered[path], "duplicate coverage for %s", path)
			covered[path] = descriptor.Name
		}
	}
	controls := map[string]bool{
		"agent code surface": true, "agent code describe": true,
		"agent code generate": true, "agent code serve": true, "agent code execute": true,
		"agent current-user": true, // Help-only parent, not a task operation.
	}
	var inspect func(*cobra.Command)
	inspect = func(command *cobra.Command) {
		if command != agentCmd && (command.Run != nil || command.RunE != nil) {
			path := runtimePlanCommandPath(command)
			if !controls[path] {
				require.NotEmpty(t, covered[path], "executable agent operation has no code-mode contract: %s", path)
				delete(covered, path)
			}
		}
		for _, child := range command.Commands() {
			inspect(child)
		}
	}
	inspect(agentCmd)
	require.Equal(t, "note_move", covered["note move"])
	delete(covered, "note move")
	require.Empty(t, covered, "catalog contains stale or non-executable CLI coverage")
}

func TestCanonicalAgentCodeVaultPathResolvesSymlink(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	vault := setupAgentTestVault(t, map[string]string{"note.md": "# Fixture\n"})
	// Obsidian-config lookup matches the registered path suffix, so the link name is the vault name.
	linkRoot := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(vault.path, linkRoot); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	configFile := filepath.Join(t.TempDir(), "obsidian.json")
	configBody, err := json.Marshal(map[string]any{"vaults": map[string]any{"link-id": map[string]string{"path": linkRoot}}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configFile, configBody, 0o644))
	env.ObsidianConfigFile = configFile

	output := filepath.Join(t.TempDir(), "artifacts")
	stdout, stderr, err := runRootCLI(t, vaultconfig.WithEnv(context.Background(), env), []string{"agent", "code", "generate", "--operation", "files", "--output", output, "--vault", "linked"})
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	var manifest agentcode.Manifest
	require.NoError(t, json.Unmarshal([]byte(stdout), &manifest))
	realRoot, err := filepath.EvalSymlinks(vault.path)
	require.NoError(t, err)
	// Manifests carry the canonical slash-separated vault root.
	require.Equal(t, filepath.ToSlash(realRoot), manifest.VaultPath)
}

func TestAgentCodeFrontsDeclareRuntimeFreeAuthority(t *testing.T) {
	for _, operation := range []string{"surface", "describe", "generate", "execute"} {
		declaration, found := oneshotruntime.DefaultRegistry().Declaration(oneshotruntime.OperationID("agent.code." + operation))
		require.True(t, found)
		require.NotNil(t, declaration.StaticPlan)
		require.False(t, declaration.StaticPlan.RequiresRuntime())
		if operation == "generate" || operation == "execute" {
			require.Equal(t, oneshotruntime.AuthorityWriteCapable, declaration.Authority)
		} else {
			require.Equal(t, oneshotruntime.AuthorityReadOnly, declaration.Authority)
		}
	}
}
