package validate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyFixPlanLocksVaultDefinitionRootWhenPathIsOmitted(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, ".rhizome", "index.lock")
	release, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	defer func() { _ = release() }()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := ApplyFixPlan(ctx, RunContext{VaultDef: obsidian.VaultDefinition{Path: root}}, nil, Options{Fix: true})
		done <- err
	}()
	require.Eventually(t, func() bool { return indexlock.CheckPriority(lockPath) }, time.Second, 10*time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("repair did not cancel its vault writer wait")
	}
}

func TestApplyFixPlanComposesEditIntoRenameDestination(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\r\n")
	source := filepath.Join(root, "old.md")
	require.NoError(t, os.WriteFile(source, before, 0o751))
	wantMode := mustRepairMode(t, source)
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:move-edit", issueKey: "issue:move-edit",
		operations: []RepairOperation{
			{
				ID: "write", Kind: RepairOperationWrite, Path: "old.md", SourceHash: SourceHash(before),
				Expected: []ExpectedText{{StartByte: 0, EndByte: len(before), Text: string(before), Replacement: "after\r\n"}},
				Content:  []byte("after\r\n"),
			},
			{
				ID: "rename", Kind: RepairOperationRename, Path: "old.md", DestinationPath: "new.md",
				SourceHash: SourceHash(before),
			},
		},
	}})

	execution, err := ApplyFixPlan(
		context.Background(), repairRunContext(t, root), &plan,
		Options{Fix: true, NonInteractive: true},
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"action:move-edit"}, execution.Applied)
	assert.NoFileExists(t, filepath.Join(root, "old.md"))
	assert.Equal(t, []byte("after\r\n"), mustReadFile(t, filepath.Join(root, "new.md")))
	assert.Equal(t, wantMode, mustRepairMode(t, filepath.Join(root, "new.md")))
}

func TestApplyFixPlanRollsBackInstalledFileAfterLaterCommitFailure(t *testing.T) {
	root := t.TempDir()
	first := []byte("first\n")
	second := []byte("second\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "first.md"), first, 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(root, "second.md"), second, 0o600))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:rollback", issueKey: "issue:rollback",
		operations: []RepairOperation{
			repairWriteOperation("op-first", "action:rollback", "first.md", first, []byte("first changed\n")),
			repairWriteOperation("op-second", "action:rollback", "second.md", second, []byte("second changed\n")),
		},
	}})
	injected := errors.New("injected second install failure")

	execution, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{BeforeInstall: func(index int, _ string) error {
			if index == 1 {
				return injected
			}
			return nil
		}},
	})

	require.ErrorIs(t, err, injected)
	assert.Empty(t, execution.Applied)
	assert.Equal(t, first, mustReadFile(t, filepath.Join(root, "first.md")))
	assert.Equal(t, second, mustReadFile(t, filepath.Join(root, "second.md")))
	evidence, detectErr := DetectPendingRepairJournals(repairRunContext(t, root))
	require.NoError(t, detectErr)
	assert.Empty(t, evidence)
}

func TestApplyFixPlanRestoresDeletedFileAfterLaterCommitFailure(t *testing.T) {
	root := t.TempDir()
	deleted := []byte("delete me\n")
	written := []byte("write me\n")
	deletedPath := filepath.Join(root, "a-delete.md")
	require.NoError(t, os.WriteFile(deletedPath, deleted, 0o751))
	wantMode := mustRepairMode(t, deletedPath)
	require.NoError(t, os.WriteFile(filepath.Join(root, "z-write.md"), written, 0o640))
	deleteOperation := RepairOperation{
		ID: "op-delete", Kind: RepairOperationDelete, Path: "a-delete.md", SourceHash: SourceHash(deleted),
	}
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:delete-rollback", issueKey: "issue:delete-rollback",
		operations: []RepairOperation{
			deleteOperation,
			repairWriteOperation("op-write", "action:delete-rollback", "z-write.md", written, []byte("changed\n")),
		},
	}})
	injected := errors.New("injected failure after delete")

	_, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{BeforeInstall: func(index int, _ string) error {
			if index == 1 {
				return injected
			}
			return nil
		}},
	})

	require.ErrorIs(t, err, injected)
	assert.Equal(t, deleted, mustReadFile(t, filepath.Join(root, "a-delete.md")))
	assert.Equal(t, wantMode, mustRepairMode(t, deletedPath))
	assert.Equal(t, written, mustReadFile(t, filepath.Join(root, "z-write.md")))
}

func TestApplyFixPlanRecoversPreparedJournalBeforeRetry(t *testing.T) {
	root := t.TempDir()
	first := []byte("first\n")
	second := []byte("second\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "first.md"), first, 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(root, "second.md"), second, 0o600))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:interrupt", issueKey: "issue:interrupt",
		operations: []RepairOperation{
			repairWriteOperation("op-first", "action:interrupt", "first.md", first, []byte("first changed\n")),
			repairWriteOperation("op-second", "action:interrupt", "second.md", second, []byte("second changed\n")),
		},
	}})
	runCtx := repairRunContext(t, root)

	_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{AfterInstall: func(index int, _ string) error {
			if index == 0 {
				return errSimulatedRepairInterruption
			}
			return nil
		}},
	})
	require.ErrorIs(t, err, errSimulatedRepairInterruption)
	evidence, err := DetectPendingRepairJournals(runCtx)
	require.NoError(t, err)
	require.Len(t, evidence, 1)
	assert.Equal(t, repairJournalPrepared, evidence[0].State)

	_, err = ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
	})
	require.ErrorContains(t, err, "replan")
	assert.Equal(t, first, mustReadFile(t, filepath.Join(root, "first.md")))
	assert.Equal(t, second, mustReadFile(t, filepath.Join(root, "second.md")))

	execution, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{Fix: true, NonInteractive: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"action:interrupt"}, execution.Applied)
	assert.Equal(t, []byte("first changed\n"), mustReadFile(t, filepath.Join(root, "first.md")))
	assert.Equal(t, []byte("second changed\n"), mustReadFile(t, filepath.Join(root, "second.md")))
	evidence, err = DetectPendingRepairJournals(runCtx)
	require.NoError(t, err)
	assert.Empty(t, evidence)
}

func TestApplyFixPlanCancellationPreservesConcurrentIndexLockHolder(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	path := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(path, before, 0o640))
	runCtx := repairRunContext(t, root)
	release, acquired, err := indexlock.TryAcquire(filepath.Join(root, ".rhizome", "index.lock"))
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { _ = release() })
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:locked", issueKey: "issue:locked",
		operations: []RepairOperation{repairWriteOperation(
			"op-locked", "action:locked", "note.md", before, []byte("after\n"),
		)},
	}})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	execution, err := ApplyFixPlan(ctx, runCtx, &plan, Options{Fix: true, NonInteractive: true})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.FileExists(t, filepath.Join(root, ".rhizome", "index.lock"))
	require.False(t, indexlock.CheckPriority(filepath.Join(root, ".rhizome", "index.lock")))
	assert.Empty(t, execution.Applied)
	assert.Equal(t, before, mustReadFile(t, path))
}

func TestApplyFixPlanAppliesIndependentTransactionWhenAnotherIsStale(t *testing.T) {
	root := t.TempDir()
	staleBefore := []byte("stale before\n")
	validBefore := []byte("valid before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "stale.md"), staleBefore, 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(root, "valid.md"), validBefore, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{
		{
			actionID: "action:stale", issueKey: "issue:stale",
			operations: []RepairOperation{{
				ID: "op-stale", Kind: RepairOperationWrite, Path: "stale.md",
				SourceHash: SourceHash([]byte("different source\n")), Content: []byte("must not apply\n"),
			}},
		},
		{
			actionID: "action:valid", issueKey: "issue:valid",
			operations: []RepairOperation{repairWriteOperation(
				"op-valid", "action:valid", "valid.md", validBefore, []byte("valid after\n"),
			)},
		},
	})

	execution, err := ApplyFixPlan(
		context.Background(), repairRunContext(t, root), &plan,
		Options{Fix: true, NonInteractive: true},
	)

	require.ErrorContains(t, err, "stale")
	assert.Equal(t, []string{"action:valid"}, execution.Applied)
	assert.Equal(t, staleBefore, mustReadFile(t, filepath.Join(root, "stale.md")))
	assert.Equal(t, []byte("valid after\n"), mustReadFile(t, filepath.Join(root, "valid.md")))
	statuses := map[string]string{}
	for _, transaction := range execution.Transactions {
		statuses[transaction.OperationIDs[0]] = transaction.Status
	}
	assert.Equal(t, "failed_stale", statuses["op-stale"])
	assert.Equal(t, "applied", statuses["op-valid"])
}

func TestApplyFixPlanRejectsExpectedTextMismatchWithMatchingSourceHash(t *testing.T) {
	root := t.TempDir()
	before := []byte("exact source\n")
	path := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(path, before, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:expected", issueKey: "issue:expected",
		operations: []RepairOperation{{
			ID: "op-expected", Kind: RepairOperationWrite, Path: "note.md",
			SourceHash: SourceHash(before), Content: []byte("after\n"),
			Expected: []ExpectedText{{StartByte: 0, EndByte: 5, Text: "wrong"}},
		}},
	}})

	_, err := ApplyFixPlan(
		context.Background(), repairRunContext(t, root), &plan,
		Options{Fix: true, NonInteractive: true},
	)

	require.ErrorContains(t, err, "expected text mismatch")
	assert.Equal(t, before, mustReadFile(t, path))
}

func TestRecoveryFailureBlocksNewRepairAndRetainsEvidence(t *testing.T) {
	root := t.TempDir()
	firstBefore := []byte("first before\n")
	firstAfter := []byte("first after\n")
	secondBefore := []byte("second before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "first.md"), firstBefore, 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(root, "second.md"), secondBefore, 0o640))
	runCtx := repairRunContext(t, root)
	firstPlan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:first", issueKey: "issue:first",
		operations: []RepairOperation{repairWriteOperation(
			"op-first", "action:first", "first.md", firstBefore, firstAfter,
		)},
	}})
	refreshErr := errors.New("leave committed recovery evidence")
	_, err := ApplyFixPlan(context.Background(), runCtx, &firstPlan, Options{
		Fix: true, NonInteractive: true,
		PostApplyRefresher: &repairPostApplyProbe{t: t, refreshErr: refreshErr},
	})
	require.ErrorIs(t, err, refreshErr)
	require.NoError(t, os.WriteFile(filepath.Join(root, "first.md"), []byte("tampered\n"), 0o640))
	secondPlan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:second", issueKey: "issue:second",
		operations: []RepairOperation{repairWriteOperation(
			"op-second", "action:second", "second.md", secondBefore, []byte("second after\n"),
		)},
	}})

	_, err = ApplyFixPlan(context.Background(), runCtx, &secondPlan, Options{Fix: true, NonInteractive: true})

	require.ErrorContains(t, err, "recover committed transaction")
	assert.Equal(t, secondBefore, mustReadFile(t, filepath.Join(root, "second.md")))
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	require.Len(t, evidence, 1)
	assert.Equal(t, repairJournalCommitted, evidence[0].State)
}

func TestApplyFixPlanRechecksSourceAfterWaitingForIndexLock(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	path := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(path, before, 0o640))
	runCtx := repairRunContext(t, root)
	lockPath := filepath.Join(root, ".rhizome", "index.lock")
	release, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { _ = release() })
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:waiting", issueKey: "issue:waiting",
		operations: []RepairOperation{repairWriteOperation("op-waiting", "action:waiting", "note.md", before, []byte("my edit\n"))},
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type result struct {
		execution *FixExecution
		err       error
	}
	done := make(chan result, 1)
	go func() {
		execution, err := ApplyFixPlan(ctx, runCtx, &plan, Options{Fix: true, NonInteractive: true})
		done <- result{execution, err}
	}()
	require.Eventually(t, func() bool { return indexlock.CheckPriority(lockPath) }, time.Second, 5*time.Millisecond)
	external := []byte("another writer's edit\n")
	require.NoError(t, os.WriteFile(path, external, 0o640))
	require.NoError(t, release())
	select {
	case got := <-done:
		require.Empty(t, got.execution.Applied)
		require.Contains(t, got.execution.Failed, "action:waiting")
	case <-ctx.Done():
		t.Fatal("save did not finish after the lock was released")
	}
	require.Equal(t, external, mustReadFile(t, path))
	require.NoFileExists(t, lockPath)
	require.False(t, indexlock.CheckPriority(lockPath))
}
