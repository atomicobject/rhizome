package validate

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyFixPlanRefreshesPreparedRollbackDeltaAndPostchecksUnderLease(t *testing.T) {
	root := t.TempDir()
	changedBefore := []byte("before\n")
	changedAfter := []byte("after\n")
	renamedBefore := []byte("rename\n")
	deletedBefore := []byte("delete\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "changed.md"), changedBefore, 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(root, "old.md"), renamedBefore, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "deleted.md"), deletedBefore, 0o644))
	runCtx := repairRunContext(t, root)
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:prepared-recovery", issueKey: "issue:prepared-recovery",
		operations: []RepairOperation{
			repairWriteOperation("op-prepared-recovery", "action:prepared-recovery", "changed.md", changedBefore, changedAfter),
			{
				ID: "op-prepared-rename", Kind: RepairOperationRename, Path: "old.md",
				DestinationPath: "new.md", SourceHash: SourceHash(renamedBefore),
			},
			{
				ID: "op-prepared-delete", Kind: RepairOperationDelete, Path: "deleted.md",
				SourceHash: SourceHash(deletedBefore),
			},
		},
	}})
	lastInstall := len(plan.Transactions[0].AffectedPaths) - 1

	_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{AfterInstall: func(index int, _ string) error {
			if index == lastInstall {
				return errSimulatedRepairInterruption
			}
			return nil
		}},
	})
	require.ErrorIs(t, err, errSimulatedRepairInterruption)
	assert.Equal(t, changedAfter, mustReadFile(t, filepath.Join(root, "changed.md")))
	assert.NoFileExists(t, filepath.Join(root, "old.md"))
	assert.Equal(t, renamedBefore, mustReadFile(t, filepath.Join(root, "new.md")))
	assert.NoFileExists(t, filepath.Join(root, "deleted.md"))

	probe := &repairPostApplyProbe{t: t}
	postchecked := false
	emptyPlan := RepairPlan{}
	execution, err := ApplyFixPlan(context.Background(), runCtx, &emptyPlan, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: probe,
		postApplyCheck: func(_ context.Context, lease *IndexLockLease, runtime *ontology.Runtime, _ repairPostApplyScope) error {
			require.NoError(t, lease.RequireHeld())
			assert.NotNil(t, runtime)
			assert.Equal(t, []string{"changed.md", "deleted.md"}, probe.changed, "rollback delta must refresh restored paths before postcheck")
			assert.Equal(t, []PathRename{{From: "new.md", To: "old.md"}}, probe.renamed)
			assert.Empty(t, probe.deleted)
			postchecked = true
			return nil
		},
	})
	require.NoError(t, err)
	require.NotNil(t, execution)
	assert.Equal(t, changedBefore, mustReadFile(t, filepath.Join(root, "changed.md")))
	assert.Equal(t, renamedBefore, mustReadFile(t, filepath.Join(root, "old.md")))
	assert.NoFileExists(t, filepath.Join(root, "new.md"))
	assert.Equal(t, deletedBefore, mustReadFile(t, filepath.Join(root, "deleted.md")))
	assert.Equal(t, []string{"changed.md", "deleted.md"}, probe.changed)
	assert.Equal(t, []PathRename{{From: "new.md", To: "old.md"}}, probe.renamed)
	assert.Empty(t, probe.deleted)
	assert.True(t, postchecked)
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	assert.Empty(t, evidence)
}

func TestApplyFixPlanRecoversPendingJournalBeforeZeroOperationActionPlanReturn(t *testing.T) {
	root := t.TempDir()
	before := []byte("original\n")
	after := []byte("partially installed\n")
	path := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(path, before, 0o640))
	runCtx := repairRunContext(t, root)
	pendingPlan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:pending", issueKey: "issue:pending",
		operations: []RepairOperation{
			repairWriteOperation("op-pending", "action:pending", "note.md", before, after),
		},
	}})

	_, err := ApplyFixPlan(context.Background(), runCtx, &pendingPlan, Options{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{AfterInstall: func(index int, _ string) error {
			if index == 0 {
				return errSimulatedRepairInterruption
			}
			return nil
		}},
	})
	require.ErrorIs(t, err, errSimulatedRepairInterruption)
	assert.Equal(t, after, mustReadFile(t, path))

	probe := &repairPostApplyProbe{t: t}
	actionOnly := FixPlan{Actions: []FixAction{{
		ID: "action:no-edit", Safety: FixSafetySafe, IssueKeys: []string{"issue:no-edit"},
	}}}
	execution, err := ApplyFixPlan(context.Background(), runCtx, &actionOnly, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: probe,
	})
	require.NoError(t, err)
	require.NotNil(t, execution)
	assert.Equal(t, before, mustReadFile(t, path), "recovery must precede the zero-operation return")
	assert.Equal(t, []string{"note.md"}, probe.changed)
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	assert.Empty(t, evidence)
}

func TestApplyFixPlanRejectsConcurrentSourceChangeBeforeFirstMutation(t *testing.T) {
	root := t.TempDir()
	before := []byte("planned source\n")
	after := []byte("planned repair\n")
	userEdit := []byte("concurrent user edit\n")
	path := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(path, before, 0o640))
	runCtx := repairRunContext(t, root)
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:toctou", issueKey: "issue:toctou",
		operations: []RepairOperation{
			repairWriteOperation("op-toctou", "action:toctou", "note.md", before, after),
		},
	}})

	execution, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{BeforeInstall: func(index int, _ string) error {
			if index == 0 {
				return os.WriteFile(path, userEdit, 0o640)
			}
			return nil
		}},
	})
	require.Error(t, err)
	require.NotNil(t, execution)
	assert.Equal(t, userEdit, mustReadFile(t, path), "a post-prepare user edit must never be overwritten")
	require.Len(t, execution.Transactions, 1)
	assert.Equal(t, "failed", execution.Transactions[0].Status)
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	require.Len(t, evidence, 1, "blocked rollback keeps durable recovery evidence")
	assert.Equal(t, repairJournalPrepared, evidence[0].State)
}

func TestApplyFixPlanRejectsConcurrentSourceModeChangeBeforeFirstMutation(t *testing.T) {
	root := t.TempDir()
	before := []byte("planned source\n")
	path := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(path, before, 0o640))
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	originalMode := mustRepairMode(t, path)
	requestedMode := os.FileMode(0o600)
	if runtime.GOOS == "windows" {
		requestedMode = 0o444
	}
	var changedMode os.FileMode
	runCtx := repairRunContext(t, root)
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:mode-toctou", issueKey: "issue:mode-toctou",
		operations: []RepairOperation{
			repairWriteOperation("op-mode-toctou", "action:mode-toctou", "note.md", before, []byte("planned repair\n")),
		},
	}})

	_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{BeforeInstall: func(index int, _ string) error {
			if index == 0 {
				if err := os.Chmod(path, requestedMode); err != nil {
					return err
				}
				changedMode = mustRepairMode(t, path)
			}
			return nil
		}},
	})
	require.Error(t, err)
	assert.Equal(t, before, mustReadFile(t, path))
	require.NotEqual(t, originalMode, changedMode, "fixture must create a platform-observable mode change")
	assert.Equal(t, changedMode, mustRepairMode(t, path), "post-prepare chmod must not be overwritten")
}

func TestApplyFixPlanRejectsConcurrentSourceDeletionBeforeFirstMutation(t *testing.T) {
	root := t.TempDir()
	before := []byte("planned source\n")
	path := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(path, before, 0o640))
	runCtx := repairRunContext(t, root)
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:delete-toctou", issueKey: "issue:delete-toctou",
		operations: []RepairOperation{
			repairWriteOperation("op-delete-toctou", "action:delete-toctou", "note.md", before, []byte("planned repair\n")),
		},
	}})

	_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{BeforeInstall: func(index int, _ string) error {
			if index == 0 {
				return os.Remove(path)
			}
			return nil
		}},
	})
	require.Error(t, err)
	assert.NoFileExists(t, path, "post-prepare deletion must not be resurrected or replaced")
}

func TestApplyFixPlanRejectsParentSymlinkSwapBeforeFirstMutation(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	parent := filepath.Join(root, "notes")
	parked := filepath.Join(root, "notes-parked")
	require.NoError(t, os.Mkdir(parent, 0o755))
	before := []byte("inside original\n")
	after := []byte("inside repaired\n")
	require.NoError(t, os.WriteFile(filepath.Join(parent, "note.md"), before, 0o640))
	outsideSentinel := []byte("outside sentinel\n")
	outsidePath := filepath.Join(outside, "note.md")
	require.NoError(t, os.WriteFile(outsidePath, outsideSentinel, 0o600))
	runCtx := repairRunContext(t, root)
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:symlink-swap", issueKey: "issue:symlink-swap",
		operations: []RepairOperation{
			repairWriteOperation("op-symlink-swap", "action:symlink-swap", "notes/note.md", before, after),
		},
	}})

	execution, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{BeforeInstall: func(index int, _ string) error {
			if index != 0 {
				return nil
			}
			if err := os.Rename(parent, parked); err != nil {
				return err
			}
			if err := os.Symlink(outside, parent); err != nil {
				return err
			}
			stages, err := filepath.Glob(filepath.Join(parked, "note.md.rzm-repair-*.stage"))
			if err != nil {
				return err
			}
			if len(stages) != 1 {
				t.Fatalf("expected one prepared stage artifact, got %v", stages)
			}
			staged, err := os.ReadFile(stages[0])
			if err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(outside, filepath.Base(stages[0])), staged, 0o600)
		}},
	})
	require.Error(t, err)
	require.NotNil(t, execution)
	assert.ErrorContains(t, err, "outside vault")
	assert.Equal(t, outsideSentinel, mustReadFile(t, outsidePath), "repair must not follow a swapped parent outside the vault")
	assert.Equal(t, before, mustReadFile(t, filepath.Join(parked, "note.md")))
	require.Len(t, execution.Transactions, 1)
	assert.Equal(t, "failed", execution.Transactions[0].Status)
}
