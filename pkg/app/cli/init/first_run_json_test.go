package init

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/credentials"
	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/version"
	"github.com/stretchr/testify/require"
)

func machineRunRepo(t *testing.T) string {
	t.Helper()
	root := firstRunRepo(t)
	writeFixtureFile(t, root, "testdata/case.go", "package testdata\n")
	return root
}

func planRule(scope Scope, pattern string) (ScopeRule, bool) {
	for _, rule := range scope.Rules {
		if rule.Layer == ignore.LayerRhizome && rule.Pattern == pattern {
			return rule, true
		}
	}
	return ScopeRule{}, false
}

func TestPlanReportsTheRecommendedSetupWithoutWriting(t *testing.T) {
	root := machineRunRepo(t)

	plan, err := Plan(RunOptions{Dir: root})
	require.NoError(t, err)

	require.Equal(t, SetupSchema, plan.Schema)
	require.False(t, plan.Configured)
	require.Equal(t, filepath.Base(root), plan.Name)
	require.Equal(t, "all Markdown (3 notes in docs/)", plan.Findings.Docs)
	require.Equal(t, "testdata/ (1 file)", plan.Findings.Skip)
	require.Equal(t, "agentic-engineering", plan.Workflow)
	require.Len(t, plan.Workflows, 3)
	require.True(t, plan.Workflows[0].Recommended)
	require.Equal(t, []AddonOption{{
		ID: templateActionItems, Label: "Action items",
		Description: "Track #action-item commitments in notes, with assignees and due dates",
		DefaultFor:  []string{"agentic-engineering", "domain"}, Enabled: true,
	}}, plan.Addons)
	require.Equal(t, searchVoyage, plan.Search.Provider)
	require.False(t, plan.Search.Ready)
	require.Equal(t, "VOYAGE_API_KEY", plan.Search.Providers[0].Key)
	require.Equal(t, version.Version, plan.Pin)
	require.Equal(t, ".rhizome/config.yml", plan.Writes.Summary[0])
	require.Equal(t, []string{".rhizome/config.yml", ".rhizome/workflows.yml", ".rhizome/ignore"}, plan.Writes.Files[:3])
	require.Contains(t, plan.Writes.Files, "AGENTS.md")

	rule, ok := planRule(plan.Scope, "/testdata/")
	require.True(t, ok)
	require.Equal(t, reasonFixtures, rule.Reason)
	require.True(t, rule.Planned)
	require.NoDirExists(t, filepath.Join(root, ".rhizome"))
}

func TestPlanFollowsTheChoicesItIsGiven(t *testing.T) {
	root := machineRunRepo(t)

	plan, err := Plan(RunOptions{Dir: root, Workflow: "none", Addons: templateActionItems, Agents: "claude", Search: "off", Skip: []string{"cmd"}, KeepIndexed: []string{"testdata"}})
	require.NoError(t, err)

	require.Equal(t, "none", plan.Workflow)
	require.True(t, plan.Addons[0].Enabled, "an add-on can be chosen without a workflow")
	require.Equal(t, []AgentOption{{ID: "claude", Label: "Claude Code", Enabled: true}, {ID: "codex", Label: "Codex"}, {ID: "cursor", Label: "Cursor"}}, plan.Agents)
	require.Equal(t, searchOff, plan.Search.Provider)
	_, skipsCmd := planRule(plan.Scope, "/cmd/")
	_, skipsFixtures := planRule(plan.Scope, "/testdata/")
	require.True(t, skipsCmd)
	require.False(t, skipsFixtures)
	require.Equal(t, []string{"testdata"}, plan.Scope.KeepIndexed)
	require.NotContains(t, plan.Writes.Files, "docs/engineering/README.md")
}

func TestApplyWritesTheChoicesAndReportsThem(t *testing.T) {
	root := machineRunRepo(t)

	result, err := Apply(RunOptions{Dir: root, Workflow: "agentic-engineering", Addons: "none", Search: "off", Skip: []string{"cmd"}})
	require.NoError(t, err)

	canonical, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	require.Equal(t, canonical, result.Root)
	require.Equal(t, ".rhizome/config.yml", result.Summary[0])
	require.Contains(t, result.Summary, "Skipped cmd/ and testdata/ (1 file) in .rhizome/ignore")
	require.Equal(t, ".rhizome/", result.Commit[0])
	require.Equal(t, PinResult{Version: version.Version}, result.Pin)
	require.NotContains(t, result.Created, ".rhizome/ontology/action-items.graphql")
	require.Equal(t, []string{".rhizome/config.yml", ".rhizome/workflows.yml", ".rhizome/ignore"}, result.Created[:3])

	workflow, _, err := obsidian.LoadLocalWorkflowConfig(root)
	require.NoError(t, err)
	require.Equal(t, []string{templateActionItems}, workflow.Addons.Disabled)
	require.Contains(t, readIgnoreFile(t, root), "# "+reasonManual+"\n/cmd/\n")

	// A terminal run afterward has nothing left to change.
	require.NoError(t, Run(RunOptions{Dir: root, Check: true, Stdout: io.Discard, Stderr: io.Discard}))
}

func TestApplySavesAKeyFromOptionsWithoutReportingIt(t *testing.T) {
	root := machineRunRepo(t)

	result, err := Apply(RunOptions{Dir: root, SearchKey: "pa-from-a-pipe"})
	require.NoError(t, err)

	require.Equal(t, "Voyage API Key", result.SavedKey)
	require.Equal(t, "pa-from-a-pipe", readCliConfigForTest(t).Env["VOYAGE_API_KEY"])
	cfg, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	require.True(t, cfg.NoteEmbeddings.Enabled)
	doc, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(doc), "pa-from-a-pipe")
}

func TestASearchKeyFromOptionsRecognizesTheTeamKey(t *testing.T) {
	root := machineRunRepo(t)
	const unlock = "synthetic-team-unlock"
	r, s, chosen, err := startMachineRun(RunOptions{Dir: root, SearchKey: unlock})
	require.NoError(t, err)
	r.session = credentials.NewSession(credentials.WithTeamKeyBundle(func() bool { return true }, func(value string) bool { return value == unlock }))

	_, _, outcome, err := r.prepareFirstRun(s, chosen)
	require.NoError(t, err)
	require.Equal(t, "Atomic Object Rhizome key", outcome.savedKey)
	require.Equal(t, unlock, readCliConfigForTest(t).Env[credentials.AtomicRhizomeKey])
}

func TestApplyReportsAFailedPinnedInstallWithTheWrittenSetup(t *testing.T) {
	root := machineRunRepo(t)
	original := installPinnedBinary
	installPinnedBinary = func(context.Context, appupdate.EnsureOptions) error { return errors.New("release not found") }
	t.Cleanup(func() { installPinnedBinary = original })

	result, err := Apply(RunOptions{Dir: root})
	require.NoError(t, err)

	require.Contains(t, result.Pin.Error, "release not found")
	require.NotEmpty(t, result.Created)
	require.FileExists(t, filepath.Join(root, ".rhizome", "config.yml"))
}

func TestMachineRunsAndFirstRunOptionsNeedAFolderThatIsNotSetUp(t *testing.T) {
	root := machineRunRepo(t)
	_, err := Apply(RunOptions{Dir: root})
	require.NoError(t, err)

	_, err = Plan(RunOptions{Dir: root})
	require.ErrorIs(t, err, ErrNotFirstRun)
	_, err = Apply(RunOptions{Dir: root})
	require.ErrorIs(t, err, ErrNotFirstRun)
	err = Run(RunOptions{Dir: root, Skip: []string{"cmd"}, Stdout: io.Discard, Stderr: io.Discard})
	require.ErrorIs(t, err, ErrNotFirstRun)
}

func TestFirstRunOptionsFailBeforeWriting(t *testing.T) {
	for name, opts := range map[string]RunOptions{
		"unknown add-on":        {Addons: "core", SearchKey: "pa-never-saved"},
		"missing skip path":     {Skip: []string{"nope"}},
		"escaping keep path":    {KeepIndexed: []string{"../outside"}},
		"key while checking":    {SearchKey: "pa-key", Check: true},
		"key for a keyless one": {SearchKey: "pa-key", Search: "ollama"},
	} {
		t.Run(name, func(t *testing.T) {
			root := machineRunRepo(t)
			opts.Dir = root
			_, err := Apply(opts)
			require.Error(t, err)
			require.NoDirExists(t, filepath.Join(root, ".rhizome"))
			require.Empty(t, readCliConfigForTest(t).Env["VOYAGE_API_KEY"], "no key is saved before options are checked")
		})
	}
}
