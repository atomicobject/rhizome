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

func TestPreparedRecoveryRestoresPersistedRepairMembershipWithoutCurrentPlan(t *testing.T) {
	root := t.TempDir()
	runCtx := repairRunContext(t, root)
	beforeA := []byte("before a\n")
	beforeZ := []byte("before z\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.md"), beforeA, 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(root, "z.md"), beforeZ, 0o640))

	plan := mustRepairPlan(t, []repairPlanInput{
		{
			actionID: "action:z", issueKey: "issue:z",
			operations: []RepairOperation{withRepairIdentity(repairWriteOperation(
				"operation:z", "action:z", "z.md", beforeZ, []byte("after z\n"),
			), "membership:test")},
		},
		{
			actionID: "action:a", issueKey: "issue:a",
			operations: []RepairOperation{withRepairIdentity(repairWriteOperation(
				"operation:a", "action:a", "a.md", beforeA, []byte("after a\n"),
			), "membership:test")},
		},
	})
	require.Len(t, plan.Transactions, 1)

	_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{AfterJournalPublished: func(string) error {
			return errSimulatedRepairInterruption
		}},
	})
	require.ErrorIs(t, err, errSimulatedRepairInterruption)

	dir, err := repairJournalDir(runCtx, plan.Transactions[0].ID)
	require.NoError(t, err)
	manifest, err := readRepairJournalManifest(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"operation:a", "operation:z"}, manifest.OperationIDs)
	assert.Equal(t, []string{"action:a", "action:z"}, manifest.ActionIDs)
	assert.Equal(t, []string{"issue:a", "issue:z"}, manifest.IssueKeys)

	execution, err := ApplyFixPlan(context.Background(), runCtx, &FixPlan{}, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
	})
	require.NoError(t, err)
	assert.Empty(t, execution.Applied)
	assert.Empty(t, execution.Skipped)
	assert.Equal(t, []string{"action:a", "action:z"}, execution.Failed)
	assert.Equal(t, []string{"issue:a", "issue:z"}, execution.RemainingIssueKeys)
	require.Len(t, execution.Transactions, 1)
	assert.Equal(t, "recovered_prepared", execution.Transactions[0].Status)
	assert.Equal(t, []string{"operation:a", "operation:z"}, execution.Transactions[0].OperationIDs)
}

func withRepairIdentity(operation RepairOperation, identity string) RepairOperation {
	operation.Identities = append(operation.Identities, identity)
	return operation
}

func TestCommittedRecoveryRestoresOriginalMembershipAlongsideDifferentPlan(t *testing.T) {
	root := t.TempDir()
	runCtx := repairRunContext(t, root)
	originalBefore := []byte("original before\n")
	newBefore := []byte("new before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "original.md"), originalBefore, 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(root, "new.md"), newBefore, 0o640))

	originalPlan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:original", issueKey: "issue:original",
		operations: []RepairOperation{repairWriteOperation(
			"operation:original", "action:original", "original.md", originalBefore, []byte("original after\n"),
		)},
	}})
	retainJournal := errors.New("retain committed journal")
	_, err := ApplyFixPlan(context.Background(), runCtx, &originalPlan, Options{
		Fix: true, NonInteractive: true,
		PostApplyRefresher: &repairPostApplyProbe{t: t, refreshErr: retainJournal},
	})
	require.ErrorIs(t, err, retainJournal)

	newPlan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:new-confirm", issueKey: "issue:new-confirm",
		operations: []RepairOperation{repairWriteOperation(
			"operation:new-confirm", "action:new-confirm", "new.md", newBefore, []byte("new after\n"),
		)},
	}})
	newPlan.Actions[0].Safety = FixSafetyConfirm
	newPlan, err = FinalizeRepairPlan(newPlan)
	require.NoError(t, err)

	execution, err := ApplyFixPlan(context.Background(), runCtx, &newPlan, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
	})
	require.ErrorContains(t, err, "replan")
	assert.Equal(t, []string{"action:original"}, execution.Applied)
	assert.Equal(t, 1, execution.AppliedTransactions)
	assert.Equal(t, 1, execution.AppliedWrites)
	assert.Equal(t, []string{"action:new-confirm"}, execution.Skipped)
	assert.Empty(t, execution.Failed)
	assert.Equal(t, []string{"issue:new-confirm"}, execution.RemainingIssueKeys)
	require.Len(t, execution.Transactions, 2)
	assert.Equal(t, "recovered_committed", execution.Transactions[0].Status)
	assert.Equal(t, []string{"operation:original"}, execution.Transactions[0].OperationIDs)
	assert.Equal(t, "skipped", execution.Transactions[1].Status)
	assert.Equal(t, newBefore, mustReadFile(t, filepath.Join(root, "new.md")))
}

func TestLegacyJournalV1WithoutRepairMembershipRemainsRecoverable(t *testing.T) {
	root := t.TempDir()
	runCtx := repairRunContext(t, root)
	before := []byte("before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), before, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:legacy", issueKey: "issue:legacy",
		operations: []RepairOperation{repairWriteOperation(
			"operation:legacy", "action:legacy", "note.md", before, []byte("after\n"),
		)},
	}})

	_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{AfterJournalPublished: func(string) error {
			return errSimulatedRepairInterruption
		}},
	})
	require.ErrorIs(t, err, errSimulatedRepairInterruption)
	dir, err := repairJournalDir(runCtx, plan.Transactions[0].ID)
	require.NoError(t, err)
	manifest, err := readRepairJournalManifest(dir)
	require.NoError(t, err)
	manifest.OperationIDs = nil
	manifest.ActionIDs = nil
	manifest.IssueKeys = nil
	require.NoError(t, writeRepairJournalManifest(dir, manifest))

	execution, err := ApplyFixPlan(context.Background(), runCtx, &FixPlan{}, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
	})
	require.NoError(t, err)
	assert.Empty(t, execution.Applied)
	assert.Empty(t, execution.Skipped)
	assert.Empty(t, execution.Failed)
	assert.Empty(t, execution.RemainingIssueKeys)
	evidence, err := DetectPendingRepairJournals(runCtx)
	require.NoError(t, err)
	assert.Empty(t, evidence)
}

func TestCleanupRecoveryDiscardsInvalidBestEffortMembership(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*repairJournalManifest)
		wantApplied bool
	}{
		{
			name: "action and issue membership tampered",
			mutate: func(manifest *repairJournalManifest) {
				manifest.ActionIDs = []string{"action:forged"}
				manifest.IssueKeys = []string{"issue:a", "issue:z"}
			},
			wantApplied: true,
		},
		{
			name: "membership partial",
			mutate: func(manifest *repairJournalManifest) {
				manifest.IssueKeys = nil
			},
			wantApplied: true,
		},
		{
			name: "operation membership unsorted",
			mutate: func(manifest *repairJournalManifest) {
				manifest.OperationIDs[0], manifest.OperationIDs[1] = manifest.OperationIDs[1], manifest.OperationIDs[0]
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			runCtx := repairRunContext(t, root)
			beforeA := []byte("before a\n")
			beforeZ := []byte("before z\n")
			require.NoError(t, os.WriteFile(filepath.Join(root, "a.md"), beforeA, 0o640))
			require.NoError(t, os.WriteFile(filepath.Join(root, "z.md"), beforeZ, 0o640))
			plan := mustRepairPlan(t, []repairPlanInput{
				{
					actionID: "action:z", issueKey: "issue:z",
					operations: []RepairOperation{withRepairIdentity(repairWriteOperation(
						"operation:z", "action:z", "z.md", beforeZ, []byte("after z\n"),
					), "membership:cleanup")},
				},
				{
					actionID: "action:a", issueKey: "issue:a",
					operations: []RepairOperation{withRepairIdentity(repairWriteOperation(
						"operation:a", "action:a", "a.md", beforeA, []byte("after a\n"),
					), "membership:cleanup")},
				},
			})
			require.Len(t, plan.Transactions, 1)

			_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
				Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
				repairHooks: &repairExecutionHooks{AfterCleanupBoundary: func(boundary, _ string) error {
					if boundary == cleanupAfterDetach {
						return errSimulatedRepairInterruption
					}
					return nil
				}},
			})
			require.ErrorIs(t, err, errSimulatedRepairInterruption)

			rootDir, err := repairJournalRoot(runCtx)
			require.NoError(t, err)
			entries, err := os.ReadDir(rootDir)
			require.NoError(t, err)
			require.Len(t, entries, 1)
			tombstone := filepath.Join(rootDir, entries[0].Name())
			manifest, err := readRepairJournalManifest(tombstone)
			require.NoError(t, err)
			tt.mutate(&manifest)
			require.NoError(t, writeRepairJournalManifest(tombstone, manifest))

			execution, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
				Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
				postApplyCheck: func(context.Context, *IndexLockLease, *ontology.Runtime, repairPostApplyScope) error {
					return nil
				},
			})
			require.ErrorContains(t, err, "terminal repair cleanup recovered; replan")
			assert.NotContains(t, execution.Applied, "action:forged")
			if tt.wantApplied {
				assert.Equal(t, []string{"action:a", "action:z"}, execution.Applied)
				assert.Empty(t, execution.Failed)
				assert.Empty(t, execution.RemainingIssueKeys)
			} else {
				assert.Empty(t, execution.Applied)
				assert.Equal(t, []string{"action:a", "action:z"}, execution.Failed)
				assert.Equal(t, []string{"issue:a", "issue:z"}, execution.RemainingIssueKeys)
			}
			evidence, detectErr := DetectPendingRepairJournals(runCtx)
			require.NoError(t, detectErr)
			assert.Empty(t, evidence)
		})
	}
}
