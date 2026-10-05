package validate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/validate/namespaceadmission"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type namespaceTestPlanner func(context.Context, *IndexLockLease) (NamespaceMutationPlan, error)

func (p namespaceTestPlanner) PlanNamespaceMutation(ctx context.Context, lease *IndexLockLease) (NamespaceMutationPlan, error) {
	return p(ctx, lease)
}

func namespaceTestRunContext(root string) RunContext {
	return RunContext{VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}}
}

func namespaceTestPlan(t *testing.T, root string) NamespaceMutationPlan {
	t.Helper()
	content := mustReadFile(t, filepath.Join(root, "old.md"))
	info, err := os.Stat(filepath.Join(root, "old.md"))
	require.NoError(t, err)
	mode := repairModeBits(info.Mode())
	return NamespaceMutationPlan{Operations: []RepairOperation{
		{Kind: RepairOperationRename, Path: "old.md", DestinationPath: "new.md", SourceHash: SourceHash(content), SourceMode: &mode, DestinationState: &RepairDestinationState{Kind: RepairDestinationAbsent}},
		{Kind: RepairOperationWrite, Path: "old.md", SourceHash: SourceHash(content), SourceMode: &mode, Content: []byte("after\n")},
	}, Summary: []byte(`{"count":1}`)}
}

func TestNamespaceSessionPublishesRequiredWritesUnderBorrowedLease(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "old.md"), []byte("before\n"), 0o640))
	plan := namespaceTestPlan(t, root)
	probe := &repairPostApplyProbe{t: t, result: PostApplyRefreshResult{Paths: []string{"old.md", "new.md"}}}
	result, err := ApplyNamespaceMutation(context.Background(), namespaceTestRunContext(root), namespaceTestPlanner(func(_ context.Context, lease *IndexLockLease) (NamespaceMutationPlan, error) {
		require.NoError(t, lease.RequireHeldForVault(root))
		return plan, nil
	}), probe, nil)
	require.NoError(t, err)
	require.Equal(t, NamespaceCommitted, result.Current.Decision)
	require.False(t, result.Current.RecoveryPending)
	require.FileExists(t, filepath.Join(root, result.Current.ReceiptPath))
	require.Equal(t, []byte("after\n"), mustReadFile(t, filepath.Join(root, "new.md")))
	require.NoFileExists(t, filepath.Join(root, "old.md"))
	require.Equal(t, []PathRename{{From: "old.md", To: "new.md"}}, probe.renamed)
	require.Equal(t, []string{"new.md"}, probe.changed)
	require.Error(t, probe.lease.RequireHeld())
	require.NoError(t, namespaceadmission.Check(root))
	journals, err := DetectPendingRepairJournals(namespaceTestRunContext(root))
	require.NoError(t, err)
	require.Empty(t, journals)
}

func TestNamespaceSessionMissingRefresherFailsBeforePlannerOrPublication(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "old.md"), []byte("before\n"), 0o600))
	result, err := ApplyNamespaceMutation(context.Background(), namespaceTestRunContext(root), namespaceTestPlanner(func(context.Context, *IndexLockLease) (NamespaceMutationPlan, error) {
		t.Fatal("planner must not run without refresh capability")
		return NamespaceMutationPlan{}, nil
	}), nil, nil)
	require.ErrorContains(t, err, "post-apply refresher")
	require.Equal(t, NamespaceNotStarted, result.Current.Decision)
	require.Equal(t, []byte("before\n"), mustReadFile(t, filepath.Join(root, "old.md")))
	require.NoDirExists(t, filepath.Join(root, ".rhizome"))
}

func TestNamespaceSessionPreparesNestedDestinationAndRestoresFiles(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[fail], func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "old.md"), []byte("before\n"), 0o600))
			plan := namespaceTestPlan(t, root)
			plan.Operations[0].DestinationPath = "Folder/Nested/new.md"
			probe := &repairPostApplyProbe{t: t, result: PostApplyRefreshResult{Paths: []string{"old.md", "Folder/Nested/new.md"}}}
			var hooks *repairExecutionHooks
			if fail {
				hooks = &repairExecutionHooks{AfterMutation: func(int, string) error { return errors.New("nested write failed") }}
			}
			result, err := applyNamespaceMutation(context.Background(), namespaceTestRunContext(root), namespaceTestPlanner(func(context.Context, *IndexLockLease) (NamespaceMutationPlan, error) { return plan, nil }), probe, nil, hooks)
			if fail {
				require.ErrorContains(t, err, "nested write failed")
				require.Equal(t, NamespaceRestored, result.Current.Decision)
				require.Equal(t, []byte("before\n"), mustReadFile(t, filepath.Join(root, "old.md")))
				require.NoFileExists(t, filepath.Join(root, "Folder/Nested/new.md"))
			} else {
				require.NoError(t, err)
				require.Equal(t, NamespaceCommitted, result.Current.Decision)
				require.Equal(t, []byte("after\n"), mustReadFile(t, filepath.Join(root, "Folder/Nested/new.md")))
			}
			require.DirExists(t, filepath.Join(root, "Folder/Nested"))
		})
	}
}

func TestNamespaceSessionRollbackAndInterruptedRecovery(t *testing.T) {
	for _, crash := range []bool{false, true} {
		t.Run(map[bool]string{false: "failure", true: "crash"}[crash], func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "old.md"), []byte("before\n"), 0o640))
			plan := namespaceTestPlan(t, root)
			failure := errors.New("write failed")
			if crash {
				failure = errSimulatedRepairInterruption
			}
			probe := &repairPostApplyProbe{t: t, result: PostApplyRefreshResult{Paths: []string{"old.md", "new.md"}}}
			planner := namespaceTestPlanner(func(context.Context, *IndexLockLease) (NamespaceMutationPlan, error) { return plan, nil })
			result, err := applyNamespaceMutation(context.Background(), namespaceTestRunContext(root), planner, probe, nil, &repairExecutionHooks{AfterMutation: func(int, string) error { return failure }})
			require.ErrorIs(t, err, failure)
			if crash {
				require.Equal(t, NamespaceUnresolved, result.Current.Decision)
				require.Empty(t, result.Current.ReceiptPath)
				require.Error(t, namespaceadmission.Check(root))
				result, err = ApplyNamespaceMutation(context.Background(), namespaceTestRunContext(root), namespaceTestPlanner(func(context.Context, *IndexLockLease) (NamespaceMutationPlan, error) {
					t.Fatal("older recovery must not plan the new request")
					return NamespaceMutationPlan{}, nil
				}), probe, nil)
				require.ErrorContains(t, err, "replan")
				require.Equal(t, NamespaceNotStarted, result.Current.Decision)
				require.Len(t, result.Recovered, 1)
				require.Equal(t, NamespaceRestored, result.Recovered[0].Decision)
				require.Empty(t, result.Recovered[0].ReceiptPath)
				require.False(t, result.Recovered[0].RecoveryPending)
			} else {
				require.Equal(t, NamespaceRestored, result.Current.Decision)
				require.Empty(t, result.Current.ReceiptPath)
			}
			require.Equal(t, []byte("before\n"), mustReadFile(t, filepath.Join(root, "old.md")))
			require.NoFileExists(t, filepath.Join(root, "new.md"))
			require.Empty(t, probe.renamed)
			require.NoError(t, namespaceadmission.Check(root))
		})
	}
}
