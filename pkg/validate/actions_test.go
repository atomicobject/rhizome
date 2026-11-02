package validate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunActionsReportsEachDeclarationProblem(t *testing.T) {
	root := t.TempDir()
	module := filepath.Join(root, ".rhizome", "modules", "demo")
	files := map[string]string{
		"scripts/run.mjs":              "// present\n",
		"recipes/shared.yaml":          "apiVersion: rhizome.query-recipe.v1\nid: shared\nname: Shared\nproblem: Collides with an action.\nquery:\n  graphQL: |\n    query { types { name } }\n",
		"actions/good.yaml":            "description: Fine\ninputs:\n  items: {kind: list, enum: [a, b]}\nrun:\n  command: [node, \"{module}/scripts/run.mjs\"]\n",
		"actions/broken.yaml":          "description: [unclosed\n",
		"actions/no-description.yaml":  "run:\n  command: [true]\n",
		"actions/bad-kind.yaml":        "description: x\ninputs:\n  n: {kind: number}\nrun:\n  command: [true]\n",
		"actions/bool-enum.yaml":       "description: x\ninputs:\n  on: {kind: bool, enum: [yes]}\nrun:\n  command: [true]\n",
		"actions/no-run.yaml":          "description: x\n",
		"actions/two-runs.yaml":        "description: x\nrun:\n  command: [true]\n  script: scripts/run.mjs\n",
		"actions/missing-script.yaml":  "description: x\nrun:\n  script: scripts/gone.mjs\n",
		"actions/missing-command.yaml": "description: x\nrun:\n  command: [node, \"{module}/scripts/gone.mjs\"]\n",
		"actions/slow.yaml":            "description: x\nrun:\n  command: [true]\ntimeout: 6m\n",
		"actions/shared.yaml":          "description: x\nrun:\n  command: [true]\n",
	}
	for rel, content := range files {
		abs := filepath.Join(module, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
	}

	result := RunActions(RunContext{VaultPath: root})
	require.False(t, result.OK)
	require.Empty(t, result.Error)
	got := map[string]string{}
	for _, issue := range result.Issues {
		require.Equal(t, IssueCodeInvalidActionFile, issue.Code)
		require.Empty(t, got[issue.Path], "one issue per file in this fixture: %s", issue.Path)
		got[issue.Path] = issue.Message
	}
	at := func(name string) string { return got[".rhizome/modules/demo/actions/"+name+".yaml"] }
	require.Contains(t, at("broken"), "cannot parse")
	require.Equal(t, "description is required", at("no-description"))
	require.Contains(t, at("bad-kind"), `unknown kind "number"`)
	require.Contains(t, at("bool-enum"), "an enum needs kind string or list")
	require.Equal(t, "run must hold command or script", at("no-run"))
	require.Equal(t, "run must hold one of command or script, not both", at("two-runs"))
	require.Equal(t, ".rhizome/modules/demo/scripts/gone.mjs does not exist", at("missing-script"))
	require.Equal(t, ".rhizome/modules/demo/scripts/gone.mjs does not exist", at("missing-command"))
	require.Contains(t, at("slow"), "over the 5m0s limit")
	require.Contains(t, at("shared"), "name of a query recipe")
	require.Empty(t, at("good"))
	require.Len(t, got, 10)
	require.Equal(t, 10, result.IssueCount)

	finalized := finalizeCheckResult(CheckActions, result, 0, 3)
	require.Len(t, finalized.Issues, 3)
	require.Equal(t, 10, finalized.IssueCount)
}

func TestRunActionsWithoutActionsIsNotApplicable(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "modules", "demo", "recipes"), 0o755))
	result := RunActions(RunContext{VaultPath: root})
	require.True(t, result.OK)
	require.Equal(t, "no module actions found", result.NotApplicable)
}
