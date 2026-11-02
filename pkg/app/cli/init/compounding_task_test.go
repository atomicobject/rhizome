package init

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/atomicobject/rhizome/pkg/vault/taskdef"
)

// The compounding add-on installs its mining task as a module task that the
// task loader accepts (SPEC-0124 US8).
func TestCompoundingInstallsTheMineTask(t *testing.T) {
	root := t.TempDir()
	writeProjectConfig(t, root, "notes:\n  includes: [\"**/*.md\"]\n", repoconfig.LocalWorkflowConfig{})
	_, err := runInit(t, RunOptions{Dir: root, AddAddon: templateCompounding})
	require.NoError(t, err)

	rel := ".rhizome/modules/compounding/tasks/mine.yml"
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	require.NoError(t, err)
	def, err := taskdef.ParseDefinition(rel, data)
	require.NoError(t, err)
	require.Equal(t, "compounding/mine", def.ID)
	require.Equal(t, taskdef.ScopePerson, def.Scope)
	require.Equal(t, 7*24*time.Hour, def.Cadence)
	require.Equal(t, []string{"node", ".rhizome/modules/compounding/scripts/task-check.mjs"}, def.Check.Command)
	require.FileExists(t, filepath.Join(root, ".rhizome", "modules", "compounding", "scripts", "task-check.mjs"))
	requireUpToDate(t, root)
}

// The mining task's prompt mines in task mode with a fixed budget, then
// reports.
func TestCompoundingMineTaskPromptMinesInTaskModeThenReports(t *testing.T) {
	const source = "templates/starters/compounding/module/tasks/mine.yml"
	data, err := agentHelperTemplates.ReadFile(source)
	require.NoError(t, err)
	def, err := taskdef.ParseDefinition(".rhizome/modules/compounding/tasks/mine.yml", data)
	require.NoError(t, err)
	require.Contains(t, def.Prompt, "mine.mjs --harness <h> --task-mode --budget 10")
	require.Contains(t, def.Prompt, "scripts/report.mjs")
}

// The run's final message renders as Markdown on the Tasks page, so it links
// to the workspace.
func TestCompoundingMineTaskPromptLinksToTheWorkspace(t *testing.T) {
	data, err := agentHelperTemplates.ReadFile("templates/starters/compounding/module/tasks/mine.yml")
	require.NoError(t, err)
	def, err := taskdef.ParseDefinition(".rhizome/modules/compounding/tasks/mine.yml", data)
	require.NoError(t, err)
	require.Contains(t, def.Prompt, "[Review in the compounding workspace](/notes?view=workspace)")
}

// The workspace ships one action per workspace.mjs operation, each running
// that operation, and its two custom views (SPEC-0124 US9).
func TestCompoundingTemplateShipsTheWorkspaceActionsAndViews(t *testing.T) {
	const module = "templates/starters/compounding/module/"
	for _, op := range []string{"recommendations", "item", "prompt", "dismiss", "hide", "health", "shards-check"} {
		data, err := agentHelperTemplates.ReadFile(module + "actions/" + op + ".yaml")
		require.NoError(t, err, op)
		require.Contains(t, string(data), `command: [node, "{module}/scripts/workspace.mjs", "`+op+`"]`, op)
	}
	for _, file := range []string{"scripts/workspace.mjs", "views/workspace/workspace.yaml", "views/workspace/workspace.tsx", "views/workspace/compounding-item.yaml", "views/workspace/item.tsx", "views/workspace/parts.tsx"} {
		_, err := agentHelperTemplates.ReadFile(module + file)
		require.NoError(t, err, file)
	}
	meta, err := agentHelperTemplates.ReadFile("templates/starters/compounding/template.yaml")
	require.NoError(t, err)
	require.Contains(t, string(meta), "  - actions\n")
}
