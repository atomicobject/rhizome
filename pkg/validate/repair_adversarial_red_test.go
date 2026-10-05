package validate

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareRepairTransactionPublishesManifestBeforeArtifacts(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	after := []byte("after\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), before, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:prepare-order", issueKey: "issue:prepare-order",
		operations: []RepairOperation{repairWriteOperation(
			"operation:prepare-order", "action:prepare-order", "note.md", before, after,
		)},
	}})
	runCtx := repairRunContext(t, root)
	transaction := plan.Transactions[0]

	observed := false
	prepared, err := prepareRepairTransaction(runCtx, plan.Fingerprint, transaction, plan.Operations, false,
		&repairExecutionHooks{AfterJournalPublished: func(dir string) error {
			observed = true
			manifest, readErr := readRepairJournalManifest(dir)
			require.NoError(t, readErr, "crash recovery metadata must already be durable")
			for _, entry := range manifest.Entries {
				for _, artifact := range []string{entry.BackupPath, entry.StagePath} {
					if artifact != "" {
						assert.NoFileExists(t, artifact, "manifest must precede every repair artifact")
					}
				}
			}
			return errSimulatedRepairInterruption
		}})
	require.ErrorIs(t, err, errSimulatedRepairInterruption)
	assert.Nil(t, prepared)
	assert.True(t, observed)
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	require.Len(t, evidence, 1)
	assert.Equal(t, repairJournalPrepared, evidence[0].State)

	_, err = ApplyFixPlan(context.Background(), runCtx, &plan, Options{Fix: true, NonInteractive: true})
	require.ErrorContains(t, err, "post-apply refresh")
	evidence, detectErr = DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	require.Len(t, evidence, 1, "even no-mutation recovery requires the configured refresher")

	_, err = ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
	})
	require.ErrorContains(t, err, "replan")
	assert.Equal(t, before, mustReadFile(t, filepath.Join(root, "note.md")))
	evidence, detectErr = DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	assert.Empty(t, evidence, "refreshed recovery may clear manifest-only evidence")

	execution, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{Fix: true, NonInteractive: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"action:prepare-order"}, execution.Applied)
	assert.Equal(t, after, mustReadFile(t, filepath.Join(root, "note.md")))
}

func TestPreparedRecoveryRefusesPostCrashUserEdit(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	after := []byte("after\n")
	userEdit := []byte("user edit after crash\n")
	path := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(path, before, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:user-edit", issueKey: "issue:user-edit",
		operations: []RepairOperation{repairWriteOperation(
			"operation:user-edit", "action:user-edit", "note.md", before, after,
		)},
	}})
	runCtx := repairRunContext(t, root)
	prepared, err := prepareRepairTransaction(runCtx, plan.Fingerprint, plan.Transactions[0], plan.Operations, false, nil)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, userEdit, 0o640))

	_, err = ApplyFixPlan(context.Background(), runCtx, &plan, Options{Fix: true, NonInteractive: true})
	require.Error(t, err)
	assert.ErrorContains(t, err, "changed")
	assert.Equal(t, userEdit, mustReadFile(t, path), "recovery must not overwrite a post-crash user edit")
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	require.Len(t, evidence, 1, "ambiguous recovery evidence must remain for operator action")
	assert.Equal(t, prepared.manifest.TransactionID, evidence[0].TransactionID)
}

func TestPreparedRecoveryRefusesCorruptBackup(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	after := []byte("after\n")
	path := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(path, before, 0o751))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:corrupt-backup", issueKey: "issue:corrupt-backup",
		operations: []RepairOperation{repairWriteOperation(
			"operation:corrupt-backup", "action:corrupt-backup", "note.md", before, after,
		)},
	}})
	runCtx := repairRunContext(t, root)
	prepared, err := prepareRepairTransaction(runCtx, plan.Fingerprint, plan.Transactions[0], plan.Operations, false, nil)
	require.NoError(t, err)
	require.NotEmpty(t, prepared.manifest.Entries[0].BackupPath)
	require.NoError(t, os.WriteFile(prepared.manifest.Entries[0].BackupPath, []byte("corrupt\n"), 0o751))

	_, err = ApplyFixPlan(context.Background(), runCtx, &plan, Options{Fix: true, NonInteractive: true})
	require.Error(t, err)
	assert.ErrorContains(t, err, "backup")
	assert.Equal(t, before, mustReadFile(t, path), "corrupt recovery material must never replace the source")
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	require.Len(t, evidence, 1, "corrupt recovery evidence must remain for operator action")
	assert.Equal(t, prepared.manifest.TransactionID, evidence[0].TransactionID)
}

func TestApplyFixPlanRejectsSymlinkParentEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	before := []byte("outside before\n")
	after := []byte("outside after\n")
	outsidePath := filepath.Join(outside, "note.md")
	require.NoError(t, os.WriteFile(outsidePath, before, 0o640))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "linked")))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:escape", issueKey: "issue:escape",
		operations: []RepairOperation{repairWriteOperation(
			"operation:escape", "action:escape", "linked/note.md", before, after,
		)},
	}})

	_, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
		Fix: true, NonInteractive: true,
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, "outside")
	assert.Equal(t, before, mustReadFile(t, outsidePath))
}

func TestApplyFixPlanCaseOnlyRenameRecoversAndRetriesIdempotently(t *testing.T) {
	root := t.TempDir()
	before := []byte("case-only rename\n")
	source := filepath.Join(root, "Name.md")
	destination := filepath.Join(root, "name.md")
	require.NoError(t, os.WriteFile(source, before, 0o751))
	wantMode := mustRepairMode(t, source)
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:case-only", issueKey: "issue:case-only",
		operations: []RepairOperation{{
			ID: "operation:case-only", Kind: RepairOperationRename,
			Path: "Name.md", DestinationPath: "name.md", SourceHash: SourceHash(before),
		}},
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

	_, err = ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
	})
	require.ErrorContains(t, err, "replan")
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	var recoveredNames []string
	for _, entry := range entries {
		if entry.Name() == "Name.md" || entry.Name() == "name.md" {
			recoveredNames = append(recoveredNames, entry.Name())
		}
	}
	assert.Equal(t, []string{"Name.md"}, recoveredNames, "recovery restores the original directory-entry casing")

	execution, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{Fix: true, NonInteractive: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"action:case-only"}, execution.Applied)
	entries, err = os.ReadDir(root)
	require.NoError(t, err)
	var exactNames []string
	for _, entry := range entries {
		if entry.Name() == "Name.md" || entry.Name() == "name.md" {
			exactNames = append(exactNames, entry.Name())
		}
	}
	assert.Equal(t, []string{"name.md"}, exactNames, "directory entry must use the requested destination case")
	assert.Equal(t, before, mustReadFile(t, destination))
	assert.Equal(t, wantMode, mustRepairMode(t, destination))
	evidence, err := DetectPendingRepairJournals(runCtx)
	require.NoError(t, err)
	assert.Empty(t, evidence)
}

func TestRepairAbsPathPreservesRequestedCaseForExistingSameFile(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "Name.md")
	requested := filepath.Join(root, "name.md")
	require.NoError(t, os.WriteFile(source, []byte("same file\n"), 0o600))
	sourceInfo, err := os.Lstat(source)
	require.NoError(t, err)
	requestedInfo, err := os.Lstat(requested)
	if os.IsNotExist(err) {
		t.Skip("filesystem is case-sensitive")
	}
	require.NoError(t, err)
	if !os.SameFile(sourceInfo, requestedInfo) {
		t.Skip("case-folded destination is not the same file")
	}
	vaultPaths, err := paths.NewVaultPaths(root)
	require.NoError(t, err)
	clean, err := paths.CleanRelPath("name.md")
	require.NoError(t, err)
	canonicalized, err := vaultPaths.Abs(clean)
	require.NoError(t, err)
	if runtime.GOOS == "windows" {
		assert.Equal(t, filepath.Base(source), filepath.Base(canonicalized.String()), "typed absolute resolution canonicalizes an existing leaf")
		canonicalizedInfo, statErr := os.Lstat(canonicalized.String())
		require.NoError(t, statErr)
		assert.True(t, os.SameFile(sourceInfo, canonicalizedInfo), "canonical parent spellings must retain file identity")
	}

	resolved, err := repairAbsPath(repairRunContext(t, root), "name.md")
	require.NoError(t, err)
	resolvedRoot, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	assert.Equal(t, "name.md", filepath.Base(resolved))
	resolvedParentInfo, err := os.Lstat(filepath.Dir(resolved))
	require.NoError(t, err)
	rootInfo, err := os.Lstat(resolvedRoot)
	require.NoError(t, err)
	assert.True(t, os.SameFile(rootInfo, resolvedParentInfo), "equivalent parent spellings must retain vault containment")
	resolvedInfo, err := os.Lstat(resolved)
	require.NoError(t, err)
	assert.True(t, os.SameFile(sourceInfo, resolvedInfo), "preconditions must still inspect the existing source")
}

func TestApplyFixPlanRunsPostApplyCheckWithHeldLeaseBeforeJournalCleanup(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	after := []byte("after\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), before, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:postcheck", issueKey: "issue:postcheck",
		operations: []RepairOperation{repairWriteOperation(
			"operation:postcheck", "action:postcheck", "note.md", before, after,
		)},
	}})
	runCtx := repairRunContext(t, root)
	var borrowed *IndexLockLease
	called := false

	_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true,
		postApplyCheck: func(_ context.Context, lease *IndexLockLease, _ *ontology.Runtime, _ repairPostApplyScope) error {
			called = true
			borrowed = lease
			require.NoError(t, lease.RequireHeld(), "postcheck must borrow the engine's held lease")
			evidence, detectErr := DetectPendingRepairJournals(runCtx)
			require.NoError(t, detectErr)
			require.Len(t, evidence, 1, "committed journal must remain until postcheck converges")
			assert.Equal(t, repairJournalCommitted, evidence[0].State)
			assert.Equal(t, plan.Transactions[0].ID, evidence[0].TransactionID)
			return nil
		},
	})
	require.NoError(t, err)
	assert.True(t, called)
	require.NotNil(t, borrowed)
	assert.Error(t, borrowed.RequireHeld(), "engine must invalidate the borrowed lease after apply returns")
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	assert.Empty(t, evidence, "journal cleanup must follow successful postcheck")
}

type preparedRuntimeRefresher struct {
	runtime    *ontology.Runtime
	closeCalls int
}

func (r *preparedRuntimeRefresher) Close() error {
	r.closeCalls++
	return nil
}

func (r *preparedRuntimeRefresher) Refresh(
	_ context.Context,
	lease *IndexLockLease,
	_ []string,
	_ []PathRename,
	_ []string,
) (PostApplyRefreshResult, error) {
	if err := lease.RequireHeld(); err != nil {
		return PostApplyRefreshResult{}, err
	}
	result := NewPostApplyRefreshResult(r.runtime, r)
	return result, nil
}

func TestApplyFixPlanHandsPreparedRuntimeToPostCheckThenClosesOnce(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), before, 0o640))
	plan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:runtime", issueKey: "issue:runtime",
		operations: []RepairOperation{repairWriteOperation(
			"operation:runtime", "action:runtime", "note.md", before, []byte("after\n"),
		)},
	}})
	preparedRuntime := &ontology.Runtime{}
	refresher := &preparedRuntimeRefresher{runtime: preparedRuntime}
	seen := false

	_, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: refresher,
		postApplyCheck: func(_ context.Context, lease *IndexLockLease, runtime *ontology.Runtime, _ repairPostApplyScope) error {
			require.NoError(t, lease.RequireHeldForVault(root))
			assert.Same(t, preparedRuntime, runtime)
			assert.Equal(t, 0, refresher.closeCalls, "runtime must remain open through validation")
			seen = true
			return nil
		},
	})
	require.NoError(t, err)
	assert.True(t, seen)
	assert.Equal(t, 1, refresher.closeCalls)
}

func TestApplyFixPlanDerivesClosedEffortLifecycleFromRawBytes(t *testing.T) {
	closed := []byte("---\ntype: EffortNote\nstatus: complete\n---\n\n# Closed\n\nProtected\n")
	changed := []byte("---\ntype: EffortNote\nstatus: complete\n---\n\n# Closed\n\nChanged\n")
	tests := []struct {
		name       string
		operation  func() RepairOperation
		assertSafe func(*testing.T, string)
		assertDone func(*testing.T, string)
	}{
		{
			name: "write",
			operation: func() RepairOperation {
				op := repairWriteOperation("operation:closed-write", "action:closed-write", "closed.md", closed, changed)
				op.Lifecycle = LifecyclePolicyResult{Decision: LifecycleAllowed, Reason: "forged caller decision"}
				return op
			},
			assertSafe: func(t *testing.T, root string) {
				assert.Equal(t, closed, mustReadFile(t, filepath.Join(root, "closed.md")))
			},
			assertDone: func(t *testing.T, root string) {
				assert.Equal(t, changed, mustReadFile(t, filepath.Join(root, "closed.md")))
			},
		},
		{
			name: "rename",
			operation: func() RepairOperation {
				return RepairOperation{
					ID: "operation:closed-rename", Kind: RepairOperationRename,
					Path: "closed.md", DestinationPath: "moved.md", SourceHash: SourceHash(closed),
					Lifecycle: LifecyclePolicyResult{Decision: LifecycleAllowed, Reason: "forged caller decision"},
				}
			},
			assertSafe: func(t *testing.T, root string) {
				assert.Equal(t, closed, mustReadFile(t, filepath.Join(root, "closed.md")))
				assert.NoFileExists(t, filepath.Join(root, "moved.md"))
			},
			assertDone: func(t *testing.T, root string) {
				assert.NoFileExists(t, filepath.Join(root, "closed.md"))
				assert.Equal(t, closed, mustReadFile(t, filepath.Join(root, "moved.md")))
			},
		},
		{
			name: "delete",
			operation: func() RepairOperation {
				return RepairOperation{
					ID: "operation:closed-delete", Kind: RepairOperationDelete,
					Path: "closed.md", SourceHash: SourceHash(closed),
					Lifecycle: LifecyclePolicyResult{Decision: LifecycleAllowed, Reason: "forged caller decision"},
				}
			},
			assertSafe: func(t *testing.T, root string) {
				assert.Equal(t, closed, mustReadFile(t, filepath.Join(root, "closed.md")))
			},
			assertDone: func(t *testing.T, root string) {
				assert.NoFileExists(t, filepath.Join(root, "closed.md"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "closed.md"), closed, 0o640))
			op := tt.operation()
			actionID := "action:closed-" + tt.name
			issueKey := "issue:closed-" + tt.name
			plan := mustRepairPlan(t, []repairPlanInput{{
				actionID: actionID, issueKey: issueKey, operations: []RepairOperation{op},
			}})
			runCtx := repairRunContext(t, root)

			execution, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{Fix: true, NonInteractive: true})
			require.NoError(t, err)
			require.Len(t, execution.Transactions, 1)
			assert.Equal(t, "skipped_lifecycle", execution.Transactions[0].Status)
			tt.assertSafe(t, root)

			execution, err = ApplyFixPlan(context.Background(), runCtx, &plan, Options{
				Fix: true, NonInteractive: true, AllowHistorical: true,
			})
			require.NoError(t, err)
			assert.Equal(t, []string{plan.Actions[0].ID}, execution.Applied)
			tt.assertDone(t, root)
		})
	}
}
