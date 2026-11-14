package init

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func clearLegacyEvidence(t *testing.T, root string) {
	t.Helper()
	workflow, _, err := repoconfig.LoadLocalWorkflowConfig(root)
	require.NoError(t, err)
	workflow.Management.SourceFingerprints = nil
	require.NoError(t, repoconfig.SaveLocalWorkflowConfig(root, workflow))
}

func findSetupDecision(t *testing.T, decisions []SetupDecision, path string) SetupDecision {
	t.Helper()
	for _, decision := range decisions {
		if decision.Path == path {
			return decision
		}
	}
	t.Fatalf("missing decision %s in %+v", path, decisions)
	return SetupDecision{}
}

func TestLegacyDecisionsRemainVisibleUntilResolved(t *testing.T) {
	root := legacyOwnershipRepo(t)
	clearLegacyEvidence(t, root)
	original := readTestFile(t, root, legacyEffortPath)
	plan, err := Plan(RunOptions{Dir: root})
	require.NoError(t, err)
	decision := findSetupDecision(t, plan.Writes.Decisions, moduleEffortPath)
	require.Equal(t, "unknown", decision.Kind)
	require.Equal(t, legacyEffortPath, decision.From)
	require.Contains(t, decision.Diff, "+")
	require.Contains(t, decision.Diff, "Delivery")
	require.Contains(t, plan.Writes.Kept, moduleEffortPath)
	result, err := Apply(RunOptions{Dir: root})
	require.NoError(t, err)
	require.Equal(t, decision, findSetupDecision(t, result.Decisions, moduleEffortPath))
	require.NotContains(t, result.Summary, "Everything is up to date.")
	require.Equal(t, original, readTestFile(t, root, legacyEffortPath))
	out, err := runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err)
	require.NotContains(t, out, "Everything is up to date.")
}

func TestTakeLegacyDecisionPreservesIdentifiers(t *testing.T) {
	root := legacyOwnershipRepo(t)
	clearLegacyEvidence(t, root)
	writeTestFile(t, root, legacyEffortPath, legacyEffortSchema+"type Custom { name: String }\n")
	result, err := Apply(RunOptions{Dir: root, Take: []string{moduleEffortPath}})
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(root, legacyEffortPath))
	updated := readTestFile(t, root, moduleEffortPath)
	require.Contains(t, updated, `group: "Delivery"`)
	require.NotContains(t, updated, "type Custom")
	contracts, err := parseIdentifierAllocationContracts("taken schema", []byte(updated))
	require.NoError(t, err)
	for _, name := range []string{"EffortNote.id", "EffortWorkspace.id"} {
		require.Equal(t, "SEQUENTIAL", contracts[name].allocation.strategy)
		require.Equal(t, "EFF", contracts[name].allocation.prefix)
	}
	require.Contains(t, result.Created, moduleEffortPath)
	require.Contains(t, result.Commit, ".rhizome/")
	record, ok := loadGeneratedFiles(root)
	require.True(t, ok)
	require.NotContains(t, record.Written, legacyEffortPath)
	require.Contains(t, record.Written, moduleEffortPath)
	require.Empty(t, result.Decisions)
}

func TestKeepLegacyDecisionPersistsDecline(t *testing.T) {
	root := legacyOwnershipRepo(t)
	clearLegacyEvidence(t, root)
	original := readTestFile(t, root, legacyEffortPath)
	_, err := runInit(t, RunOptions{Dir: root, Keep: []string{moduleEffortPath}})
	require.NoError(t, err)
	require.Equal(t, original, readTestFile(t, root, legacyEffortPath))
	require.NoFileExists(t, filepath.Join(root, moduleEffortPath))
	record, ok := loadGeneratedFiles(root)
	require.True(t, ok)
	require.NotEmpty(t, record.Declined[moduleEffortPath])
	require.NotContains(t, record.Declined, legacyEffortPath)
	plan, err := Plan(RunOptions{Dir: root})
	require.NoError(t, err)
	require.Empty(t, plan.Writes.Decisions)
	require.Empty(t, plan.Writes.Files)
}

func fixtureFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[rel] = string(data)
		return nil
	}))
	return files
}

func TestInvalidChoicesFailBeforeAnyWrites(t *testing.T) {
	for _, scenario := range []string{"stale", "conflict", "one valid one invalid", "ambiguous"} {
		t.Run(scenario, func(t *testing.T) {
			root := legacyOwnershipRepo(t)
			clearLegacyEvidence(t, root)
			opts := RunOptions{Dir: root, Take: []string{"missing.graphql"}}
			switch scenario {
			case "conflict":
				opts.Take, opts.Keep = []string{moduleEffortPath}, []string{moduleEffortPath}
			case "one valid one invalid":
				opts.Take = []string{moduleEffortPath, "missing.graphql"}
			case "ambiguous":
				writeTestFile(t, root, "AGENTS.md", "<!-- BEGIN RZM INIT RHIZOME BLOCK -->\nlocal\n<!-- END RZM INIT RHIZOME BLOCK -->\n<!-- BEGIN RZM INIT TEMPLATE BLOCK: core -->\nlocal\n<!-- END RZM INIT TEMPLATE BLOCK: core -->\n")
				record, _ := loadGeneratedFiles(root)
				record.Written["AGENTS.md#rhizome"], record.Written["AGENTS.md#core"] = "old", "old"
				require.NoError(t, record.save(root))
				opts.Take = []string{"AGENTS.md"}
			}
			before := fixtureFiles(t, root)
			_, err := Apply(opts)
			require.Error(t, err)
			require.Equal(t, before, fixtureFiles(t, root))
		})
	}
}

func TestPreparedMigrationRefusesChangedSourceBeforeWriting(t *testing.T) {
	root := legacyOwnershipRepo(t)
	r, s, _, err := startMachineRun(RunOptions{Dir: root, Take: []string{moduleEffortPath}})
	require.NoError(t, err)
	// Make this an explicit edited decision rather than a proven automatic update.
	writeTestFile(t, root, legacyEffortPath, legacyEffortSchema+"type Custom { name: String }\n")
	plan, err := r.planSettings(s)
	require.NoError(t, err)
	writeTestFile(t, root, legacyEffortPath, legacyEffortSchema+"type Newer { name: String }\n")
	before := fixtureFiles(t, root)
	_, err = r.apply(plan.setup, plan.res)
	require.ErrorContains(t, err, "changed while setup was being prepared")
	require.Equal(t, before, fixtureFiles(t, root))
}

func TestDecisionCopiesResolveTogetherAndPathsMustBeUnique(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"a", "b"} {
		writeTestFile(t, root, rel, "local")
	}
	plan := &filePlan{files: []generatedFile{{rel: "a", group: "skill", content: []byte("new"), mode: 0o644}, {rel: "b", group: "skill", content: []byte("new"), mode: 0o644}}}
	prepared, err := classifyFilePlan(root, plan, &generatedFiles{Written: map[string]string{}, Declined: map[string]string{}})
	require.NoError(t, err)
	decisions := prepared.decisions()
	require.Len(t, decisions, 1)
	require.Equal(t, []string{"b"}, decisions[0].Copies)
	require.NoError(t, prepared.setChoices([]string{"b"}, nil))
	_, err = prepared.apply(nil)
	require.NoError(t, err)
	require.Equal(t, "new", readTestFile(t, root, "a"))
	require.Equal(t, "new", readTestFile(t, root, "b"))
}

func TestSetupJSONAlwaysIncludesDecisions(t *testing.T) {
	root := machineRunRepo(t)
	plan, err := Plan(RunOptions{Dir: root, Agents: "none", Workflow: "none", Search: "off"})
	require.NoError(t, err)
	data, err := json.Marshal(plan.Writes)
	require.NoError(t, err)
	require.Contains(t, string(data), `"decisions":[]`)
	result, err := Apply(RunOptions{Dir: root, Agents: "none", Workflow: "none", Search: "off"})
	require.NoError(t, err)
	data, err = json.Marshal(result)
	require.NoError(t, err)
	require.Contains(t, string(data), `"decisions":[]`)
}

func historyGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func TestLegacyOwnershipRecoveryUsesOnlyExactGitEvidence(t *testing.T) {
	for _, scenario := range []struct {
		name            string
		changed, nested bool
	}{{"exact", false, false}, {"changed", true, false}, {"nested exact", false, true}, {"nested changed", true, true}} {
		t.Run(scenario.name, func(t *testing.T) {
			root := legacyOwnershipRepo(t)
			gitRoot := root
			if scenario.nested {
				gitRoot = filepath.Dir(root)
			}
			historyGit(t, gitRoot, "init", "-q")
			historyGit(t, root, "add", "-f", ".rhizome/workflows.yml")
			historyGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "legacy metadata")
			clearLegacyEvidence(t, root)
			if scenario.changed {
				writeTestFile(t, root, legacyEffortPath, legacyEffortSchema+"type Custom { name: String }\n")
			}
			workflow := readTestFile(t, root, ".rhizome/workflows.yml")
			plan, err := Plan(RunOptions{Dir: root})
			require.NoError(t, err)
			require.Equal(t, workflow, readTestFile(t, root, ".rhizome/workflows.yml"))
			if scenario.changed {
				findSetupDecision(t, plan.Writes.Decisions, moduleEffortPath)
				require.NotContains(t, plan.Writes.Files, moduleEffortPath)
			} else {
				require.Contains(t, plan.Writes.Files, moduleEffortPath)
				require.Empty(t, plan.Writes.Decisions)
				_, err = runInit(t, RunOptions{Dir: root})
				require.NoError(t, err)
				require.Contains(t, readTestFile(t, root, moduleEffortPath), `group: "Delivery"`)
				require.NoFileExists(t, filepath.Join(root, legacyEffortPath))
			}
		})
	}
}

func TestOrdinaryOwnershipDoesNotInvokeGitHistory(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "called")
	t.Setenv("GIT_TRACE2_EVENT", marker)
	record := &generatedFiles{Written: map[string]string{}, Declined: map[string]string{}}
	require.NoError(t, record.recoverLegacyStarterFiles(root, []string{templateAgenticEngineering}))
	require.NoFileExists(t, marker)
	writeTestFile(t, root, legacyEffortPath, legacyEffortSchema)
	require.NoError(t, record.recoverLegacyStarterFiles(root, []string{templateAgenticEngineering}))
	require.FileExists(t, marker)
	require.Empty(t, record.Written)
}
