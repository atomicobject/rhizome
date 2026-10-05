//go:build integration

package validate

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// These fixtures use the same durable creator and interruption boundary as a
// native session. Public adapters run in the external test package through the
// test binary, avoiding a validate -> application import cycle or test exports.
func TestIntegrationNamespacePublicWriterAdmission(t *testing.T) {
	for _, decision := range []string{repairJournalPrepared, repairJournalCommitted, repairJournalRestored} {
		writers := []string{"edit-session", "heading", "held-link"}
		if decision == repairJournalPrepared {
			writers = append(writers, "delete", "tags", "property")
		}
		for _, writer := range writers {
			t.Run(decision+"/"+writer, func(t *testing.T) {
				f := newNamespaceWriterFixture(t, decision, false)
				before := f.refusalSnapshot(t)
				runNamespaceApplication(t, f, writer, "refuse")
				require.Equal(t, before, f.refusalSnapshot(t), "refused public writer changed authored bytes, durable journal, or Git ownership")
				require.NoDirExists(t, filepath.Join(f.root, ".rhizome/write-locks"), "admission must precede per-note lock work")
				require.NoDirExists(t, filepath.Join(f.root, ".rhizome/edit-journal"), "admission must precede legacy journal publication")
				f.requireWriterLeaseReleased(t)
			})
		}
	}
}

func TestIntegrationNamespaceSettledPublicWritersPreserveLaterGitState(t *testing.T) {
	for _, decision := range []string{repairJournalCommitted, repairJournalRestored} {
		for _, writer := range []string{"edit-session", "heading", "held-link"} {
			t.Run(decision+"/"+writer, func(t *testing.T) {
				f := newNamespaceWriterFixture(t, decision, true)
				f.installLaterGitState(t)
				before := namespaceWriterSnapshot(t, f.root)
				runNamespaceApplication(t, f, writer, "apply")
				after := namespaceWriterSnapshot(t, f.root)
				require.NotEqual(t, before["edit.md"].content, after["edit.md"].content)
				delete(before, "edit.md")
				delete(after, "edit.md")
				require.Equal(t, before, after, "settled journal claimed newer authored or Git state")
				f.requireWriterLeaseReleased(t)
			})
		}
	}
}

func TestIntegrationNamespaceStartupStabilizesBeforeOntologyRecovery(t *testing.T) {
	for _, decision := range []string{repairJournalPrepared, repairJournalCommitted, repairJournalRestored} {
		t.Run(decision, func(t *testing.T) {
			f := newNamespaceWriterFixture(t, decision, false)
			if decision == repairJournalPrepared {
				_, release, err := acquireRepairIndexLockLease(filepath.Join(f.root, ".rhizome/index.lock"))
				require.NoError(t, err)
				f.interruptPublication(t)
				require.NoError(t, release())
				require.NoFileExists(t, filepath.Join(f.root, "old.md"))
			} else {
				// A durable decision ends rollback authority even while projection
				// convergence is pending. Startup must preserve later authored edits.
				path := "new.md"
				if decision == repairJournalRestored {
					path = "old.md"
				}
				f.write(t, path, "later authored edit\n", 0o640)
			}
			before := namespaceWriterSnapshot(t, f.root)
			runNamespaceApplication(t, f, "startup", "stabilize")
			journals, err := discoverRepairJournals(f.runCtx)
			require.NoError(t, err)
			require.Len(t, journals, 1, "startup leaves the journal for projection convergence")
			want := decision
			if decision == repairJournalPrepared {
				want = repairJournalRestored
				require.Equal(t, f.original["old.md"], namespaceWriterSnapshot(t, f.root)["old.md"])
				require.Equal(t, f.original["backlinks.md"], namespaceWriterSnapshot(t, f.root)["backlinks.md"])
				require.NoFileExists(t, filepath.Join(f.root, "new.md"))
				require.Equal(t, f.originalStaging, f.git(t, "ls-files", "--stage", "-z"), "rollback must preserve staged content distinct from dirty authored content")
			} else {
				after := namespaceWriterSnapshot(t, f.root)
				delete(before, ".git/index.lock")
				delete(after, ".git/index.lock")
				require.Equal(t, before, after)
			}
			require.Equal(t, want, journals[0].state)
			require.Equal(t, want, journals[0].manifest.Namespace.SettledDecision)
			require.NoFileExists(t, filepath.Join(f.root, ".git/index.lock"))
			f.requireWriterLeaseReleased(t)
			// Repeated startup may inspect terminal evidence, but must not replay
			// its old Git index or remove another command's newly acquired lock.
			f.installLaterGitState(t)
			stable := namespaceWriterSnapshot(t, f.root)
			runNamespaceApplication(t, f, "startup", "apply")
			require.Equal(t, stable, namespaceWriterSnapshot(t, f.root))
		})
	}
}

func TestIntegrationNamespaceStartupResynchronizesUncertainDecision(t *testing.T) {
	for _, decision := range []string{repairJournalCommitted, repairJournalRestored} {
		t.Run(decision, func(t *testing.T) {
			f := newNamespaceWriterFixture(t, repairJournalPrepared, false)
			_, release, err := acquireRepairIndexLockLease(filepath.Join(f.root, ".rhizome/index.lock"))
			require.NoError(t, err)
			failure := errors.New("synthetic namespace decision directory-sync failure")
			marker := map[string]string{repairJournalCommitted: "COMMITTED", repairJournalRestored: "RESTORED"}[decision]
			hooks := &repairExecutionHooks{BeforeNamespaceDecisionDirectorySync: func(got string) error {
				require.Equal(t, marker, got)
				return failure
			}}
			if decision == repairJournalCommitted {
				err = commitPreparedRepairTransaction(f.runCtx, f.prepared, hooks)
			} else {
				f.interruptPublication(t)
				err = restoreNamespacePrepared(f.runCtx, f.prepared, hooks)
			}
			require.ErrorIs(t, err, failure)
			require.FileExists(t, filepath.Join(f.prepared.dir, marker), "the real exclusive marker creator ran before the failed directory sync")
			require.FileExists(t, filepath.Join(f.root, ".git/index.lock"), "uncertain decision retains owned Git exclusion")
			require.NoError(t, release())
			before := namespaceWriterSnapshot(t, f.root)
			runNamespaceApplication(t, f, "startup", "stabilize")
			after := namespaceWriterSnapshot(t, f.root)
			delete(before, ".git/index.lock")
			delete(after, ".git/index.lock")
			require.Equal(t, before, after, "startup must synchronize the terminal decision without replaying authored or Git state")
			require.NoFileExists(t, filepath.Join(f.root, ".git/index.lock"))
			journals, err := discoverRepairJournals(f.runCtx)
			require.NoError(t, err)
			require.Len(t, journals, 1)
			require.Equal(t, decision, journals[0].state)
			require.Equal(t, decision, journals[0].manifest.Namespace.SettledDecision)
			f.requireWriterLeaseReleased(t)
		})
	}
}

type namespaceWriterFixture struct {
	root            string
	runCtx          RunContext
	prepared        *preparedRepairTransaction
	original        map[string]namespaceWriterFile
	originalStaging string
}

type namespaceWriterFile struct {
	exists  bool
	content string
	mode    os.FileMode
}

func newNamespaceWriterFixture(t *testing.T, decision string, settled bool) *namespaceWriterFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	f := &namespaceWriterFixture{root: root, runCtx: RunContext{VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}}}
	f.write(t, ".rhizome/config.yml", "notes:\n  includes: [\"**/*.md\"]\n", 0o644)
	f.write(t, ".rhizome/ontology/schema.graphql", `
type Topic implements Section @node(locator: EMBEDDED) {
  status: String @field
}
type Spec @node(paths: ["edit.md"]) {
  summary: String!
  topics: [Topic!] @contains(level: H2)
}
`, 0o644)
	f.write(t, "old.md", "staged source\n", 0o640)
	f.write(t, "backlinks.md", "[[old]]\n", 0o644)
	f.write(t, "edit.md", "---\ntype: Spec\nsummary: Original\n---\n# Edit\n\n## Old heading\nstatus:: TODO\n", 0o644)
	f.write(t, "unrelated.txt", "original\n", 0o644)
	// Local fixture configuration makes Git independent of machine settings.
	global := filepath.Join(t.TempDir(), "git-config")
	require.NoError(t, os.WriteFile(global, nil, 0o600))
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	f.git(t, "init", "--quiet")
	f.git(t, "config", "user.name", "Namespace Fixture")
	f.git(t, "config", "user.email", "fixture@example.invalid")
	f.git(t, "add", "old.md", "backlinks.md", "edit.md", "unrelated.txt")
	f.git(t, "-c", "core.hooksPath=", "commit", "--quiet", "-m", "fixture")
	f.originalStaging = f.git(t, "ls-files", "--stage", "-z")
	f.write(t, "old.md", "dirty authored source\n", 0o640)
	f.original = namespaceWriterSnapshot(t, root)
	_, release, err := acquireRepairIndexLockLease(filepath.Join(root, ".rhizome/index.lock"))
	require.NoError(t, err)
	defer func() { require.NoError(t, release()) }()
	var operations []RepairOperation
	for _, path := range []string{"old.md", "backlinks.md"} {
		file := f.original[path]
		mode := repairModeBits(file.mode)
		op := RepairOperation{Path: path, SourceHash: SourceHash([]byte(file.content)), SourceMode: &mode}
		if path == "old.md" {
			op.Kind, op.DestinationPath = RepairOperationRename, "new.md"
			op.DestinationState = &RepairDestinationState{Kind: RepairDestinationAbsent}
		} else {
			op.Kind, op.Content = RepairOperationWrite, []byte("[[new]]\n")
		}
		operations = append(operations, op)
	}
	compiled, err := compileNamespacePlan(f.runCtx, NamespaceMutationPlan{Operations: operations, Summary: []byte(`{"count":1}`)})
	require.NoError(t, err)
	f.prepared, err = prepareNativeNamespace(context.Background(), f.runCtx, compiled, nil)
	require.NoError(t, err)
	require.NotNil(t, f.prepared.manifest.Namespace.Git, "fixture must exercise actual owned Git artifacts")
	require.Equal(t, namespaceGitLockContent(f.prepared.manifest.Namespace.Git), mustReadFile(t, filepath.Join(root, ".git/index.lock")))
	switch decision {
	case repairJournalPrepared:
	case repairJournalCommitted:
		require.NoError(t, commitPreparedRepairTransaction(f.runCtx, f.prepared, nil))
	case repairJournalRestored:
		f.interruptPublication(t)
		f.restoreBeforeRelease(t)
	default:
		t.Fatalf("unknown fixture decision %s", decision)
	}
	if settled {
		journal := recoveredRepairJournal{dir: f.prepared.dir, manifest: f.prepared.manifest, state: decision, decisionSynced: f.prepared.decisionSynced}
		require.NoError(t, settleNamespaceGitRelease(f.runCtx, &journal))
		require.Equal(t, decision, journal.manifest.Namespace.SettledDecision)
	}
	journals, err := discoverRepairJournals(f.runCtx)
	require.NoError(t, err, "fixture must contain the owner's complete durable journal and owned artifacts")
	require.Len(t, journals, 1)
	require.Equal(t, decision, journals[0].state)
	wantSettled := ""
	if settled {
		wantSettled = decision
	}
	require.Equal(t, wantSettled, journals[0].manifest.Namespace.SettledDecision)
	return f
}

func (f *namespaceWriterFixture) restoreBeforeRelease(t *testing.T) {
	t.Helper()
	// Reproduce the real restore producer's verified prefix, stopping at its
	// durable decision before Git release. The full restore helper settles in
	// the same call, so it cannot model interruption between these boundaries.
	require.NoError(t, acquireNamespaceGitExclusion(f.runCtx, f.prepared))
	require.NoError(t, preflightNamespaceGitRollback(f.runCtx, f.prepared.manifest))
	plan, err := buildRepairRollbackPlan(f.runCtx, f.prepared.manifest)
	require.NoError(t, err)
	_, err = executeRepairRollbackPlan(f.runCtx, f.prepared.manifest, plan, nil)
	require.NoError(t, err)
	require.NoError(t, restoreNamespaceGitIndex(f.runCtx, f.prepared))
	require.NoError(t, verifyNamespaceOriginalFiles(f.runCtx, f.prepared.manifest))
	require.NoError(t, verifyNamespaceGitRestored(f.runCtx, f.prepared.manifest))
	require.NoError(t, markNamespaceRestored(f.prepared.dir))
	f.prepared.decisionSynced = true
}

func (f *namespaceWriterFixture) interruptPublication(t *testing.T) {
	t.Helper()
	err := commitPreparedRepairTransaction(f.runCtx, f.prepared, &repairExecutionHooks{AfterMutation: func(_ int, path string) error {
		if path == "old.md" {
			return errSimulatedRepairInterruption
		}
		return nil
	}})
	require.ErrorIs(t, err, errSimulatedRepairInterruption)
}

func (f *namespaceWriterFixture) installLaterGitState(t *testing.T) {
	t.Helper()
	f.write(t, "unrelated.txt", "later staging\n", 0o644)
	f.git(t, "add", "unrelated.txt")
	f.write(t, ".git/index.lock", "another Git command owns this lock\n", 0o640)
}

func (f *namespaceWriterFixture) write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	abs := filepath.Join(f.root, path)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte(content), mode))
}

func (f *namespaceWriterFixture) git(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", f.root, "-c", "core.fsmonitor=false"}, args...)...)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	return string(output)
}

func (f *namespaceWriterFixture) requireWriterLeaseReleased(t *testing.T) {
	t.Helper()
	release, acquired, err := indexlock.TryAcquire(filepath.Join(f.root, ".rhizome/index.lock"))
	require.NoError(t, err)
	require.True(t, acquired, "public adapter leaked the vault writer lease")
	require.NoError(t, release())
}

func (f *namespaceWriterFixture) refusalSnapshot(t *testing.T) map[string]namespaceWriterFile {
	t.Helper()
	paths := append([]string{filepath.Join(f.prepared.dir, "manifest.json"), filepath.Join(f.prepared.dir, "COMMITTED"), filepath.Join(f.prepared.dir, "RESTORED")}, f.prepared.manifest.OwnedArtifacts...)
	return namespaceWriterSnapshot(t, f.root, paths...)
}

func namespaceWriterSnapshot(t *testing.T, root string, additionalPaths ...string) map[string]namespaceWriterFile {
	t.Helper()
	files := map[string]namespaceWriterFile{}
	paths := []string{"old.md", "new.md", "backlinks.md", "edit.md", "unrelated.txt", ".git/index", ".git/index.lock"}
	for _, path := range additionalPaths {
		relative, err := filepath.Rel(root, path)
		require.NoError(t, err)
		paths = append(paths, relative)
	}
	for _, path := range paths {
		info, err := os.Stat(filepath.Join(root, path))
		if os.IsNotExist(err) {
			files[path] = namespaceWriterFile{}
			continue
		}
		require.NoError(t, err)
		files[path] = namespaceWriterFile{exists: true, content: string(mustReadFile(t, filepath.Join(root, path))), mode: info.Mode()}
	}
	return files
}

func runNamespaceApplication(t *testing.T, f *namespaceWriterFixture, mode, expectation string) {
	t.Helper()
	executable, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestIntegrationNamespaceApplicationHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(), "RZM_NAMESPACE_TEST_ROOT="+f.root, "RZM_NAMESPACE_TEST_MODE="+mode, "RZM_NAMESPACE_TEST_EXPECT="+expectation, "RZM_NAMESPACE_TEST_TRANSACTION="+f.prepared.manifest.TransactionID)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
}
