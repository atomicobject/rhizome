package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/validate"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/require"
)

func TestBuiltinCommandNamesMatchRootCommands(t *testing.T) {
	app := newRootCommand(commandEnv{})
	names := []string{"help", "completion"}
	for _, sub := range app.Commands() {
		names = append(names, sub.Name())
		names = append(names, sub.Aliases...)
	}
	sort.Strings(names)
	want := append([]string(nil), validate.BuiltinCommandNames...)
	sort.Strings(want)
	require.Equal(t, want, names, "update validate.BuiltinCommandNames when root commands change")
}

func writeModuleRecipe(t *testing.T, vaultPath, module string) {
	t.Helper()
	dir := filepath.Join(vaultPath, ".rhizome", "modules", module, "recipes")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "project.md"), []byte(projectRecipeMarkdown()), 0o644))
}

func TestModuleCommandListsAndRunsRecipes(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())
	writeModuleRecipe(t, vault.path, "demo")
	ctx := vaultconfig.WithEnv(context.Background(), vault.env)

	stdout, _, err := runRootCLI(t, ctx, []string{"demo"})
	require.NoError(t, err)
	require.Contains(t, stdout, "project-by-path")
	require.Contains(t, stdout, "Load one project and its decisions")

	stdout, _, err = runRootCLI(t, ctx, []string{"--no-pager", "demo"})
	require.NoError(t, err, "root flags before the module name still dispatch")
	require.Contains(t, stdout, "project-by-path")

	stdout, _, err = runRootCLI(t, ctx, []string{"demo", "project-by-path", "--help"})
	require.NoError(t, err)
	require.Contains(t, stdout, "--path string")
	require.Contains(t, stdout, "(required)")

	stdout, _, err = runRootCLI(t, ctx, []string{"demo", "project-by-path", "--path", "notes/projects/roadmap-refresh.md", "--vault", vault.name})
	require.NoError(t, err)
	var resp struct {
		Variables map[string]any       `json:"variables"`
		Result    ontologyquery.Result `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
	require.Equal(t, map[string]any{"path": "notes/projects/roadmap-refresh.md"}, resp.Variables)
	rows := resp.Result.Data["project"].([]any)
	require.Len(t, rows, 1)
	require.Equal(t, "Roadmap Refresh", rows[0].(map[string]any)["name"])

	_, _, err = runRootCLI(t, ctx, []string{"demo", "missing-recipe"})
	require.ErrorContains(t, err, `unknown command "missing-recipe"`)
}

func TestModuleCommandBuiltinsWin(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())
	writeModuleRecipe(t, vault.path, "validate")
	writeModuleRecipe(t, vault.path, "demo")
	ctx := vaultconfig.WithEnv(context.Background(), vault.env)
	app := newRootCommand(commandEnvFromContext(ctx))

	_, ok := app.moduleDispatchArgs(ctx, []string{"validate", "project-by-path"})
	require.False(t, ok, "a module named like a built-in must not be dispatched")
	routed, ok := app.moduleDispatchArgs(ctx, []string{"demo", "project-by-path"})
	require.True(t, ok)
	require.Equal(t, []string{"module", "demo", "project-by-path"}, routed)
	_, ok = app.moduleDispatchArgs(ctx, []string{"nosuchmodule"})
	require.False(t, ok)

	stdout, _, err := runRootCLI(t, ctx, []string{"module", "validate"})
	require.NoError(t, err)
	require.Contains(t, stdout, "project-by-path")

	_, _, err = runRootCLI(t, ctx, []string{"nosuchmodule"})
	require.ErrorContains(t, err, `unknown command "nosuchmodule"`)
	_, _, err = runRootCLI(t, ctx, []string{"module", "nosuchmodule"})
	require.ErrorContains(t, err, `no module named "nosuchmodule"`)
}

func TestModuleVaultFlagMatchesCobraForms(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"recipe", "--vault", "other"}, "other"},
		{[]string{"recipe", "--vault=other"}, "other"},
		{[]string{"recipe", "-v", "other"}, "other"},
		{[]string{"recipe", "-v=other"}, "other"},
		{[]string{"recipe", "-vother"}, "other"},
		{[]string{"recipe", "--verbose-thing"}, ""},
		{[]string{"recipe", "--", "-vother"}, ""},
	} {
		require.Equal(t, tc.want, moduleVaultFlag(tc.args), "%v", tc.args)
	}
}
