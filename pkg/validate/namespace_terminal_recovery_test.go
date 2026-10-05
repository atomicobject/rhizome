package validate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type namespaceCountingRefresher struct {
	probe *repairPostApplyProbe
	calls int
}

func (p *namespaceCountingRefresher) Refresh(ctx context.Context, lease *IndexLockLease, changed []string, renamed []PathRename, deleted []string) (PostApplyRefreshResult, error) {
	p.calls++
	return p.probe.Refresh(ctx, lease, changed, renamed, deleted)
}

func TestNamespaceTombstoneRetainsRecoveredOutcome(t *testing.T) {
	for _, restored := range []bool{false, true} {
		for _, boundary := range []string{cleanupAfterDetach, cleanupAfterCommittedRemoved, cleanupAfterManifestRemoved, cleanupAfterDirectoryRemoved} {
			t.Run(map[bool]string{false: "committed", true: "restored"}[restored]+"/"+boundary, func(t *testing.T) {
				root := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(root, "old.md"), []byte("before\n"), 0640))
				plan := namespaceTestPlan(t, root)
				runCtx := namespaceTestRunContext(root)
				probe := &namespaceCountingRefresher{probe: &repairPostApplyProbe{t: t, result: PostApplyRefreshResult{Paths: []string{"old.md", "new.md"}}}}
				hooks := &repairExecutionHooks{AfterCleanupBoundary: func(at, _ string) error {
					if at == boundary {
						return errSimulatedRepairInterruption
					}
					return nil
				}}
				wantDecision := NamespaceCommitted
				if restored {
					wantDecision = NamespaceRestored
					hooks.AfterMutation = func(int, string) error { return errors.New("publication rejected") }
				}
				first, err := applyNamespaceMutation(context.Background(), runCtx, namespaceTestPlanner(func(context.Context, *IndexLockLease) (NamespaceMutationPlan, error) { return plan, nil }), probe, nil, hooks)
				require.ErrorIs(t, err, errSimulatedRepairInterruption)
				require.Equal(t, wantDecision, first.Current.Decision)
				require.True(t, first.Current.RecoveryPending)
				if restored {
					require.Empty(t, first.Current.ReceiptPath)
				} else {
					require.FileExists(t, filepath.Join(root, first.Current.ReceiptPath))
				}
				require.Equal(t, 1, probe.calls)
				require.NoError(t, os.WriteFile(filepath.Join(root, "old.md"), []byte("later old\n"), 0600))
				require.NoError(t, os.WriteFile(filepath.Join(root, "new.md"), []byte("later new\n"), 0600))
				journals, err := discoverRepairJournals(runCtx)
				require.NoError(t, err)
				if boundary == cleanupAfterDirectoryRemoved {
					require.Empty(t, journals)
				} else {
					require.Len(t, journals, 1)
				}
				probe.calls = 0
				planned := false
				freshPlanErr := errors.New("new request planner entered after complete cleanup")
				recovered, err := ApplyNamespaceMutation(context.Background(), runCtx, namespaceTestPlanner(func(context.Context, *IndexLockLease) (NamespaceMutationPlan, error) {
					planned = true
					return NamespaceMutationPlan{}, freshPlanErr
				}), probe, nil)
				require.Equal(t, NamespaceNotStarted, recovered.Current.Decision)
				require.Zero(t, probe.calls, "a native cleanup tombstone has already crossed refresh and must not run a generic postcheck")
				if boundary == cleanupAfterDirectoryRemoved {
					require.True(t, planned)
					require.ErrorIs(t, err, freshPlanErr)
					require.Empty(t, recovered.Recovered, "no evidence remains to invent a historical report")
				} else {
					require.False(t, planned)
					require.ErrorContains(t, err, "replan")
					require.Len(t, recovered.Recovered, 1, "native cleanup must retain a separate native recovery outcome")
					require.Equal(t, wantDecision, recovered.Recovered[0].Decision)
					require.False(t, recovered.Recovered[0].RecoveryPending)
					if restored {
						require.Empty(t, recovered.Recovered[0].ReceiptPath)
					}
					if boundary == cleanupAfterManifestRemoved {
						require.Empty(t, recovered.Recovered[0].TransactionID)
						require.Empty(t, recovered.Recovered[0].Moves)
						require.Empty(t, recovered.Recovered[0].Summary)
						require.Empty(t, recovered.Recovered[0].ReceiptPath)
					} else {
						require.Equal(t, first.Current.TransactionID, recovered.Recovered[0].TransactionID)
					}
				}
				require.Equal(t, "later old\n", string(mustReadFile(t, filepath.Join(root, "old.md"))))
				require.Equal(t, "later new\n", string(mustReadFile(t, filepath.Join(root, "new.md"))))
			})
		}
	}
}

func TestNamespaceReplayPreservesLaterContentAndRecreatedSource(t *testing.T) {
	for _, restored := range []bool{false, true} {
		t.Run(map[bool]string{false: "committed", true: "restored"}[restored], func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "old.md"), []byte("before\n"), 0640))
			plan := namespaceTestPlan(t, root)
			runCtx := namespaceTestRunContext(root)
			blocked := errors.New("refresh unavailable")
			failProbe := &repairPostApplyProbe{t: t, refreshErr: blocked}
			var hooks *repairExecutionHooks
			if restored {
				hooks = &repairExecutionHooks{AfterMutation: func(int, string) error { return errors.New("publication rejected") }}
			}
			first, err := applyNamespaceMutation(context.Background(), runCtx, namespaceTestPlanner(func(context.Context, *IndexLockLease) (NamespaceMutationPlan, error) { return plan, nil }), failProbe, nil, hooks)
			require.ErrorIs(t, err, blocked)
			want := NamespaceCommitted
			if restored {
				want = NamespaceRestored
			}
			require.Equal(t, want, first.Current.Decision)
			require.NoError(t, os.WriteFile(filepath.Join(root, "old.md"), []byte("later old\n"), 0600))
			require.NoError(t, os.WriteFile(filepath.Join(root, "new.md"), []byte("later new\n"), 0600))
			probe := &repairPostApplyProbe{t: t, result: PostApplyRefreshResult{Paths: []string{"old.md", "new.md"}}}
			recovered, err := ApplyNamespaceMutation(context.Background(), runCtx, namespaceTestPlanner(func(context.Context, *IndexLockLease) (NamespaceMutationPlan, error) {
				t.Fatal("recovery must not call planner")
				return NamespaceMutationPlan{}, nil
			}), probe, nil)
			require.ErrorContains(t, err, "replan")
			require.Len(t, recovered.Recovered, 1)
			require.Equal(t, want, recovered.Recovered[0].Decision)
			require.False(t, recovered.Recovered[0].RecoveryPending)
			require.ElementsMatch(t, []string{"old.md", "new.md"}, probe.changed)
			require.Empty(t, probe.renamed)
			require.Empty(t, probe.deleted)
			require.Equal(t, "later old\n", string(mustReadFile(t, filepath.Join(root, "old.md"))))
			require.Equal(t, "later new\n", string(mustReadFile(t, filepath.Join(root, "new.md"))))
			require.Error(t, probe.lease.RequireHeld())
		})
	}
}
