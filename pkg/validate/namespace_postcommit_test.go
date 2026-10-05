package validate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/validate/namespaceadmission"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

func TestNamespacePostCommitBorrowsOriginalLeaseAfterRequiredCompletion(t *testing.T) {
	for _, optionalFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "optional_failure"}[optionalFailure], func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "old.md"), []byte("before\n"), 0o600))
			plan := namespaceTestPlan(t, root)
			probe := &repairPostApplyProbe{t: t, result: PostApplyRefreshResult{Paths: []string{"old.md", "new.md"}}}
			ctx := context.Background()
			var plannedLease, savedLease *IndexLockLease
			var optionalErr error
			var callbackReceipt NamespaceOutcome
			calls := 0
			result, err := ApplyNamespaceMutation(ctx, namespaceTestRunContext(root), namespaceTestPlanner(func(_ context.Context, lease *IndexLockLease) (NamespaceMutationPlan, error) {
				plannedLease = lease
				return plan, nil
			}), probe, func(callbackCtx context.Context, lease *IndexLockLease) {
				calls++
				require.Equal(t, ctx, callbackCtx)
				require.Same(t, plannedLease, lease)
				require.Same(t, probe.lease, lease)
				require.NoError(t, lease.RequireHeldForVault(root))
				savedLease = lease
				require.Equal(t, "after\n", string(mustReadFile(t, filepath.Join(root, "new.md"))))
				require.NoFileExists(t, filepath.Join(root, "old.md"))
				require.NoError(t, namespaceadmission.Check(root))
				journals, err := DetectPendingRepairJournals(namespaceTestRunContext(root))
				require.NoError(t, err)
				require.Empty(t, journals, "required cleanup precedes optional writes")
				receipts, err := os.ReadDir(filepath.Join(root, repairCompletionDirectory))
				require.NoError(t, err)
				require.Len(t, receipts, 1)
				receipt := mustReadFile(t, filepath.Join(root, repairCompletionDirectory, receipts[0].Name()))
				require.NoError(t, json.Unmarshal(receipt, &callbackReceipt))
				require.Equal(t, NamespaceCommitted, callbackReceipt.Decision)
				require.FileExists(t, filepath.Join(root, callbackReceipt.ReceiptPath))
				release, acquired, err := indexlock.TryAcquire(filepath.Join(root, ".rhizome", "index.lock"))
				require.NoError(t, err)
				if acquired {
					require.NoError(t, release())
				}
				require.False(t, acquired, "a competing writer must wait until optional completion returns")
				optionalPath := filepath.Join(root, "optional.py")
				if optionalFailure {
					require.NoError(t, os.Mkdir(optionalPath, 0o700))
				}
				optionalErr = os.WriteFile(optionalPath, []byte("# [[new]]\n"), 0o600)
			})
			require.NoError(t, err, "optional failure cannot enter required transaction policy")
			require.Equal(t, 1, calls)
			require.Equal(t, NamespaceCommitted, result.Current.Decision)
			require.False(t, result.Current.RecoveryPending)
			require.Equal(t, callbackReceipt.TransactionID, result.Current.TransactionID)
			if optionalFailure {
				require.Error(t, optionalErr)
			} else {
				require.NoError(t, optionalErr)
			}
			require.Error(t, savedLease.RequireHeld())
			namespacePostCommitLeaseReleased(t, root)
		})
	}
}

type namespacePostCommitCloseFunc func() error

func (f namespacePostCommitCloseFunc) Close() error { return f() }

func TestNamespacePostCommitReceivesCancellationAfterRequiredCompletion(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "old.md"), []byte("before\n"), 0o600))
	plan := namespaceTestPlan(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	refresh := NewPostApplyRefreshResult(&ontology.Runtime{}, namespacePostCommitCloseFunc(func() error {
		cancel()
		return nil
	}))
	refresh.Paths = []string{"old.md", "new.md"}
	probe := &repairPostApplyProbe{t: t, result: refresh}
	called, optionalStarted := false, false
	result, err := ApplyNamespaceMutation(ctx, namespaceTestRunContext(root), namespaceFixturePlanner(plan), probe, func(callbackCtx context.Context, lease *IndexLockLease) {
		called = true
		require.Equal(t, ctx, callbackCtx)
		require.ErrorIs(t, callbackCtx.Err(), context.Canceled)
		require.NoError(t, lease.RequireHeldForVault(root))
		if callbackCtx.Err() != nil {
			return
		}
		optionalStarted = true
	})
	require.NoError(t, err)
	require.True(t, called)
	require.False(t, optionalStarted)
	require.Equal(t, NamespaceCommitted, result.Current.Decision)
	require.False(t, result.Current.RecoveryPending)
	namespacePostCommitLeaseReleased(t, root)
}

func TestNamespacePostCommitSkipsRequiredFailuresAndRecovery(t *testing.T) {
	for _, stage := range []string{"dependency", "planning", "witness", "prepared", "publication", "interruption", "decision_sync", "refresh", "cleanup"} {
		t.Run(stage, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "old.md"), []byte("before\n"), 0o600))
			runCtx := namespaceTestRunContext(root)
			plan := namespaceTestPlan(t, root)
			probe := &repairPostApplyProbe{t: t, result: PostApplyRefreshResult{Paths: []string{"old.md", "new.md"}}}
			var refresher PostApplyRefresher = probe
			failure := errors.New("required work failed")
			hooks := &repairExecutionHooks{}
			switch stage {
			case "dependency":
				refresher = nil
			case "witness":
				plan.Operations[0].SourceHash = SourceHash([]byte("stale source"))
			case "prepared":
				hooks.AfterJournalPublished = func(string) error { return failure }
			case "publication":
				hooks.AfterMutation = func(int, string) error { return failure }
			case "interruption":
				hooks.AfterMutation = func(int, string) error { return errSimulatedRepairInterruption }
			case "decision_sync":
				hooks.BeforeNamespaceDecisionDirectorySync = func(string) error { return failure }
			case "refresh":
				probe.refreshErr = failure
			case "cleanup":
				hooks.AfterCleanupBoundary = func(at, _ string) error {
					if at == cleanupAfterDetach {
						return errSimulatedRepairInterruption
					}
					return nil
				}
			}
			calls := 0
			callback := func(context.Context, *IndexLockLease) { calls++ }
			_, err := applyNamespaceMutation(context.Background(), runCtx, namespaceTestPlanner(func(context.Context, *IndexLockLease) (NamespaceMutationPlan, error) {
				if stage == "planning" {
					return NamespaceMutationPlan{}, failure
				}
				return plan, nil
			}), refresher, callback, hooks)
			require.Error(t, err)
			require.Zero(t, calls)
			namespacePostCommitLeaseReleased(t, root)
			journals, err := DetectPendingRepairJournals(runCtx)
			require.NoError(t, err)
			if len(journals) != 0 {
				probe.refreshErr = nil
				result, err := ApplyNamespaceMutation(context.Background(), runCtx, namespaceRecoveryOnlyPlanner(t), probe, callback)
				require.ErrorContains(t, err, "replan")
				require.Equal(t, NamespaceNotStarted, result.Current.Decision)
				require.NotEmpty(t, result.Recovered)
				require.False(t, result.Recovered[0].RecoveryPending)
				require.Zero(t, calls, "recovered work cannot replay optional writes")
				namespacePostCommitLeaseReleased(t, root)
			}
		})
	}
}

func namespacePostCommitLeaseReleased(t *testing.T, root string) {
	t.Helper()
	release, acquired, err := indexlock.TryAcquire(filepath.Join(root, ".rhizome", "index.lock"))
	require.NoError(t, err)
	require.True(t, acquired)
	require.NoError(t, release())
}
