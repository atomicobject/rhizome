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

	"github.com/atomicobject/rhizome/pkg/sourcetxn"
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

func TestIntegrationNamespaceSettledFootprintAllowsLaterSourceEdit(t *testing.T) {
	for _, decision := range []string{repairJournalCommitted, repairJournalRestored} {
		t.Run(decision, func(t *testing.T) {
			f := newNamespaceWriterFixture(t, decision, true)
			f.installLaterGitState(t)
			path := "new.md"
			if decision == repairJournalRestored {
				path = "old.md"
			}
			before := f.refusalSnapshot(t)
			lease, acquired, err := indexlock.TryAcquireLease(f.root, indexlock.AcquireOptions{})
			require.NoError(t, err)
			require.True(t, acquired)
			t.Cleanup(func() { require.NoError(t, lease.Release()) })
			writer, _, err := sourcetxn.Open(f.root, lease, sourcetxn.WriterOptions{Role: sourcetxn.SourceEditor})
			require.NoError(t, err)
			t.Cleanup(writer.Close)
			err = writer.Apply(t.Context(), sourcetxn.Intent{
				Fingerprint: sourcetxn.Hash([]byte("later edit of settled namespace source")),
				Files: []sourcetxn.FilePatch{{
					Path: path, Original: []byte(before[path].content), OriginalMode: before[path].mode,
					Updated: []byte("later authored edit on settled namespace source\n"),
				}},
			})
			require.NoError(t, err)
			writer.Close()
			expected := before
			expected[path] = namespaceWriterFile{exists: true, content: "later authored edit on settled namespace source\n", mode: before[path].mode}
			require.Equal(t, expected, f.refusalSnapshot(t), "source editing must preserve settled native evidence and later Git ownership")

			reopened, _, err := sourcetxn.Open(f.root, lease, sourcetxn.WriterOptions{Role: sourcetxn.SourceEditor})
			require.NoError(t, err)
			reopened.Close()
			require.Equal(t, expected, f.refusalSnapshot(t), "reopening must not roll back the later edit or replay the old Git state")
			journals, err := sourcetxn.InspectPending(f.root)
			require.NoError(t, err)
			require.Len(t, journals, 1)
			require.Equal(t, f.info.Manifest.TransactionID, journals[0].Manifest.TransactionID)
			require.Equal(t, decision, journals[0].Manifest.Namespace.SettledDecision)
		})
	}
}

func TestIntegrationNamespaceStartupStabilizesBeforeOntologyRecovery(t *testing.T) {
	for _, decision := range []string{repairJournalPrepared, repairJournalCommitted, repairJournalRestored} {
		t.Run(decision, func(t *testing.T) {
			f := newNamespaceWriterFixture(t, decision, false, namespaceFixtureCrash{partialPublication: decision == repairJournalPrepared})
			if decision == repairJournalPrepared {
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
			journals, err := sourcetxn.InspectPending(f.root)
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
			require.Equal(t, want, journals[0].State)
			require.Equal(t, want, journals[0].Manifest.Namespace.SettledDecision)
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
			failure := errors.New("synthetic namespace decision directory-sync failure")
			f := newNamespaceWriterFixture(t, decision, false, namespaceFixtureCrash{decisionSyncFailure: failure})
			marker := map[string]string{repairJournalCommitted: "COMMITTED", repairJournalRestored: "RESTORED"}[decision]
			require.FileExists(t, filepath.Join(f.info.Directory, marker), "the real exclusive marker creator ran before the failed directory sync")
			require.FileExists(t, filepath.Join(f.root, ".git/index.lock"), "uncertain decision retains owned Git exclusion")
			before := namespaceWriterSnapshot(t, f.root)
			runNamespaceApplication(t, f, "startup", "stabilize")
			after := namespaceWriterSnapshot(t, f.root)
			delete(before, ".git/index.lock")
			delete(after, ".git/index.lock")
			require.Equal(t, before, after, "startup must synchronize the terminal decision without replaying authored or Git state")
			require.NoFileExists(t, filepath.Join(f.root, ".git/index.lock"))
			journals, err := sourcetxn.InspectPending(f.root)
			require.NoError(t, err)
			require.Len(t, journals, 1)
			require.Equal(t, decision, journals[0].State)
			require.Equal(t, decision, journals[0].Manifest.Namespace.SettledDecision)
			f.requireWriterLeaseReleased(t)
		})
	}
}

type namespaceWriterFixture struct {
	root            string
	runCtx          RunContext
	info            sourcetxn.JournalInfo
	original        map[string]namespaceWriterFile
	originalStaging string
}

type namespaceFixtureCrash struct {
	partialPublication  bool
	decisionSyncFailure error
}

type namespaceWriterFile struct {
	exists  bool
	content string
	mode    os.FileMode
}

func newNamespaceWriterFixture(t *testing.T, decision string, settled bool, crashes ...namespaceFixtureCrash) *namespaceWriterFixture {
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
	var crash namespaceFixtureCrash
	if len(crashes) > 0 {
		crash = crashes[0]
	}
	hooks := &repairExecutionHooks{}
	if decision == repairJournalRestored || crash.partialPublication {
		hooks.AfterMutation = func(_ int, path string) error {
			if path != "old.md" {
				return nil
			}
			if crash.partialPublication {
				return errSimulatedRepairInterruption
			}
			return errors.New("publication rejected")
		}
	}
	if crash.decisionSyncFailure != nil {
		hooks.BeforeNamespaceDecisionDirectorySync = func(marker string) error {
			require.Equal(t, map[string]string{repairJournalCommitted: "COMMITTED", repairJournalRestored: "RESTORED"}[decision], marker)
			return crash.decisionSyncFailure
		}
	} else if !settled {
		hooks.BeforeNamespaceGitRelease = func(got string) error {
			require.Equal(t, decision, got)
			return errSimulatedRepairInterruption
		}
	}
	writer, err := openRepairWriterFixture(t, f.runCtx, hooks)
	require.NoError(t, err)
	defer writer.close()
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
	prepared, err := writer.prepareNamespace(context.Background(), compiled)
	require.NoError(t, err)
	initial := prepared.Info()
	require.NotNil(t, initial.Manifest.Namespace.Git, "fixture must exercise actual owned Git artifacts")
	require.Equal(t, initial.Manifest.Namespace.Git.LockHash, SourceHash(mustReadFile(t, filepath.Join(root, ".git/index.lock"))))
	if decision != repairJournalPrepared || crash.partialPublication {
		_, err = writer.writer.Publish(prepared)
		switch {
		case crash.decisionSyncFailure != nil:
			require.ErrorIs(t, err, crash.decisionSyncFailure)
		case !settled || crash.partialPublication:
			require.ErrorIs(t, err, errSimulatedRepairInterruption)
		case decision == repairJournalRestored:
			require.ErrorContains(t, err, "publication rejected")
		default:
			require.NoError(t, err)
		}
	}
	f.info = prepared.Info()
	writer.close()
	if decision != repairJournalPrepared && crash.decisionSyncFailure == nil {
		require.True(t, f.info.DecisionSynced, "the fixture stops only after a synchronized terminal decision")
	}
	journals, err := sourcetxn.InspectPending(f.root)
	require.NoError(t, err, "fixture must contain the owner's complete durable journal and owned artifacts")
	require.Len(t, journals, 1)
	require.Equal(t, decision, journals[0].State)
	wantSettled := ""
	if settled {
		wantSettled = decision
	}
	require.Equal(t, wantSettled, journals[0].Manifest.Namespace.SettledDecision)
	return f
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
	paths := append([]string{filepath.Join(f.info.Directory, "manifest.json"), filepath.Join(f.info.Directory, "COMMITTED"), filepath.Join(f.info.Directory, "RESTORED")}, f.info.Manifest.OwnedArtifacts...)
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
	cmd.Env = append(os.Environ(), "RZM_NAMESPACE_TEST_ROOT="+f.root, "RZM_NAMESPACE_TEST_MODE="+mode, "RZM_NAMESPACE_TEST_EXPECT="+expectation, "RZM_NAMESPACE_TEST_TRANSACTION="+f.info.Manifest.TransactionID)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
}
