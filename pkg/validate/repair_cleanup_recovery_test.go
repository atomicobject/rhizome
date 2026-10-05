package validate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepairJournalCleanupInterruptionNeverRollsBackCommittedBytes(t *testing.T) {
	tests := []struct {
		name          string
		boundary      string
		evidenceState string
		needsRefresh  bool
	}{
		{name: "after artifact removal", boundary: "after_artifact_removed", evidenceState: repairJournalCommitted, needsRefresh: true},
		{name: "before detach", boundary: "before_detach", evidenceState: repairJournalCommitted, needsRefresh: true},
		{name: "after detach", boundary: "after_detach", evidenceState: "cleanup_pending"},
		{name: "after committed marker removal", boundary: "after_committed_removed", evidenceState: "cleanup_pending"},
		{name: "after manifest removal", boundary: "after_manifest_removed", evidenceState: "cleanup_pending"},
		{name: "after directory removal", boundary: "after_directory_removed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			runCtx := repairRunContext(t, root)
			plan, notePath, after := leaveCommittedRepairJournal(t, runCtx)
			injected := errors.New("injected cleanup interruption")

			_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
				Fix:                true,
				NonInteractive:     true,
				PostApplyRefresher: &repairPostApplyProbe{t: t},
				repairHooks: &repairExecutionHooks{AfterCleanupBoundary: func(boundary, _ string) error {
					if boundary == tt.boundary {
						return injected
					}
					return nil
				}},
			})
			require.ErrorIs(t, err, injected)
			assert.Equal(t, after, mustReadFile(t, notePath), "cleanup interruption must not reinterpret a committed transaction as prepared")

			evidence, detectErr := DetectPendingRepairJournals(runCtx)
			require.NoError(t, detectErr)
			if tt.evidenceState == "" {
				assert.Empty(t, evidence, "completed directory removal leaves no recovery work")
				return
			}
			require.Len(t, evidence, 1)
			assert.Equal(t, tt.evidenceState, evidence[0].State)

			opts := Options{
				Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
				postApplyCheck: func(context.Context, *IndexLockLease, *ontology.Runtime, repairPostApplyScope) error { return nil },
			}
			_, err = ApplyFixPlan(context.Background(), runCtx, &FixPlan{}, opts)
			require.NoError(t, err, "cleanup retry must converge from every durable boundary")
			assert.Equal(t, after, mustReadFile(t, notePath))
			evidence, detectErr = DetectPendingRepairJournals(runCtx)
			require.NoError(t, detectErr)
			assert.Empty(t, evidence)
		})
	}
}

func TestRepairJournalCleanupUnknownEntryRetainsDecodableCommittedEvidence(t *testing.T) {
	root := t.TempDir()
	runCtx := repairRunContext(t, root)
	plan, notePath, after := leaveCommittedRepairJournal(t, runCtx)
	dir, err := repairJournalDir(runCtx, plan.Transactions[0].ID)
	require.NoError(t, err)
	manifest, err := readRepairJournalManifest(dir)
	require.NoError(t, err)
	require.NotEmpty(t, manifest.OwnedArtifacts)
	var retainedArtifact string
	for _, artifact := range manifest.OwnedArtifacts {
		if _, statErr := os.Lstat(artifact); statErr == nil {
			retainedArtifact = artifact
			break
		}
	}
	require.NotEmpty(t, retainedArtifact, "committed journal retains at least its backup artifact")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "unexpected-entry"), []byte("not engine owned\n"), 0o600))

	_, err = ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix:                true,
		NonInteractive:     true,
		PostApplyRefresher: &repairPostApplyProbe{t: t},
	})
	require.Error(t, err)
	assert.Equal(t, after, mustReadFile(t, notePath))
	require.FileExists(t, filepath.Join(dir, "manifest.json"), "unsafe cleanup must preserve decodable recovery evidence")
	require.FileExists(t, filepath.Join(dir, "COMMITTED"), "unsafe cleanup must preserve terminal commit authority")
	require.FileExists(t, retainedArtifact, "metadata preflight must run before artifact deletion")

	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	require.Len(t, evidence, 1)
	assert.Equal(t, repairJournalCommitted, evidence[0].State)
	assert.Equal(t, plan.Transactions[0].ID, evidence[0].TransactionID)
}

func TestRepairJournalDiscoveryFailsClosedOnUnexpectedRootFile(t *testing.T) {
	root := t.TempDir()
	runCtx := repairRunContext(t, root)
	journalRoot, err := repairJournalRoot(runCtx)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(journalRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(journalRoot, "unexpected-root-file"), []byte("corrupt\n"), 0o600))

	_, err = DetectPendingRepairJournals(runCtx)
	require.ErrorContains(t, err, "unexpected repair journal root entry")
}

func TestRepairJournalCleanupRetryRecognizesAlreadyRestoredRename(t *testing.T) {
	root := t.TempDir()
	runCtx := repairRunContext(t, root)
	before := []byte("rename me\n")
	source := filepath.Join(root, "old.md")
	destination := filepath.Join(root, "new.md")
	require.NoError(t, os.WriteFile(source, before, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:cleanup-rename", issueKey: "issue:cleanup-rename",
		operations: []RepairOperation{{
			ID: "operation:cleanup-rename", ActionID: "action:cleanup-rename",
			Kind: RepairOperationRename, Path: "old.md", DestinationPath: "new.md",
			SourceHash: SourceHash(before), Lifecycle: LifecyclePolicyResult{Decision: LifecycleNotHistorical},
		}},
	}})

	_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{AfterInstall: func(_ int, path string) error {
			if path == "new.md" {
				return errSimulatedRepairInterruption
			}
			return nil
		}},
	})
	require.ErrorIs(t, err, errSimulatedRepairInterruption)
	require.FileExists(t, destination)

	dir, err := repairJournalDir(runCtx, plan.Transactions[0].ID)
	require.NoError(t, err)
	unexpected := filepath.Join(dir, "unexpected-entry")
	require.NoError(t, os.WriteFile(unexpected, []byte("operator evidence\n"), 0o600))
	_, err = ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
	})
	require.Error(t, err)
	assert.Equal(t, before, mustReadFile(t, source))
	assert.NoFileExists(t, destination)

	require.NoError(t, os.Remove(unexpected))
	_, err = ApplyFixPlan(context.Background(), runCtx, &FixPlan{}, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
	})
	require.NoError(t, err, "cleanup retry must not require the deleted backup after rollback restored the source")
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	assert.Empty(t, evidence)
	assert.Equal(t, before, mustReadFile(t, source))
}

func TestApplyRepairSessionConvergesAfterCleanupPendingRecovery(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"target.md": "# Target\n",
		"source.md": "clean link: [[target]]\n",
	}))
	runCtx := linkHygieneRunContext(t, root)
	plan, _, _ := leaveCommittedRepairJournal(t, runCtx)
	injected := errors.New("stop after terminal detach")
	_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
		repairHooks: &repairExecutionHooks{AfterCleanupBoundary: func(boundary, _ string) error {
			if boundary == cleanupAfterDetach {
				return injected
			}
			return nil
		}},
	})
	require.ErrorIs(t, err, injected)

	blocked, scanCtx, err := RunSuiteOnce(context.Background(), Options{
		Checks: []string{CheckLinkHygiene}, RunContext: &runCtx,
	})
	require.NoError(t, err)
	require.Len(t, blocked.RepairJournals, 1, "the cleanup-pending journal must block the read-only scan")
	result, _, err := ApplyRepairSession(context.Background(), blocked, scanCtx, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
	})
	require.NoError(t, err)
	assert.True(t, result.OK, "cleanup-only recovery must return the fresh validation result")
	assert.Empty(t, result.RepairJournals)
	require.NotNil(t, result.FixExecution)
	require.Len(t, result.FixExecution.Transactions, 1)
	assert.Equal(t, "recovered_cleanup", result.FixExecution.Transactions[0].Status)
}

func TestRecoveredCommittedJournalBlocksDifferentPlanBeforeNewPrepare(t *testing.T) {
	root := t.TempDir()
	runCtx := repairRunContext(t, root)
	path := filepath.Join(root, "note.md")
	before := []byte("before\n")
	firstAfter := []byte("first after\n")
	secondAfter := []byte("second after\n")
	require.NoError(t, os.WriteFile(path, before, 0o640))
	firstPlan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:first-overlap", issueKey: "issue:first-overlap",
		operations: []RepairOperation{repairWriteOperation(
			"operation:first-overlap", "action:first-overlap", "note.md", before, firstAfter,
		)},
	}})
	refreshErr := errors.New("retain first committed journal")
	_, err := ApplyFixPlan(context.Background(), runCtx, &firstPlan, Options{
		Fix: true, NonInteractive: true,
		PostApplyRefresher: &repairPostApplyProbe{t: t, refreshErr: refreshErr},
	})
	require.ErrorIs(t, err, refreshErr)

	secondPlan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:second-overlap", issueKey: "issue:second-overlap",
		operations: []RepairOperation{repairWriteOperation(
			"operation:second-overlap", "action:second-overlap", "note.md", firstAfter, secondAfter,
		)},
	}})
	_, err = ApplyFixPlan(context.Background(), runCtx, &secondPlan, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
		repairHooks: &repairExecutionHooks{AfterJournalPublished: func(string) error {
			return errSimulatedRepairInterruption
		}},
	})
	require.ErrorContains(t, err, "replan")
	assert.NotErrorIs(t, err, errSimulatedRepairInterruption, "new transaction must never reach prepare")
	assert.Equal(t, firstAfter, mustReadFile(t, path))
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	assert.Empty(t, evidence, "recovered committed evidence is refreshed and cleaned before replanning")
}

func TestPreparedRecoveryReplanReportsNewUnselectedActionOnlyAsSkipped(t *testing.T) {
	root := t.TempDir()
	runCtx := repairRunContext(t, root)
	recoveryBefore := []byte("recovery before\n")
	newBefore := []byte("new before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "recovery.md"), recoveryBefore, 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(root, "new.md"), newBefore, 0o640))

	recoveryPlan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:recovery", issueKey: "issue:recovery",
		operations: []RepairOperation{repairWriteOperation(
			"operation:recovery", "action:recovery", "recovery.md", recoveryBefore, []byte("recovery after\n"),
		)},
	}})
	_, err := ApplyFixPlan(context.Background(), runCtx, &recoveryPlan, Options{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{AfterInstall: func(_ int, _ string) error {
			return errSimulatedRepairInterruption
		}},
	})
	require.ErrorIs(t, err, errSimulatedRepairInterruption)

	newPlan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:confirm-new", issueKey: "issue:confirm-new",
		operations: []RepairOperation{repairWriteOperation(
			"operation:confirm-new", "action:confirm-new", "new.md", newBefore, []byte("new after\n"),
		)},
	}})
	newPlan.Actions[0].Safety = FixSafetyConfirm
	newPlan, err = FinalizeRepairPlan(newPlan)
	require.NoError(t, err)

	execution, err := ApplyFixPlan(context.Background(), runCtx, &newPlan, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
	})
	require.ErrorContains(t, err, "replan")
	assert.Contains(t, execution.Skipped, "action:confirm-new")
	assert.NotContains(t, execution.Failed, "action:confirm-new",
		"an action excluded by safety selection must not also be classified as failed")
	assert.Equal(t, recoveryBefore, mustReadFile(t, filepath.Join(root, "recovery.md")))
	assert.Equal(t, newBefore, mustReadFile(t, filepath.Join(root, "new.md")))
}

func TestRenameRollbackRejectsRecreatedDistinctSourceWithoutMutatingEitherFile(t *testing.T) {
	root := t.TempDir()
	runCtx := repairRunContext(t, root)
	original := []byte("original\n")
	userSource := []byte("user recreated source\n")
	source := filepath.Join(root, "old.md")
	destination := filepath.Join(root, "new.md")
	require.NoError(t, os.WriteFile(source, original, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:rename-collision", issueKey: "issue:rename-collision",
		operations: []RepairOperation{{
			ID: "operation:rename-collision", ActionID: "action:rename-collision",
			Kind: RepairOperationRename, Path: "old.md", DestinationPath: "new.md",
			SourceHash: SourceHash(original), Lifecycle: LifecyclePolicyResult{Decision: LifecycleNotHistorical},
		}},
	}})
	_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{AfterInstall: func(_ int, path string) error {
			if path == "new.md" {
				return errSimulatedRepairInterruption
			}
			return nil
		}},
	})
	require.ErrorIs(t, err, errSimulatedRepairInterruption)
	journals, err := discoverRepairJournals(runCtx)
	require.NoError(t, err)
	require.Len(t, journals, 1)
	rollbackPlan, err := buildRepairRollbackPlan(runCtx, journals[0].manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(source, userSource, 0o600))

	mutated, err := executeRepairRollbackPlan(runCtx, journals[0].manifest, rollbackPlan)
	require.Error(t, err)
	assert.False(t, mutated)
	assert.Equal(t, userSource, mustReadFile(t, source))
	assert.Equal(t, original, mustReadFile(t, destination))
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	require.Len(t, evidence, 1)
	assert.Equal(t, repairJournalPrepared, evidence[0].State)
}

func TestPreparedRecoveryRetryReplaysRollbackDeltaAfterRefreshFailure(t *testing.T) {
	root := t.TempDir()
	runCtx := repairRunContext(t, root)
	before := []byte("before\n")
	path := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(path, before, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:rollback-refresh", issueKey: "issue:rollback-refresh",
		operations: []RepairOperation{repairWriteOperation(
			"operation:rollback-refresh", "action:rollback-refresh", "note.md", before, []byte("after\n"),
		)},
	}})
	_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{AfterInstall: func(_ int, _ string) error {
			return errSimulatedRepairInterruption
		}},
	})
	require.ErrorIs(t, err, errSimulatedRepairInterruption)

	refreshErr := errors.New("rollback refresh failed")
	firstRefresh := &repairPostApplyProbe{t: t, refreshErr: refreshErr}
	_, err = ApplyFixPlan(context.Background(), runCtx, &FixPlan{}, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: firstRefresh,
	})
	require.ErrorIs(t, err, refreshErr)
	assert.Equal(t, []string{"note.md"}, firstRefresh.changed)
	assert.Equal(t, before, mustReadFile(t, path))

	retryRefresh := &repairPostApplyProbe{t: t}
	_, err = ApplyFixPlan(context.Background(), runCtx, &FixPlan{}, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: retryRefresh,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"note.md"}, retryRefresh.changed,
		"already-restored PREPARED evidence must replay the rollback delta until cleanup")
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	assert.Empty(t, evidence)
}

func TestPartialRollbackFailureRefreshesCompletedDeltaAndRetainsJournal(t *testing.T) {
	root := t.TempDir()
	runCtx := repairRunContext(t, root)
	aBefore := []byte("a before\n")
	bBefore := []byte("b before\n")
	aPath := filepath.Join(root, "a.md")
	bPath := filepath.Join(root, "b.md")
	require.NoError(t, os.WriteFile(aPath, aBefore, 0o640))
	require.NoError(t, os.WriteFile(bPath, bBefore, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:partial-rollback", issueKey: "issue:partial-rollback",
		operations: []RepairOperation{
			repairWriteOperation("operation:a-partial", "action:partial-rollback", "a.md", aBefore, []byte("a after\n")),
			repairWriteOperation("operation:b-partial", "action:partial-rollback", "b.md", bBefore, []byte("b after\n")),
		},
	}})
	_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{AfterInstall: func(_ int, path string) error {
			if path == "b.md" {
				return errSimulatedRepairInterruption
			}
			return nil
		}},
	})
	require.ErrorIs(t, err, errSimulatedRepairInterruption)

	userEdit := []byte("a user edit during recovery\n")
	probe := &repairPostApplyProbe{t: t}
	execution, err := ApplyFixPlan(context.Background(), runCtx, &FixPlan{}, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: probe,
		repairHooks: &repairExecutionHooks{BeforeRollbackMutation: func(_ int, path string) error {
			if path == "a.md" {
				return os.WriteFile(aPath, userEdit, 0o640)
			}
			return nil
		}},
	})
	require.Error(t, err)
	assert.Equal(t, []string{"a.md", "b.md"}, probe.changed,
		"completed rollback mutations must refresh before the later failure returns")
	assert.Equal(t, userEdit, mustReadFile(t, aPath))
	assert.Equal(t, bBefore, mustReadFile(t, bPath))
	require.NotNil(t, execution)
	var partial bool
	for _, transaction := range execution.Transactions {
		partial = partial || transaction.Status == "recovery_partial"
	}
	assert.True(t, partial)
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	require.Len(t, evidence, 1, "partial journal remains for operator resolution and retry")
}

func leaveCommittedRepairJournal(t *testing.T, runCtx RunContext) (RepairPlan, string, []byte) {
	t.Helper()
	path := filepath.Join(runCtx.VaultPath, "note.md")
	before := []byte("before\n")
	after := []byte("after\n")
	require.NoError(t, os.WriteFile(path, before, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:cleanup", issueKey: "issue:cleanup",
		operations: []RepairOperation{
			repairWriteOperation("operation:cleanup", "action:cleanup", "note.md", before, after),
		},
	}})

	refreshErr := errors.New("retain committed journal for cleanup test")
	_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix:                true,
		NonInteractive:     true,
		PostApplyRefresher: &repairPostApplyProbe{t: t, refreshErr: refreshErr},
	})
	require.ErrorIs(t, err, refreshErr)
	assert.Equal(t, after, mustReadFile(t, path))

	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	require.Len(t, evidence, 1)
	require.Equal(t, repairJournalCommitted, evidence[0].State)
	return plan, path, after
}
