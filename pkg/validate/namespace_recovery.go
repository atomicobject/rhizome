package validate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
)

// Native replay ends historical rollback authority at a terminal decision.
// Projection convergence always observes today's complete recorded footprint.
func preflightNativeNamespace(runCtx RunContext, journal recoveredRepairJournal) error {
	if journal.state != repairJournalPrepared {
		return nil
	}
	if _, err := buildRepairRollbackPlan(runCtx, journal.manifest); err != nil {
		return err
	}
	return preflightNamespaceGitRollback(runCtx, journal.manifest)
}

func stabilizeNativeNamespace(runCtx RunContext, journal *recoveredRepairJournal, hooks *repairExecutionHooks) error {
	if journal.state == repairJournalPrepared {
		prepared := &preparedRepairTransaction{dir: journal.dir, manifest: journal.manifest}
		err := restoreNamespacePrepared(runCtx, prepared, hooks)
		journal.manifest = prepared.manifest
		journal.decisionSynced = prepared.decisionSynced
		if decision, decisionErr := nativeJournalDecision(journal.dir); decisionErr == nil {
			journal.state = decision
		}
		return err
	}
	return settleNamespaceGitRelease(runCtx, journal)
}

func namespaceJournalOutcome(journal recoveredRepairJournal) NamespaceOutcome {
	outcome := NamespaceOutcome{
		Decision: NamespaceUnresolved, TransactionID: journal.manifest.TransactionID,
		Moves: append([]PathRename(nil), journal.manifest.Renamed...), RecoveryPending: true,
	}
	if purpose := journal.manifest.Namespace; purpose != nil {
		outcome.Moves = append([]PathRename(nil), purpose.Moves...)
		outcome.Summary = append([]byte(nil), purpose.Summary...)
		outcome.GitMoves = append([]PathRename(nil), purpose.GitMoves...)
		if journal.decisionSynced || purpose.SettledDecision != "" {
			switch journal.state {
			case repairJournalCommitted:
				outcome.Decision = NamespaceCommitted
			case repairJournalRestored:
				outcome.Decision = NamespaceRestored
			}
		}
	}
	if outcome.Decision == NamespaceCommitted || journal.nativeCleanupDecision == string(NamespaceCommitted) {
		for _, entry := range journal.manifest.Entries {
			if entry.Internal {
				outcome.ReceiptPath = entry.Path
			}
		}
	}
	return outcome
}

func namespaceCleanupOutcomes(journals []recoveredRepairJournal) []NamespaceOutcome {
	var outcomes []NamespaceOutcome
	for _, journal := range journals {
		outcome := namespaceJournalOutcome(journal)
		outcome.Decision = NamespaceDecision(journal.nativeCleanupDecision)
		_, err := os.Lstat(journal.dir)
		outcome.RecoveryPending = !os.IsNotExist(err)
		outcomes = append(outcomes, outcome)
	}
	return outcomes
}

func convergeNativeNamespace(ctx context.Context, runCtx RunContext, lease *IndexLockLease, refresher PostApplyRefresher, journal recoveredRepairJournal, initial bool, hooks *repairExecutionHooks) error {
	if journal.manifest.Namespace.SettledDecision == "" {
		return fmt.Errorf("native decision release is unsettled")
	}
	files, changed, deleted, err := snapshotNamespaceFootprint(runCtx, journal.manifest)
	if err != nil {
		return err
	}
	var renamed []PathRename
	if initial {
		changed, renamed, deleted = journal.manifest.Changed, journal.manifest.Renamed, journal.manifest.Deleted
	}
	// Use the ordinary exact refresher: every present recovery path needs anchors.
	refresh, refreshErr := refresher.Refresh(ctx, lease, changed, renamed, deleted)
	if refreshErr != nil {
		return errors.Join(refreshErr, refresh.Close())
	}
	checkErr := refresh.validateRuntimeOwnership()
	covered := make(map[string]bool, len(refresh.Paths))
	for _, path := range refresh.Paths {
		covered[path] = true
	}
	required := append(repairManifestPostApplyPaths(journal.manifest), changed...)
	required = append(required, deleted...)
	for _, file := range files {
		required = append(required, file.path)
	}
	for _, rename := range renamed {
		required = append(required, rename.From, rename.To)
	}
	for _, path := range sortedUnique(required) {
		if !covered[path] {
			checkErr = errors.Join(checkErr, fmt.Errorf("native refresher omitted path %s", path))
		}
	}
	after, _, _, snapshotErr := snapshotNamespaceFootprint(runCtx, journal.manifest)
	checkErr = errors.Join(checkErr, snapshotErr)
	if snapshotErr == nil && !slices.Equal(files, after) {
		checkErr = errors.Join(checkErr, fmt.Errorf("native refresh footprint changed during refresh"))
	}
	if err := errors.Join(checkErr, refresh.Close()); err != nil {
		return err
	}
	return cleanupRepairJournal(runCtx, journal.dir, journal.manifest, hooks)
}

func convergeRecoveredNamespaces(ctx context.Context, runCtx RunContext, lease *IndexLockLease, refresher PostApplyRefresher, journals []recoveredRepairJournal, hooks *repairExecutionHooks) ([]NamespaceOutcome, error) {
	var outcomes []NamespaceOutcome
	var recoveryErr error
	for _, journal := range journals {
		outcome := namespaceJournalOutcome(journal)
		if outcome.Decision != NamespaceUnresolved && journal.manifest.Namespace.SettledDecision != "" {
			if err := convergeNativeNamespace(ctx, runCtx, lease, refresher, journal, false, hooks); err != nil {
				recoveryErr = errors.Join(recoveryErr, fmt.Errorf("converge native transaction %s: %w", outcome.TransactionID, err))
			} else {
				outcome.RecoveryPending = false
			}
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes, recoveryErr
}
