package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	moduleactions "github.com/atomicobject/rhizome/pkg/app/actions"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/actiondef"
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
		{[]string{"recipe", "--vault", "first", "-v", "last"}, "last"},
		{[]string{"recipe", "--query", "-verbose"}, ""},
		{[]string{"recipe", "--query=-verbose", "-v", "other"}, "other"},
		{[]string{"recipe", "--summary", "-v", "other"}, "other"},
	} {
		require.Equal(t, tc.want, moduleVaultFlag(tc.args), "%v", tc.args)
	}
}

// trustActionsForTest makes every action runner trust the repository.
func trustActionsForTest(t *testing.T) {
	t.Helper()
	previous := newActionRunner
	t.Cleanup(func() { newActionRunner = previous })
	newActionRunner = func(root string) *moduleactions.Runner {
		return &moduleactions.Runner{RepoRoot: root, Trust: func(context.Context) string { return "" }}
	}
}

func moduleActionFiles() map[string]string {
	return map[string]string{
		".rhizome/modules/demo/actions/echo.yaml":   "description: Echo the input\ninputs:\n  items: {kind: list, required: true}\n  count: {kind: int}\n  dry: {kind: bool}\n  summary: {kind: string}\nrun:\n  command: [cat]\n",
		".rhizome/modules/demo/actions/refuse.yaml": "description: Always refuse\nrun:\n  command: [sh, -c, \"echo busy >&2; exit 2\"]\n",
		".rhizome/modules/demo/actions/fail.yaml":   "description: Always fail\nrun:\n  command: [sh, -c, \"echo boom >&2; exit 1\"]\n",
	}
}

func TestModuleCommandRunsActionsWithExitCodes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh and cat")
	}
	trustActionsForTest(t)
	files := ontologyCommandFiles()
	for rel, body := range moduleActionFiles() {
		files[rel] = body
	}
	vault := setupAgentTestVault(t, files)
	writeModuleRecipe(t, vault.path, "demo")
	ctx := vaultconfig.WithEnv(context.Background(), vault.env)

	stdout, _, err := runRootCLI(t, ctx, []string{"demo"})
	require.NoError(t, err)
	require.Contains(t, stdout, "Recipes:")
	require.Contains(t, stdout, "project-by-path")
	require.Contains(t, stdout, "Actions:")
	require.Contains(t, stdout, "Echo the input")

	// A bool takes its value; an input may share a recipe-only flag's name.
	stdout, _, err = runRootCLI(t, ctx, []string{"demo", "echo", "--items", "a", "--items", "b", "--count", "3", "--dry", "true", "--summary", "s"})
	require.NoError(t, err)
	require.JSONEq(t, `{"items": ["a", "b"], "count": 3, "dry": true, "summary": "s"}`, stdout)
	// --vault after a bool input still chooses the vault, which has no demo module.
	_, _, err = runRootCLI(t, ctx, []string{"demo", "echo", "--items", "a", "--dry", "false", "--vault", "no-such-vault"})
	require.ErrorContains(t, err, `unknown command "demo"`)

	// Without the test driver's silencing, a refusal prints only its reason.
	stdout, stderr, err := runRootCLIWithRootSilence(t, ctx, []string{"demo", "refuse"}, false)
	require.Equal(t, silentExitError{code: 2}, err)
	require.Equal(t, "busy", stderr)
	require.Empty(t, stdout)

	_, stderr, err = runRootCLI(t, ctx, []string{"demo", "fail"})
	require.Equal(t, silentExitError{code: 1}, err)
	require.Equal(t, "exit status 1\nboom", stderr)

	_, _, err = runRootCLI(t, ctx, []string{"demo", "echo"})
	require.ErrorIs(t, err, actiondef.ErrInvalidInput, "a missing required input starts nothing")
	_, _, err = runRootCLI(t, ctx, []string{"demo", "echo", "--items", "a", "--count", "x"})
	require.ErrorIs(t, err, actiondef.ErrInvalidInput)
}

func TestActionOperationsRunThroughTheCodeCatalog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses cat")
	}
	trustActionsForTest(t)
	vault := setupAgentTestVault(t, moduleActionFiles())
	ctx := vaultconfig.WithEnv(context.Background(), vault.env)
	options := &vaultCommandOptions{root: newRootCommand(commandEnvFromContext(ctx)).Command, vaultName: vault.name}

	outcome, handled := options.callAgentCodeLocal(ctx, "action_list", map[string]any{}, false)
	require.True(t, handled)
	require.True(t, outcome.OK, outcome.Stderr)
	listed, err := json.Marshal(outcome.Payload)
	require.NoError(t, err)
	require.Contains(t, string(listed), `"id":"demo/echo"`)

	outcome, handled = options.callAgentCodeLocal(ctx, "action_run", map[string]any{"id": "demo/echo", "input": map[string]any{"items": []any{"x"}}}, false)
	require.True(t, handled)
	require.True(t, outcome.OK, outcome.Stderr)
	payload, err := json.Marshal(outcome.Payload)
	require.NoError(t, err)
	require.JSONEq(t, `{"status":"ok","result":{"items":["x"]}}`, string(payload))

	outcome, _ = options.callAgentCodeLocal(ctx, "action_run", map[string]any{"id": "demo/refuse"}, false)
	require.True(t, outcome.OK, "a refusal is an envelope, not a failed call")
	payload, _ = json.Marshal(outcome.Payload)
	require.JSONEq(t, `{"status":"refused","reason":"busy"}`, string(payload))

	outcome, _ = options.callAgentCodeLocal(ctx, "action_run", map[string]any{"id": "demo/missing"}, false)
	require.False(t, outcome.OK)
	require.Contains(t, outcome.Stderr, "unknown action")
}
