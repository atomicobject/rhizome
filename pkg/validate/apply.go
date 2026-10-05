package validate

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

// ApplyFixPlan applies fixes from a plan, respecting safety levels.
func ApplyFixPlan(ctx context.Context, runCtx RunContext, plan *FixPlan, opts Options) (exec *FixExecution, resultErr error) {
	exec = &FixExecution{
		Requested:      opts.Fix,
		NonInteractive: opts.NonInteractive,
	}
	completionArtifact := cloneRepairCompletionArtifact(opts.CompletionArtifact)
	if plan == nil {
		plan = &FixPlan{}
	} else {
		originalFingerprint := plan.Fingerprint
		verified, err := FinalizeRepairPlan(*plan)
		if err != nil {
			return exec, err
		}
		if originalFingerprint != "" && originalFingerprint != verified.Fingerprint {
			return exec, fmt.Errorf("repair plan fingerprint changed; replan before applying")
		}
		plan = &verified
	}
	if len(plan.Operations) == 0 && len(plan.Actions) > 0 {
		upgraded, err := buildRepairPlanFromActions(ctx, runCtx, plan.Actions, NewOntologyOperationAdapter())
		if err != nil {
			return exec, err
		}
		if upgraded != nil {
			plan = upgraded
		}
	}
	finalized, err := FinalizeRepairPlan(*plan)
	if err != nil {
		return exec, err
	}
	plan = &finalized
	exec.PlanFingerprint = plan.Fingerprint
	exec.FollowUps = append([]RepairFollowUp(nil), plan.FollowUps...)
	exec.ReplanCommand = repairReplanCommand(opts)
	exec.PlannedTransactions = len(plan.Transactions)
	exec.PlannedWrites = len(plan.Operations)

	selected, err := selectRepairActions(plan.Actions, opts)
	if err != nil {
		return exec, err
	}
	recordUnbackedRepairActions(exec, plan.Actions, plan.Operations)
	runCtx.VaultPath = strings.TrimSpace(runCtx.VaultPath)
	if runCtx.VaultPath == "" {
		runCtx.VaultPath = strings.TrimSpace(runCtx.VaultDef.BasePath())
	}
	if runCtx.VaultPath == "" {
		return exec, fmt.Errorf("apply repair plan: vault root is required")
	}
	lease, release, err := waitRepairIndexLockLease(ctx, filepath.Join(runCtx.VaultPath, ".rhizome", "index.lock"))
	if err != nil {
		return exec, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, release())
	}()
	if err := lease.RequireHeld(); err != nil {
		return exec, err
	}
	if opts.PostApplyRefresher == nil {
		pending, err := discoverRepairJournals(runCtx)
		if err != nil {
			return exec, err
		}
		for _, journal := range pending {
			if journal.manifest.Namespace != nil && journal.state != repairJournalCleanupPending {
				return exec, fmt.Errorf("native recovery requires a post-apply refresher before mutation")
			}
		}
	}

	recovery, recoveryErr := recoverRepairJournals(runCtx, opts.repairHooks)
	if len(recovery.native)+len(recovery.nativeCleanup) > 0 {
		if opts.PostApplyRefresher == nil && len(recovery.native) > 0 {
			return exec, errors.Join(recoveryErr, fmt.Errorf("native recovery requires a post-apply refresher"))
		}
		exec.RecoveredNamespaces, err = convergeRecoveredNamespaces(ctx, runCtx, lease, opts.PostApplyRefresher, recovery.native, opts.repairHooks)
		exec.RecoveredNamespaces = append(exec.RecoveredNamespaces, namespaceCleanupOutcomes(recovery.nativeCleanup)...)
		genericErr := convergeNamespaceGenericBarrier(ctx, runCtx, lease, opts.PostApplyRefresher, recovery, opts.repairHooks)
		return normalizeFixExecution(exec), errors.Join(recoveryErr, err, genericErr, fmt.Errorf("native namespace work recovered; replan before applying repairs"))
	}
	recoveredWork := len(recovery.committed) + len(recovery.rolledBack) + len(recovery.noMutation) +
		len(recovery.partial) + len(recovery.cleanupPending)
	if recoveryErr != nil && recoveredWork == 0 {
		return exec, recoveryErr
	}
	if len(recovery.cleanupPending) > 0 {
		var cleanupChecks []string
		var cleanupPaths []string
		var cleanupRefreshScope PostApplyRefreshScope
		for _, journal := range recovery.cleanupPending {
			cleanupChecks = append(cleanupChecks, journal.manifest.Checks...)
			cleanupPaths = append(cleanupPaths, repairManifestPostApplyPaths(journal.manifest)...)
			record := recoveryExecution(journal, "recovered_cleanup")
			record.TransactionID = cleanupPendingTransactionID(journal)
			record.Reason = "terminal cleanup resumed without vault mutation"
			record.OperationIDs = nil
			operationIDs, actionIDs, issueKeys, membershipMatches := exactReviewedCleanupMembership(journal, plan)
			if membershipMatches {
				record.OperationIDs = operationIDs
				exec.AppliedTransactions++
				exec.AppliedWrites += len(operationIDs)
				exec.Applied = append(exec.Applied, actionIDs...)
				exec.resolvedRecoveryIssueKeys = append(exec.resolvedRecoveryIssueKeys, issueKeys...)
			}
			exec.Transactions = append(exec.Transactions, record)
		}
		// A cleanup tombstone proves the filesystem transaction already crossed
		// its refresh/postcheck boundary. Its best-effort delta is retained only
		// for the held postcheck scope; replaying it would mutate projections from
		// unauthenticated terminal metadata.
		cleanupRefreshScope.Checks = sortedUnique(cleanupChecks)
		var replanErr error
		if len(plan.Transactions) > 0 || len(plan.Actions) > 0 {
			replanErr = fmt.Errorf("terminal repair cleanup recovered; replan before applying new transactions")
			recordLeaseHeldReplanFailure(exec, plan, selected, replanErr.Error())
		}
		if opts.PostApplyRefresher == nil || opts.postApplyCheck == nil {
			return normalizeFixExecution(exec), errors.Join(
				recoveryErr, replanErr, fmt.Errorf("terminal repair cleanup recovery requires post-apply refresh and held validation"),
			)
		}
		refresh, err := refreshPostApply(ctx, opts.PostApplyRefresher, lease, cleanupRefreshScope)
		exec.Refresh = &refresh
		if err != nil {
			return normalizeFixExecution(exec), errors.Join(recoveryErr, replanErr, err, refresh.Close())
		}
		if err := refresh.validateRuntimeOwnership(); err != nil {
			return normalizeFixExecution(exec), errors.Join(recoveryErr, replanErr, err, refresh.Close())
		}
		scope := repairPostApplyScope{Checks: sortedUnique(cleanupChecks), AffectedPaths: sortedUnique(cleanupPaths)}
		if err := opts.postApplyCheck(ctx, lease, refresh.Runtime, scope); err != nil {
			return normalizeFixExecution(exec), errors.Join(recoveryErr, replanErr, err, refresh.Close())
		}
		if err := refresh.Close(); err != nil {
			return normalizeFixExecution(exec), errors.Join(recoveryErr, replanErr, err)
		}
		return normalizeFixExecution(exec), errors.Join(recoveryErr, replanErr)
	}
	recoveryAllowsLeaseHeldReplan := recoveryErr == nil && len(recovery.committed) == 0 &&
		len(recovery.rolledBack) == 0 && len(recovery.noMutation) == 0 && len(recovery.partial) == 0
	leaseHeldReplanned := false
	if recoveryAllowsLeaseHeldReplan && opts.leaseHeldRepairPlanner != nil {
		replanned, err := opts.leaseHeldRepairPlanner(ctx, lease)
		if err != nil {
			recordLeaseHeldReplanFailure(exec, plan, selected, err.Error())
			return normalizeFixExecution(exec), err
		}
		if replanned == nil {
			err := fmt.Errorf("lease-held repair planner returned no plan")
			recordLeaseHeldReplanFailure(exec, plan, selected, err.Error())
			return normalizeFixExecution(exec), err
		}
		replannedFingerprint := replanned.Fingerprint
		verified, err := FinalizeRepairPlan(*replanned)
		if err != nil {
			recordLeaseHeldReplanFailure(exec, plan, selected, err.Error())
			return normalizeFixExecution(exec), err
		}
		if replannedFingerprint != "" && replannedFingerprint != verified.Fingerprint {
			err := fmt.Errorf("lease-held repair plan fingerprint changed; replan before applying")
			recordLeaseHeldReplanFailure(exec, plan, selected, err.Error())
			return normalizeFixExecution(exec), err
		}
		if plan.Fingerprint != verified.Fingerprint {
			err := fmt.Errorf("lease-held repair differs from reviewed plan; replan before applying")
			recordLeaseHeldReplanFailure(exec, plan, selected, err.Error())
			return normalizeFixExecution(exec), err
		}
		plan = &verified
		leaseHeldReplanned = true
		exec.PlanFingerprint = plan.Fingerprint
		exec.FollowUps = append([]RepairFollowUp(nil), plan.FollowUps...)
		exec.PlannedTransactions = len(plan.Transactions)
		exec.PlannedWrites = len(plan.Operations)
	}
	identifierRecoveryRequiresReplan := plan.RequiresLeaseHeldReplan && !leaseHeldReplanned && len(recovery.committed) == 0
	recoveredByID := make(map[string]recoveredRepairJournal, len(recovery.committed))
	for _, journal := range recovery.committed {
		recoveredByID[journal.manifest.TransactionID] = journal
	}
	verifyJournals := append([]recoveredRepairJournal(nil), recovery.committed...)
	journalsToCleanup := append([]recoveredRepairJournal(nil), recovery.committed...)
	journalsToCleanup = append(journalsToCleanup, recovery.rolledBack...)
	journalsToCleanup = append(journalsToCleanup, recovery.noMutation...)
	var changed, deleted []string
	var renamed []PathRename
	var committedChecks []string
	var refreshScope PostApplyRefreshScope
	plannedTransactionIDs := make(map[string]struct{}, len(plan.Transactions))
	for _, transaction := range plan.Transactions {
		plannedTransactionIDs[transaction.ID] = struct{}{}
	}
	for _, journal := range recovery.committed {
		committedChecks = append(committedChecks, journal.manifest.Checks...)
		changed = append(changed, journal.manifest.Changed...)
		renamed = append(renamed, journal.manifest.Renamed...)
		deleted = append(deleted, journal.manifest.Deleted...)
		refreshScope.add(journal.manifest.Checks, journal.manifest.Changed, journal.manifest.Renamed, journal.manifest.Deleted)
		_, planned := plannedTransactionIDs[journal.manifest.TransactionID]
		exactResume := planned && journal.manifest.PlanFingerprint == plan.Fingerprint
		if !exactResume {
			recordRecoveredRepair(exec, journal, "recovered_committed", recoveredRepairApplied)
		}
	}
	for _, journal := range recovery.rolledBack {
		committedChecks = append(committedChecks, journal.manifest.Checks...)
		rollbackChanged, rollbackRenamed, rollbackDeleted := rollbackRepairDelta(journal.manifest)
		changed = append(changed, rollbackChanged...)
		renamed = append(renamed, rollbackRenamed...)
		deleted = append(deleted, rollbackDeleted...)
		refreshScope.add(journal.manifest.Checks, rollbackChanged, rollbackRenamed, rollbackDeleted)
		recordRecoveredRepair(exec, journal, "recovered_rollback", recoveredRepairFailed)
	}
	for _, journal := range recovery.partial {
		committedChecks = append(committedChecks, journal.manifest.Checks...)
		rollbackChanged, rollbackRenamed, rollbackDeleted := rollbackRepairDelta(journal.manifest)
		changed = append(changed, rollbackChanged...)
		renamed = append(renamed, rollbackRenamed...)
		deleted = append(deleted, rollbackDeleted...)
		refreshScope.add(journal.manifest.Checks, rollbackChanged, rollbackRenamed, rollbackDeleted)
		record := recoveryExecution(journal, "recovery_partial")
		record.Reason = "rollback stopped after a concurrent precondition change; journal retained"
		exec.Transactions = append(exec.Transactions, record)
		exec.Failed = append(exec.Failed, journal.manifest.ActionIDs...)
		exec.RemainingIssueKeys = append(exec.RemainingIssueKeys, journal.manifest.IssueKeys...)
	}
	noMutationByID := make(map[string]struct{}, len(recovery.noMutation))
	for _, journal := range recovery.noMutation {
		committedChecks = append(committedChecks, journal.manifest.Checks...)
		noMutationByID[journal.manifest.TransactionID] = struct{}{}
		rollbackChanged, rollbackRenamed, rollbackDeleted := rollbackRepairDelta(journal.manifest)
		changed = append(changed, rollbackChanged...)
		renamed = append(renamed, rollbackRenamed...)
		deleted = append(deleted, rollbackDeleted...)
		refreshScope.add(journal.manifest.Checks, rollbackChanged, rollbackRenamed, rollbackDeleted)
		_, planned := plannedTransactionIDs[journal.manifest.TransactionID]
		exactResume := planned && journal.manifest.PlanFingerprint == plan.Fingerprint
		if !exactResume {
			record := recoveryExecution(journal, "recovered_prepared")
			record.Reason = "no mutation required"
			exec.Transactions = append(exec.Transactions, record)
			exec.Failed = append(exec.Failed, journal.manifest.ActionIDs...)
			exec.RemainingIssueKeys = append(exec.RemainingIssueKeys, journal.manifest.IssueKeys...)
		}
	}
	operationsByID := make(map[string]RepairOperation, len(plan.Operations))
	for _, operation := range plan.Operations {
		operationsByID[operation.ID] = operation
	}
	applyErr := recoveryErr
	identifierPostApplyUnavailable := plan.RequiresLeaseHeldReplan && (opts.PostApplyRefresher == nil || opts.postApplyCheck == nil)
	recoveryRequiresReplan := identifierRecoveryRequiresReplan || identifierPostApplyUnavailable || recoveryErr != nil || len(recovery.rolledBack) > 0 || len(recovery.noMutation) > 0
	if !recoveryRequiresReplan {
		for _, transaction := range plan.Transactions {
			journal, exactResume := recoveredByID[transaction.ID]
			if !exactResume || journal.manifest.PlanFingerprint != plan.Fingerprint {
				recoveryRequiresReplan = len(recovery.committed) > 0
				if recoveryRequiresReplan {
					break
				}
			}
		}
	}
	if identifierRecoveryRequiresReplan {
		applyErr = errors.Join(applyErr, fmt.Errorf("identifier repair requires lease-held authoritative revalidation before applying; replan"))
	}
	if identifierPostApplyUnavailable {
		applyErr = errors.Join(applyErr, fmt.Errorf("identifier repair requires post-apply refresh and held validation before applying"))
	}
	if recoveryRequiresReplan && len(plan.Operations) > 0 {
		applyErr = errors.Join(applyErr, fmt.Errorf("recovered interrupted repair changed source state; replan before applying"))
	}
	for _, transaction := range plan.Transactions {
		operations := operationsForTransaction(transaction, operationsByID)
		actionIDs := actionIDsForOperations(operations)
		record := RepairTransactionExecution{
			TransactionID: transaction.ID, OperationIDs: transaction.OperationIDs,
			AffectedPaths: transaction.AffectedPaths,
		}
		if journal, ok := recoveredByID[transaction.ID]; ok && journal.manifest.PlanFingerprint == plan.Fingerprint {
			if completionArtifactMatchesManifest(runCtx, completionArtifact, journal.manifest) {
				completionArtifact = nil
			}
			if len(journal.manifest.OperationIDs) > 0 {
				record.OperationIDs = append([]string(nil), journal.manifest.OperationIDs...)
			}
			if len(journal.manifest.ActionIDs) > 0 {
				actionIDs = append([]string(nil), journal.manifest.ActionIDs...)
			}
			record.Status = "recovered"
			exec.AppliedTransactions++
			exec.AppliedWrites += len(operations)
			exec.Applied = append(exec.Applied, actionIDs...)
			exec.Transactions = append(exec.Transactions, record)
			continue
		}
		if !allActionsSelected(actionIDs, selected) {
			record.Status = "skipped"
			record.Reason = "safety selection skipped one or more connected actions"
			exec.Skipped = append(exec.Skipped, actionIDs...)
			exec.RemainingIssueKeys = append(exec.RemainingIssueKeys, issueKeysForOperations(operations)...)
			exec.Transactions = append(exec.Transactions, record)
			continue
		}
		if recoveryRequiresReplan {
			record.Status = "not_attempted"
			record.Reason = "repair recovery requires replanning before new transactions"
			exec.Failed = append(exec.Failed, actionIDs...)
			exec.RemainingIssueKeys = append(exec.RemainingIssueKeys, issueKeysForOperations(operations)...)
			exec.Transactions = append(exec.Transactions, record)
			continue
		}
		if _, recovered := noMutationByID[transaction.ID]; recovered {
			record.Status = "not_attempted"
			record.Reason = "manifest-only recovery must finish before retry; replan"
			exec.Failed = append(exec.Failed, actionIDs...)
			exec.RemainingIssueKeys = append(exec.RemainingIssueKeys, issueKeysForOperations(operations)...)
			exec.Transactions = append(exec.Transactions, record)
			applyErr = errors.Join(applyErr, fmt.Errorf("recovered interrupted repair requires replan before applying"))
			continue
		}
		if len(transaction.Conflicts) > 0 {
			record.Status = "skipped_conflict"
			record.Reason = transaction.Conflicts[0].Message
			exec.Skipped = append(exec.Skipped, actionIDs...)
			exec.RemainingIssueKeys = append(exec.RemainingIssueKeys, issueKeysForOperations(operations)...)
			exec.Transactions = append(exec.Transactions, record)
			continue
		}
		prepared, err := prepareRepairTransaction(
			runCtx, plan.Fingerprint, transaction, operations, opts.AllowHistorical, opts.repairHooks, completionArtifact,
		)
		if err != nil {
			var vacancyErr repairDestinationVacancyError
			if errors.As(err, &vacancyErr) {
				record.Status = "skipped_conflict"
				record.Reason = err.Error()
				exec.Skipped = append(exec.Skipped, actionIDs...)
				exec.RemainingIssueKeys = append(exec.RemainingIssueKeys, issueKeysForOperations(operations)...)
				exec.Transactions = append(exec.Transactions, record)
				continue
			}
			var lifecycleErr lifecycleProtectedError
			if errors.As(err, &lifecycleErr) {
				record.Status = "skipped_lifecycle"
				record.Reason = err.Error()
				exec.Skipped = append(exec.Skipped, actionIDs...)
				exec.RemainingIssueKeys = append(exec.RemainingIssueKeys, issueKeysForOperations(operations)...)
				exec.Transactions = append(exec.Transactions, record)
				continue
			}
			if errors.Is(err, errSimulatedRepairInterruption) {
				record.Status = "interrupted"
				record.Reason = err.Error()
				exec.Transactions = append(exec.Transactions, record)
				exec.Failed = append(exec.Failed, actionIDs...)
				exec.RemainingIssueKeys = append(exec.RemainingIssueKeys, issueKeysForOperations(operations)...)
				applyErr = errors.Join(applyErr, err)
				appendNotAttemptedTransactions(exec, plan.Transactions, transaction.ID, operationsByID, selected)
				break
			}
			var staleErr repairStaleError
			if errors.As(err, &staleErr) {
				record.Status = "failed_stale"
			} else {
				record.Status = "failed_prepare"
			}
			record.Reason = err.Error()
			exec.Transactions = append(exec.Transactions, record)
			exec.Failed = append(exec.Failed, actionIDs...)
			exec.RemainingIssueKeys = append(exec.RemainingIssueKeys, issueKeysForOperations(operations)...)
			applyErr = errors.Join(applyErr, err)
			continue
		}
		// A completion artifact is bound to the first transaction that reaches
		// preparation. Its staged state is now owned by that transaction's
		// journal and must never be attached to another transaction.
		completionArtifact = nil
		if err := commitPreparedRepairTransaction(runCtx, prepared, opts.repairHooks); err != nil {
			if errors.Is(err, errSimulatedRepairInterruption) {
				record.Status = "interrupted"
				record.Reason = err.Error()
				exec.Transactions = append(exec.Transactions, record)
				exec.Failed = append(exec.Failed, actionIDs...)
				exec.RemainingIssueKeys = append(exec.RemainingIssueKeys, issueKeysForOperations(operations)...)
				applyErr = errors.Join(applyErr, err)
				appendNotAttemptedTransactions(exec, plan.Transactions, transaction.ID, operationsByID, selected)
				break
			}
			var rollbackErr error
			if prepared.installed > 0 {
				_, rollbackErr = rollbackRepairJournal(runCtx, prepared.manifest)
				if rollbackErr == nil {
					rollbackErr = cleanupRepairJournal(runCtx, prepared.dir, prepared.manifest, opts.repairHooks)
				}
			}
			record.Status = "failed"
			record.Reason = errors.Join(err, rollbackErr).Error()
			exec.Transactions = append(exec.Transactions, record)
			exec.Failed = append(exec.Failed, actionIDs...)
			exec.RemainingIssueKeys = append(exec.RemainingIssueKeys, issueKeysForOperations(operations)...)
			applyErr = errors.Join(applyErr, err, rollbackErr)
			if rollbackErr != nil {
				appendNotAttemptedTransactions(exec, plan.Transactions, transaction.ID, operationsByID, selected)
				break
			}
			continue
		}
		journalsToCleanup = append(journalsToCleanup, recoveredRepairJournal{
			dir: prepared.dir, manifest: prepared.manifest, state: repairJournalCommitted,
		})
		verifyJournals = append(verifyJournals, recoveredRepairJournal{
			dir: prepared.dir, manifest: prepared.manifest, state: repairJournalCommitted,
		})
		txChanged, txRenamed, txDeleted := repairOperationDelta(operations)
		changed = append(changed, txChanged...)
		renamed = append(renamed, txRenamed...)
		deleted = append(deleted, txDeleted...)
		refreshScope.add(transaction.Checks, txChanged, txRenamed, txDeleted)
		record.Status = "applied"
		committedChecks = append(committedChecks, transaction.Checks...)
		exec.AppliedTransactions++
		exec.AppliedWrites += len(operations)
		exec.Applied = append(exec.Applied, actionIDs...)
		exec.Transactions = append(exec.Transactions, record)
	}

	changed = sortedUnique(changed)
	deleted = sortedUnique(deleted)
	renamed = sortedUniqueRenames(renamed)
	var preparedRuntime *ontology.Runtime
	deltaCount := len(changed) + len(renamed) + len(deleted)
	recoveryCount := len(recovery.committed) + len(recovery.rolledBack) + len(recovery.noMutation) + len(recovery.partial)
	cleanupRecoveryCount := 0 // cleanup-only recovery returns before fresh planning or mutation
	if recoveryCount > 0 && opts.PostApplyRefresher == nil {
		return normalizeFixExecution(exec), errors.Join(
			applyErr, fmt.Errorf("repair journal recovery requires post-apply refresh before cleanup"),
		)
	}
	if (deltaCount > 0 || recoveryCount > 0 || cleanupRecoveryCount > 0) && opts.PostApplyRefresher != nil {
		if err := lease.requireHeldForPath(filepath.Join(runCtx.VaultPath, ".rhizome", "index.lock")); err != nil {
			return normalizeFixExecution(exec), errors.Join(applyErr, err)
		}
		refresh, err := refreshPostApply(ctx, opts.PostApplyRefresher, lease, refreshScope)
		exec.Refresh = &refresh
		if err != nil {
			return normalizeFixExecution(exec), errors.Join(applyErr, err, refresh.Close())
		}
		if err := refresh.validateRuntimeOwnership(); err != nil {
			return normalizeFixExecution(exec), errors.Join(applyErr, err, refresh.Close())
		}
		preparedRuntime = refresh.Runtime
	}
	for _, journal := range verifyJournals {
		if err := verifyCommittedRepairJournal(runCtx, journal.manifest); err != nil {
			return normalizeFixExecution(exec), errors.Join(applyErr, err, closePostApplyRefresh(exec.Refresh))
		}
	}
	if identifierPostApplyUnavailable && (len(journalsToCleanup) > 0 || len(recovery.partial) > 0 || cleanupRecoveryCount > 0) {
		return normalizeFixExecution(exec), errors.Join(applyErr, closePostApplyRefresh(exec.Refresh))
	}
	if opts.postApplyCheck != nil && (len(journalsToCleanup) > 0 || len(recovery.partial) > 0 || cleanupRecoveryCount > 0) {
		scope := newRepairPostApplyScope(committedChecks, changed, deleted, renamed)
		if err := opts.postApplyCheck(ctx, lease, preparedRuntime, scope); err != nil {
			return normalizeFixExecution(exec), errors.Join(applyErr, err, closePostApplyRefresh(exec.Refresh))
		}
	}
	if exec.Refresh != nil {
		if err := exec.Refresh.Close(); err != nil {
			return normalizeFixExecution(exec), errors.Join(applyErr, err)
		}
	}
	for _, journal := range journalsToCleanup {
		if err := cleanupRepairJournal(runCtx, journal.dir, journal.manifest, opts.repairHooks); err != nil {
			applyErr = errors.Join(applyErr, err)
		}
	}
	return normalizeFixExecution(exec), applyErr
}

func recordLeaseHeldReplanFailure(exec *FixExecution, plan *FixPlan, selected map[string]bool, reason string) {
	operationsByID := make(map[string]RepairOperation, len(plan.Operations))
	for _, operation := range plan.Operations {
		operationsByID[operation.ID] = operation
	}
	for _, transaction := range plan.Transactions {
		operations := operationsForTransaction(transaction, operationsByID)
		actionIDs := actionIDsForOperations(operations)
		record := RepairTransactionExecution{
			TransactionID: transaction.ID,
			OperationIDs:  append([]string(nil), transaction.OperationIDs...),
			AffectedPaths: append([]string(nil), transaction.AffectedPaths...),
			Reason:        reason,
		}
		if allActionsSelected(actionIDs, selected) {
			record.Status = "not_attempted"
			exec.Failed = append(exec.Failed, actionIDs...)
		} else {
			record.Status = "skipped"
			exec.Skipped = append(exec.Skipped, actionIDs...)
		}
		exec.RemainingIssueKeys = append(exec.RemainingIssueKeys, issueKeysForOperations(operations)...)
		exec.Transactions = append(exec.Transactions, record)
	}
}

func closePostApplyRefresh(refresh *PostApplyRefreshResult) error {
	if refresh == nil {
		return nil
	}
	return refresh.Close()
}
