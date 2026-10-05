package validate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestApplyFixPlanReplansUnderHeldLeaseBeforeMutation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(path, []byte("before\n"), 0o644))
	runCtx := repairRunContext(t, root)
	action := FixAction{
		ID: "action:identifier", Check: CheckAliases, Kind: "identifier_reconciliation",
		Safety: FixSafetySafe, IssueKeys: []string{"issue:identifier"},
	}
	preliminary := finalizedSingleWritePlan(t, action, "note.md", "before\n", "replanned\n")
	replanned := preliminary
	called := 0

	execution, err := ApplyFixPlan(context.Background(), runCtx, &preliminary, Options{
		Fix: true, NonInteractive: true,
		leaseHeldRepairPlanner: func(_ context.Context, lease *IndexLockLease) (*FixPlan, error) {
			called++
			require.NoError(t, lease.RequireHeldForVault(root))
			current, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			require.Equal(t, "before\n", string(current), "mapping must run before mutation")
			return &replanned, nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, 1, called)
	require.Equal(t, replanned.Fingerprint, execution.PlanFingerprint)
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "replanned\n", string(content))
}

func TestApplyFixPlanRejectsLeaseReplanAuthorityDrift(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("before\n"), 0o644))
	runCtx := repairRunContext(t, root)
	action := FixAction{ID: "action:identifier", Check: CheckAliases, Kind: "identifier_reconciliation", Safety: FixSafetySafe, IssueKeys: []string{"issue:identifier"}}
	preliminary := finalizedSingleWritePlan(t, action, "note.md", "before\n", "preview\n")
	drifted := action
	drifted.IssueKeys = []string{"issue:other"}
	replanned := finalizedSingleWritePlan(t, drifted, "note.md", "before\n", "replanned\n")

	execution, err := ApplyFixPlan(context.Background(), runCtx, &preliminary, Options{
		Fix: true, NonInteractive: true,
		leaseHeldRepairPlanner: func(context.Context, *IndexLockLease) (*FixPlan, error) {
			return &replanned, nil
		},
	})
	require.ErrorContains(t, err, "differs from reviewed plan")
	require.Equal(t, []string{"action:identifier"}, execution.Failed)
	require.Equal(t, []string{"issue:identifier"}, execution.RemainingIssueKeys)
	require.Len(t, execution.Transactions, 1)
	require.Equal(t, "not_attempted", execution.Transactions[0].Status)
	content, readErr := os.ReadFile(filepath.Join(root, "note.md"))
	require.NoError(t, readErr)
	require.Equal(t, "before\n", string(content))
}

func TestApplyFixPlanClassifiesReviewedTransactionsWhenHeldPlannerFails(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "safe.md"), []byte("safe before\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "confirm.md"), []byte("confirm before\n"), 0o644))
	safe := FixAction{
		ID: "action:safe-planner-failure", Check: CheckAliases, Kind: "identifier_reconciliation",
		Safety: FixSafetySafe, IssueKeys: []string{"issue:safe-planner-failure"},
	}
	confirm := FixAction{
		ID: "action:confirm-planner-failure", Check: CheckAliases, Kind: "identifier_reconciliation",
		Safety: FixSafetyConfirm, IssueKeys: []string{"issue:confirm-planner-failure"},
	}
	plan, err := FinalizeRepairPlan(RepairPlan{
		Actions: []FixAction{safe, confirm},
		Operations: []RepairOperation{
			wholeFileRepairOperation("operation:safe-planner-failure", safe, "safe.md", []byte("safe before\n"), []byte("safe after\n")),
			wholeFileRepairOperation("operation:confirm-planner-failure", confirm, "confirm.md", []byte("confirm before\n"), []byte("confirm after\n")),
		},
	})
	require.NoError(t, err)
	plannerErr := errors.New("authoritative mapper failed")
	execution, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
		Fix: true, NonInteractive: true,
		leaseHeldRepairPlanner: func(context.Context, *IndexLockLease) (*FixPlan, error) {
			return nil, plannerErr
		},
	})
	require.ErrorIs(t, err, plannerErr)
	require.Equal(t, []string{safe.ID}, execution.Failed)
	require.Equal(t, []string{confirm.ID}, execution.Skipped)
	require.ElementsMatch(t, []string{safe.IssueKeys[0], confirm.IssueKeys[0]}, execution.RemainingIssueKeys)
	require.Len(t, execution.Transactions, 2)
	statuses := map[string]string{}
	for _, transaction := range execution.Transactions {
		statuses[transaction.AffectedPaths[0]] = transaction.Status
	}
	require.Equal(t, "not_attempted", statuses["safe.md"])
	require.Equal(t, "skipped", statuses["confirm.md"])
	require.NotEmpty(t, execution.ReplanCommand)
	require.Equal(t, "safe before\n", string(mustReadFile(t, filepath.Join(root, "safe.md"))))
	require.Equal(t, "confirm before\n", string(mustReadFile(t, filepath.Join(root, "confirm.md"))))
}

func TestApplyFixPlanReportsUnbackedActionBeforeHeldPlannerFailure(t *testing.T) {
	root := t.TempDir()
	action := FixAction{
		ID: "action:unbacked-planner-failure", Check: CheckAliases, Kind: "identifier_reconciliation",
		Safety: FixSafetySafe, IssueKeys: []string{"issue:unbacked-planner-failure"},
	}
	plan, err := FinalizeRepairPlan(RepairPlan{Actions: []FixAction{action}, IssueKeys: action.IssueKeys})
	require.NoError(t, err)
	plannerErr := errors.New("held planner failed before mapping")
	execution, err := ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{
		Fix: true, NonInteractive: true,
		leaseHeldRepairPlanner: func(context.Context, *IndexLockLease) (*FixPlan, error) {
			return nil, plannerErr
		},
	})
	require.ErrorIs(t, err, plannerErr)
	require.Equal(t, []string{action.ID}, execution.Skipped)
	require.Equal(t, action.IssueKeys, execution.RemainingIssueKeys)
	require.NotEmpty(t, execution.ReplanCommand)
}

func TestApplyFixPlanRejectsIdentifierRepairWithoutLeaseHeldRevalidation(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("before\n"), 0o644))
	action := FixAction{
		ID: "action:identifier-bypass", Check: CheckAliases, Kind: "resolve_duplicate_identifier",
		Safety: FixSafetySafe, IssueKeys: []string{"issue:identifier-bypass"},
	}
	plan := finalizedSingleWritePlan(t, action, "note.md", "before\n", "after\n")
	plan.RequiresLeaseHeldReplan = true
	plan, err := FinalizeRepairPlan(plan)
	require.NoError(t, err)

	_, err = ApplyFixPlan(context.Background(), repairRunContext(t, root), &plan, Options{Fix: true, NonInteractive: true})
	require.ErrorContains(t, err, "requires lease-held authoritative revalidation")
	content, readErr := os.ReadFile(filepath.Join(root, "note.md"))
	require.NoError(t, readErr)
	require.Equal(t, "before\n", string(content))
}

func TestApplyFixPlanRequiresExactHeldRemapForIdentifierPlan(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("before\n"), 0o644))
	action := FixAction{
		ID: "action:identifier-exact", Check: CheckAliases, Kind: "resolve_duplicate_identifier",
		Safety: FixSafetySafe, IssueKeys: []string{"issue:identifier-exact"},
	}
	reviewed := finalizedSingleWritePlan(t, action, "note.md", "before\n", "reviewed\n")
	reviewed.RequiresLeaseHeldReplan = true
	reviewed, err := FinalizeRepairPlan(reviewed)
	require.NoError(t, err)
	changed := finalizedSingleWritePlan(t, action, "note.md", "before\n", "changed\n")
	changed.RequiresLeaseHeldReplan = true
	changed, err = FinalizeRepairPlan(changed)
	require.NoError(t, err)

	_, err = ApplyFixPlan(context.Background(), repairRunContext(t, root), &reviewed, Options{
		Fix: true, NonInteractive: true,
		leaseHeldRepairPlanner: func(context.Context, *IndexLockLease) (*FixPlan, error) { return &changed, nil },
	})
	require.ErrorContains(t, err, "differs from reviewed plan")
	content, readErr := os.ReadFile(filepath.Join(root, "note.md"))
	require.NoError(t, readErr)
	require.Equal(t, "before\n", string(content))
}

func TestApplyFixPlanRejectsLeaseReplanOperationMembershipSwap(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "safe.md"), []byte("safe before\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "confirm.md"), []byte("confirm before\n"), 0o644))
	runCtx := repairRunContext(t, root)
	safeAction := FixAction{
		ID: "action:safe", Check: CheckAliases, Kind: "identifier_reconciliation",
		Safety: FixSafetySafe, IssueKeys: []string{"issue:safe"},
	}
	confirmAction := FixAction{
		ID: "action:confirm", Check: CheckAliases, Kind: "identifier_reconciliation",
		Safety: FixSafetyConfirm, IssueKeys: []string{"issue:confirm"},
	}
	preliminary, err := FinalizeRepairPlan(RepairPlan{
		Actions: []FixAction{safeAction, confirmAction},
		Operations: []RepairOperation{
			wholeFileRepairOperation("operation:safe-effect", safeAction, "safe.md", []byte("safe before\n"), []byte("safe after\n")),
			wholeFileRepairOperation("operation:confirm-effect", confirmAction, "confirm.md", []byte("confirm before\n"), []byte("confirm after\n")),
		},
	})
	require.NoError(t, err)
	replanned := RepairPlan{
		Actions: []FixAction{safeAction, confirmAction},
		Operations: []RepairOperation{
			wholeFileRepairOperation("operation:safe-effect", confirmAction, "safe.md", []byte("safe before\n"), []byte("safe after\n")),
			wholeFileRepairOperation("operation:confirm-effect", safeAction, "confirm.md", []byte("confirm before\n"), []byte("confirm after\n")),
		},
	}

	_, err = ApplyFixPlan(context.Background(), runCtx, &preliminary, Options{
		Fix: true, NonInteractive: true,
		leaseHeldRepairPlanner: func(context.Context, *IndexLockLease) (*FixPlan, error) {
			return &replanned, nil
		},
	})
	require.ErrorContains(t, err, "differs from reviewed plan")
	require.Equal(t, "safe before\n", string(mustReadFile(t, filepath.Join(root, "safe.md"))))
	require.Equal(t, "confirm before\n", string(mustReadFile(t, filepath.Join(root, "confirm.md"))),
		"non-interactive safe selection must never apply a confirm effect after membership drift")
}

func TestApplyFixPlanRejectsMutatedLeaseReplanWithNonemptyFingerprint(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(path, []byte("before\n"), 0o644))
	runCtx := repairRunContext(t, root)
	action := FixAction{
		ID: "action:identifier", Check: CheckAliases, Kind: "identifier_reconciliation",
		Safety: FixSafetySafe, IssueKeys: []string{"issue:identifier"},
	}
	preliminary := finalizedSingleWritePlan(t, action, "note.md", "before\n", "preview\n")
	replanned := finalizedSingleWritePlan(t, action, "note.md", "before\n", "replanned\n")
	require.NotEmpty(t, replanned.Fingerprint)
	replanned.Operations[0].Content = []byte("tampered\n")
	replanned.Operations[0].Expected[0].Replacement = "tampered\n"

	_, err := ApplyFixPlan(context.Background(), runCtx, &preliminary, Options{
		Fix: true, NonInteractive: true,
		leaseHeldRepairPlanner: func(context.Context, *IndexLockLease) (*FixPlan, error) {
			return &replanned, nil
		},
	})
	require.ErrorContains(t, err, "fingerprint changed")
	require.Equal(t, "before\n", string(mustReadFile(t, path)))
}

func TestApplyFixPlanFinishesCleanupRecoveryAsBarrierBeforeHeldPlanner(t *testing.T) {
	root := t.TempDir()
	runCtx := repairRunContext(t, root)
	cleanupPlan, _, _ := leaveCommittedRepairJournal(t, runCtx)
	cleanupInterrupted := false
	_, err := ApplyFixPlan(context.Background(), runCtx, &cleanupPlan, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
		repairHooks: &repairExecutionHooks{AfterCleanupBoundary: func(boundary, _ string) error {
			if boundary == cleanupAfterDetach && !cleanupInterrupted {
				cleanupInterrupted = true
				return errSimulatedRepairInterruption
			}
			return nil
		}},
	})
	require.ErrorIs(t, err, errSimulatedRepairInterruption)
	pending, err := DetectPendingRepairJournals(runCtx)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, repairJournalCleanupPending, pending[0].State)

	path := filepath.Join(root, "new.md")
	require.NoError(t, os.WriteFile(path, []byte("before\n"), 0o644))
	action := FixAction{
		ID: "action:new-cleanup-failure", Check: CheckAliases, Kind: "resolve_duplicate_identifier",
		Safety: FixSafetySafe, IssueKeys: []string{"issue:new-cleanup-failure"},
	}
	plan := finalizedSingleWritePlan(t, action, "new.md", "before\n", "after\n")
	postcheckCalls := 0
	plannerCalls := 0
	probe := &scopedRepairPostApplyProbe{repairPostApplyProbe: repairPostApplyProbe{t: t}}
	execution, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: probe,
		postApplyCheck: func(_ context.Context, lease *IndexLockLease, _ *ontology.Runtime, scope repairPostApplyScope) error {
			postcheckCalls++
			require.NoError(t, lease.RequireHeldForVault(root))
			require.Equal(t, []string{CheckOntology}, scope.Checks)
			require.Equal(t, []string{"note.md"}, scope.AffectedPaths)
			return nil
		},
		leaseHeldRepairPlanner: func(context.Context, *IndexLockLease) (*FixPlan, error) {
			plannerCalls++
			return &plan, nil
		},
	})
	require.ErrorContains(t, err, "replan")
	require.Equal(t, []string{CheckOntology}, probe.scope.Checks)
	require.Empty(t, probe.scope.Changed)
	require.Empty(t, probe.scope.Renamed)
	require.Empty(t, probe.scope.Deleted)
	require.Empty(t, probe.scope.ChangedByCheck)
	require.Zero(t, plannerCalls)
	require.Equal(t, 1, postcheckCalls)
	require.Equal(t, "before\n", string(mustReadFile(t, path)), "held mapper failure must precede new mutation")
	require.Equal(t, plan.Fingerprint, execution.PlanFingerprint, "the replan result must identify the reviewed plan")
	require.Len(t, execution.Transactions, 2)
	require.Equal(t, "recovered_cleanup", execution.Transactions[0].Status)
	require.Equal(t, "not_attempted", execution.Transactions[1].Status)
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	require.Empty(t, evidence, "completed terminal cleanup must not loop after a later mapper failure")
}

func TestApplyFixPlanCleanupRecoveryBarrierFailsClosedWithoutRefreshOrPostcheck(t *testing.T) {
	tests := []struct {
		name string
		opts func(*testing.T) Options
	}{
		{
			name: "missing refresher",
			opts: func(*testing.T) Options {
				return Options{
					Fix: true, NonInteractive: true,
					postApplyCheck: func(context.Context, *IndexLockLease, *ontology.Runtime, repairPostApplyScope) error { return nil },
				}
			},
		},
		{
			name: "missing postcheck",
			opts: func(t *testing.T) Options {
				return Options{Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t}}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			runCtx := repairRunContext(t, root)
			cleanupPlan, _, _ := leaveCommittedRepairJournal(t, runCtx)
			_, err := ApplyFixPlan(context.Background(), runCtx, &cleanupPlan, Options{
				Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
				repairHooks: &repairExecutionHooks{AfterCleanupBoundary: func(boundary, _ string) error {
					if boundary == cleanupAfterDetach {
						return errSimulatedRepairInterruption
					}
					return nil
				}},
			})
			require.ErrorIs(t, err, errSimulatedRepairInterruption)
			path := filepath.Join(root, "new.md")
			require.NoError(t, os.WriteFile(path, []byte("before\n"), 0o644))
			action := FixAction{
				ID: "action:cleanup-support", Check: CheckAliases, Kind: "resolve_duplicate_identifier",
				Safety: FixSafetySafe, IssueKeys: []string{"issue:cleanup-support"},
			}
			plan := finalizedSingleWritePlan(t, action, "new.md", "before\n", "after\n")
			execution, err := ApplyFixPlan(context.Background(), runCtx, &plan, tt.opts(t))
			require.ErrorContains(t, err, "requires post-apply refresh and held validation")
			require.Equal(t, "before\n", string(mustReadFile(t, path)))
			require.Equal(t, []string{action.ID}, execution.Failed)
			require.Len(t, execution.Transactions, 2)
			require.Equal(t, "recovered_cleanup", execution.Transactions[0].Status)
			require.Equal(t, "not_attempted", execution.Transactions[1].Status)
		})
	}
}

func TestApplyFixPlanSuppressesLeaseReplanAfterStateChangingRecovery(t *testing.T) {
	root := t.TempDir()
	runCtx := repairRunContext(t, root)
	recoveryPath := filepath.Join(root, "recovery.md")
	newPath := filepath.Join(root, "new.md")
	require.NoError(t, os.WriteFile(recoveryPath, []byte("recovery before\n"), 0o644))
	require.NoError(t, os.WriteFile(newPath, []byte("new before\n"), 0o644))
	recoveryAction := FixAction{
		ID: "action:recovery", Check: CheckAliases, Kind: "identifier_reconciliation",
		Safety: FixSafetySafe, IssueKeys: []string{"issue:recovery"},
	}
	recoveryPlan := finalizedSingleWritePlan(t, recoveryAction, "recovery.md", "recovery before\n", "recovery after\n")
	recoveryPlan.Operations[0].RequiredChecks = []string{CheckOntology}
	recoveryPlan, err := FinalizeRepairPlan(recoveryPlan)
	require.NoError(t, err)
	_, err = ApplyFixPlan(context.Background(), runCtx, &recoveryPlan, Options{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{AfterInstall: func(_ int, _ string) error {
			return errSimulatedRepairInterruption
		}},
	})
	require.ErrorIs(t, err, errSimulatedRepairInterruption)

	newAction := FixAction{
		ID: "action:new", Check: CheckAliases, Kind: "identifier_reconciliation",
		Safety: FixSafetySafe, IssueKeys: []string{"issue:new"},
	}
	newPlan := finalizedSingleWritePlan(t, newAction, "new.md", "new before\n", "new after\n")
	newPlan.RequiresLeaseHeldReplan = true
	newPlan, err = FinalizeRepairPlan(newPlan)
	require.NoError(t, err)
	plannerCalls := 0
	probe := &repairPostApplyProbe{t: t}
	postcheckCalls := 0
	execution, err := ApplyFixPlan(context.Background(), runCtx, &newPlan, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: probe,
		leaseHeldRepairPlanner: func(context.Context, *IndexLockLease) (*FixPlan, error) {
			plannerCalls++
			return &newPlan, nil
		},
		postApplyCheck: func(_ context.Context, lease *IndexLockLease, _ *ontology.Runtime, scope repairPostApplyScope) error {
			postcheckCalls++
			require.NoError(t, lease.RequireHeldForVault(root))
			require.Equal(t, []string{CheckAliases, CheckOntology}, scope.Checks)
			require.Equal(t, []string{"recovery.md"}, scope.AffectedPaths)
			return nil
		},
	})
	require.ErrorContains(t, err, "replan")
	require.Zero(t, plannerCalls, "rollback recovery must block fresh planning and mutation in the same apply")
	require.Equal(t, 1, postcheckCalls, "recovered manifest checks must run before journal cleanup")
	require.Equal(t, []string{"recovery.md"}, probe.changed, "rollback delta must refresh before returning replan guidance")
	require.Equal(t, "recovery before\n", string(mustReadFile(t, recoveryPath)))
	require.Equal(t, "new before\n", string(mustReadFile(t, newPath)))
	require.Equal(t, []string{"action:new", "action:recovery"}, execution.Failed)
	require.Equal(t, []string{"issue:new", "issue:recovery"}, execution.RemainingIssueKeys)
	require.Empty(t, execution.Applied)
	require.Empty(t, execution.Skipped)
	require.Equal(t, "recovered_rollback", execution.Transactions[0].Status)
	require.Equal(t, "not_attempted", execution.Transactions[1].Status)
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	require.Empty(t, evidence, "successful rollback refresh and postcheck make PREPARED cleanup eligible")
}

func TestApplyFixPlanCompletesPartialRecoveryPostcheckBeforeReturningCausalReplanError(t *testing.T) {
	root := t.TempDir()
	runCtx := repairRunContext(t, root)
	beforeA, beforeB := []byte("a before\n"), []byte("b before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.md"), beforeA, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "b.md"), beforeB, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "new.md"), []byte("new before\n"), 0o644))
	recoveryPlan := mustRepairPlan(t, []repairPlanInput{{
		actionID: "action:recovery-partial", issueKey: "issue:recovery-partial",
		operations: []RepairOperation{
			repairWriteOperation("operation:recovery-a", "action:recovery-partial", "a.md", beforeA, []byte("a after\n")),
			repairWriteOperation("operation:recovery-b", "action:recovery-partial", "b.md", beforeB, []byte("b after\n")),
		},
	}})
	for index := range recoveryPlan.Operations {
		recoveryPlan.Operations[index].RequiredChecks = []string{CheckBrokenLinks}
	}
	recoveryPlan, err := FinalizeRepairPlan(recoveryPlan)
	require.NoError(t, err)
	_, err = ApplyFixPlan(context.Background(), runCtx, &recoveryPlan, Options{
		Fix: true, NonInteractive: true,
		repairHooks: &repairExecutionHooks{AfterInstall: func(index int, _ string) error {
			if index == 1 {
				return errSimulatedRepairInterruption
			}
			return nil
		}},
	})
	require.ErrorIs(t, err, errSimulatedRepairInterruption)

	newAction := FixAction{
		ID: "action:new-after-partial", Check: CheckAliases, Kind: "resolve_duplicate_identifier",
		Safety: FixSafetySafe, IssueKeys: []string{"issue:new-after-partial"},
	}
	newPlan := finalizedSingleWritePlan(t, newAction, "new.md", "new before\n", "new after\n")
	newPlan.RequiresLeaseHeldReplan = true
	newPlan, err = FinalizeRepairPlan(newPlan)
	require.NoError(t, err)
	rollbackCalls := 0
	plannerCalls := 0
	postcheckCalls := 0
	probe := &repairPostApplyProbe{t: t}
	execution, err := ApplyFixPlan(context.Background(), runCtx, &newPlan, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: probe,
		leaseHeldRepairPlanner: func(context.Context, *IndexLockLease) (*FixPlan, error) {
			plannerCalls++
			return &newPlan, nil
		},
		postApplyCheck: func(_ context.Context, lease *IndexLockLease, _ *ontology.Runtime, scope repairPostApplyScope) error {
			postcheckCalls++
			require.NoError(t, lease.RequireHeldForVault(root))
			require.Equal(t, []string{CheckBrokenLinks, CheckOntology}, scope.Checks)
			require.Equal(t, []string{"a.md", "b.md"}, scope.AffectedPaths)
			return nil
		},
		repairHooks: &repairExecutionHooks{BeforeRollbackMutation: func(_ int, _ string) error {
			rollbackCalls++
			if rollbackCalls == 2 {
				return errSimulatedRepairInterruption
			}
			return nil
		}},
	})
	require.ErrorIs(t, err, errSimulatedRepairInterruption, "replan guidance must retain the recovery interruption cause")
	require.ErrorContains(t, err, "replan")
	require.Zero(t, plannerCalls)
	require.Equal(t, 1, postcheckCalls)
	require.Equal(t, []string{"a.md", "b.md"}, probe.changed)
	require.Equal(t, "new before\n", string(mustReadFile(t, filepath.Join(root, "new.md"))))
	require.Equal(t, []string{"action:new-after-partial", "action:recovery-partial"}, execution.Failed)
	require.Equal(t, []string{"issue:new-after-partial", "issue:recovery-partial"}, execution.RemainingIssueKeys)
	require.Empty(t, execution.Applied)
	require.Empty(t, execution.Skipped)
	require.Equal(t, "recovery_partial", execution.Transactions[0].Status)
	require.Equal(t, "not_attempted", execution.Transactions[1].Status)
	evidence, detectErr := DetectPendingRepairJournals(runCtx)
	require.NoError(t, detectErr)
	require.Len(t, evidence, 1, "partial rollback evidence must remain after refresh and postcheck")
	require.Equal(t, repairJournalPrepared, evidence[0].State)
}

func finalizedSingleWritePlan(t *testing.T, action FixAction, notePath, before, after string) RepairPlan {
	t.Helper()
	operation := wholeFileRepairOperation("operation:"+action.ID, action, notePath, []byte(before), []byte(after))
	operation.ActionIDs = []string{action.ID}
	operation.IssueKeys = append([]string(nil), action.IssueKeys...)
	plan, err := FinalizeRepairPlan(RepairPlan{Actions: []FixAction{action}, Operations: []RepairOperation{operation}, IssueKeys: action.IssueKeys})
	require.NoError(t, err)
	return plan
}
