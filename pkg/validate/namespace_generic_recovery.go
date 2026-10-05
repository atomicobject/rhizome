package validate

import (
	"context"
	"errors"
	"fmt"
)

// A native entrypoint may settle existing generic repair work, but must retain
// generic validation's exact prepared-runtime postcheck and strict witnesses.
func convergeNamespaceGenericBarrier(ctx context.Context, runCtx RunContext, lease *IndexLockLease, refresher PostApplyRefresher, recovery repairJournalRecovery, hooks *repairExecutionHooks) error {
	journals := append(append(append([]recoveredRepairJournal(nil), recovery.committed...), recovery.rolledBack...), recovery.noMutation...)
	if len(journals)+len(recovery.partial)+len(recovery.cleanupPending) == 0 {
		return nil
	}
	if refresher == nil {
		return fmt.Errorf("generic repair recovery requires a post-apply refresher")
	}
	var scope PostApplyRefreshScope
	var affected []string
	for _, journal := range recovery.committed {
		scope.add(journal.manifest.Checks, journal.manifest.Changed, journal.manifest.Renamed, journal.manifest.Deleted)
	}
	for _, journal := range append(append(append([]recoveredRepairJournal(nil), recovery.rolledBack...), recovery.noMutation...), recovery.partial...) {
		changed, renamed, deleted := rollbackRepairDelta(journal.manifest)
		scope.add(journal.manifest.Checks, changed, renamed, deleted)
	}
	for _, journal := range append(append([]recoveredRepairJournal(nil), journals...), recovery.cleanupPending...) {
		affected = append(affected, repairManifestPostApplyPaths(journal.manifest)...)
		scope.Checks = append(scope.Checks, journal.manifest.Checks...)
	}
	refresh, err := refreshPostApply(ctx, refresher, lease, scope)
	if err != nil {
		return errors.Join(err, refresh.Close())
	}
	if err := refresh.validateRuntimeOwnership(); err != nil {
		return errors.Join(err, refresh.Close())
	}
	var checked *Result
	postcheck := repairSessionPostcheck(runCtx, Options{Checks: sortedUnique(scope.Checks)}, &checked, false)
	if err := postcheck(ctx, lease, refresh.Runtime, repairPostApplyScope{Checks: sortedUnique(scope.Checks), AffectedPaths: sortedUnique(affected)}); err != nil {
		return errors.Join(err, refresh.Close())
	}
	if err := refresh.Close(); err != nil {
		return err
	}
	if len(recovery.partial) > 0 {
		return fmt.Errorf("generic repair rollback remains unresolved")
	}
	for _, journal := range journals {
		if journal.state == repairJournalCommitted {
			if err := verifyCommittedRepairJournal(runCtx, journal.manifest); err != nil {
				return err
			}
		}
		if err := cleanupRepairJournal(runCtx, journal.dir, journal.manifest, hooks); err != nil {
			return err
		}
	}
	return nil
}
