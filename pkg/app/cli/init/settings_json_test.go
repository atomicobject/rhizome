package init

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/require"
)

// setUpRepo is a repository a machine first run set up with the
// recommendations: Agentic Engineering with Action items, Claude Code only.
func setUpRepo(t *testing.T) (string, config.Env) {
	t.Helper()
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv
	root := machineRunRepo(t)
	_, err := Apply(RunOptions{Env: env, Dir: root, Agents: "claude"})
	require.NoError(t, err)
	return root, env
}

func addon(t *testing.T, plan SetupPlan, id string) AddonOption {
	t.Helper()
	for _, option := range plan.Addons {
		if option.ID == id {
			return option
		}
	}
	t.Fatalf("no add-on %q in %+v", id, plan.Addons)
	return AddonOption{}
}

func agentEnabled(plan SetupPlan, id string) bool {
	for _, option := range plan.Agents {
		if option.ID == id {
			return option.Enabled
		}
	}
	return false
}

func TestSettingsPlanReportsAConfiguredFolderAsItIs(t *testing.T) {
	root, env := setUpRepo(t)

	plan, err := Plan(RunOptions{Env: env, Dir: root})
	require.NoError(t, err)

	require.True(t, plan.Configured)
	require.Equal(t, "agentic-engineering", plan.Workflow)
	require.Len(t, plan.Workflows, 3)
	require.True(t, addon(t, plan, templateActionItems).Enabled)
	// Optional add-ons are listed for a configured folder, so one that is
	// installed is never hidden.
	require.False(t, addon(t, plan, "compounding").Enabled)
	require.True(t, agentEnabled(plan, "claude"))
	require.False(t, agentEnabled(plan, "cursor"))
	require.Equal(t, searchOff, plan.Search.Provider)
	require.Len(t, plan.Search.Providers, 4)
	require.NotEmpty(t, plan.Pin)
	require.Empty(t, plan.Writes.Summary, "a folder just set up has nothing to change")
	require.Empty(t, plan.Writes.Files)
}

func TestSettingsPlanListsWhatOptionsWouldWriteWithoutWriting(t *testing.T) {
	root, env := setUpRepo(t)
	before, err := os.ReadFile(filepath.Join(root, ".rhizome", "workflows.yml"))
	require.NoError(t, err)

	plan, err := Plan(RunOptions{Env: env, Dir: root, Agents: "claude,cursor", AddAddon: "compounding", RemoveAddon: templateActionItems})
	require.NoError(t, err)

	require.True(t, agentEnabled(plan, "cursor"))
	require.True(t, addon(t, plan, "compounding").Enabled)
	require.False(t, addon(t, plan, templateActionItems).Enabled)
	require.NotContains(t, plan.Writes.Files, ".rhizome/.gitignore", "an unchanged support file is not listed")
	require.Contains(t, plan.Writes.Summary, "Agents: Claude Code → Claude Code, Cursor")
	require.Contains(t, plan.Writes.Files, ".rhizome/workflows.yml")
	require.Contains(t, plan.Writes.Files, ".cursor/rules/rhizome.mdc")

	after, err := os.ReadFile(filepath.Join(root, ".rhizome", "workflows.yml"))
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))
	require.NoFileExists(t, filepath.Join(root, ".cursor", "rules", "rhizome.mdc"))
}

func TestSettingsPlanListsSupportFilesApplyWrites(t *testing.T) {
	root, env := setUpRepo(t)
	require.NoError(t, os.Remove(filepath.Join(root, ".rhizome", ".gitignore")))

	plan, err := Plan(RunOptions{Env: env, Dir: root})
	require.NoError(t, err)
	require.Contains(t, plan.Writes.Files, ".rhizome/.gitignore")

	_, err = Apply(RunOptions{Env: env, Dir: root})
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(root, ".rhizome", ".gitignore"))
}

func TestSettingsApplyNamesRemovedFilesToCommit(t *testing.T) {
	root, env := setUpRepo(t)

	result, err := Apply(RunOptions{Env: env, Dir: root, Agents: "shared"})
	require.NoError(t, err)

	// Turning Claude Code off only removes files, which still need a commit.
	require.Empty(t, result.Created)
	require.Contains(t, result.Commit, ".claude/")
}

func TestSettingsApplyChangesTheRepositoryAndReportsIt(t *testing.T) {
	root, env := setUpRepo(t)

	result, err := Apply(RunOptions{Env: env, Dir: root, Workflow: "none", Agents: "claude,cursor", Search: searchOff, AddAddon: "compounding"})
	require.NoError(t, err)

	require.NotEmpty(t, result.Summary)
	require.Contains(t, result.Summary, "Agents: Claude Code, Cursor")
	require.Contains(t, result.Commit, ".rhizome/")
	require.FileExists(t, filepath.Join(root, ".cursor", "rules", "rhizome.mdc"))
	require.Empty(t, result.Pin.Error)

	plan, err := Plan(RunOptions{Env: env, Dir: root})
	require.NoError(t, err)
	require.Equal(t, "none", plan.Workflow)
	require.True(t, addon(t, plan, "compounding").Enabled)
	require.True(t, agentEnabled(plan, "cursor"))
	require.Equal(t, searchOff, plan.Search.Provider)
	require.Empty(t, plan.Writes.Summary, "the applied settings are now the repository's own")

	again, err := Apply(RunOptions{Env: env, Dir: root})
	require.NoError(t, err)
	require.Equal(t, []string{"Everything is up to date."}, again.Summary)
	require.Empty(t, again.Created)
	require.Empty(t, again.Updated)
}
