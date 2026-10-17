package sourcetxn

import "fmt"

// Runtime startup stabilizes source while preserving later user edits. The
// repair-owner role separately verifies committed snapshots before refresh,
// postcheck or cleanup.
func stabilizeRepairJournals(runCtx writerContext, journals []journal, hookOptions ...*Hooks) (recovery, error) {
	if err := runCtx.Lease.Validate(runCtx.VaultPath); err != nil {
		return recovery{}, err
	}

	var recovery recovery
	rollbackPlans := make(map[string]rollbackPlan, len(journals))
	// Preflight every journal before mutating any vault path. A later corrupt
	// journal must not strand an earlier rollback without its refresh delta.
	for _, journal := range journals {
		if journal.Manifest.Namespace != nil && journal.State != repairJournalCleanupPending {
			if err := preflightNativeNamespace(runCtx, journal); err != nil {
				recovery.Native = append(recovery.Native, journal)
				return recovery, fmt.Errorf("preflight native transaction %s: %w", journal.Manifest.TransactionID, err)
			}
			continue
		}
		if journal.State == repairJournalCleanupPending {
			if err := validateRepairJournalMetadata(journal.Directory); err != nil {
				return recovery, fmt.Errorf("resume repair cleanup %s: %w", journal.Directory, err)
			}
			continue
		}
		if journal.State == repairJournalCommitted {
			continue
		}
		plan, err := preflightRepairRollback(runCtx, journal.Manifest)
		if err != nil {
			return recovery, fmt.Errorf("preflight interrupted transaction %s: %w", journal.Manifest.TransactionID, err)
		}
		rollbackPlans[journal.Directory] = plan
	}
	for _, journal := range journals {
		if journal.Manifest.Namespace != nil && journal.State != repairJournalCleanupPending {
			err := stabilizeNativeNamespace(runCtx, &journal, firstRepairExecutionHooks(hookOptions))
			recovery.Native = append(recovery.Native, journal)
			if err != nil {
				return recovery, fmt.Errorf("stabilize native transaction %s: %w", journal.Manifest.TransactionID, err)
			}
			continue
		}
		if journal.State == repairJournalCleanupPending {
			if err := cleanupDetachedRepairJournal(runCtx, journal.Directory, nil); err != nil {
				if journal.NativeCleanupDecision != "" {
					recovery.NativeCleanup = append(recovery.NativeCleanup, journal)
				}
				return recovery, fmt.Errorf("resume repair cleanup %s: %w", journal.Directory, err)
			}
			if journal.NativeCleanupDecision != "" {
				recovery.NativeCleanup = append(recovery.NativeCleanup, journal)
			} else {
				recovery.CleanupPending = append(recovery.CleanupPending, journal)
			}
			continue
		}
		if journal.State == repairJournalCommitted {
			recovery.Committed = append(recovery.Committed, journal)
			continue
		}
		mutated, err := executeRepairRollbackPlan(
			runCtx, journal.Manifest, rollbackPlans[journal.Directory], firstRepairExecutionHooks(hookOptions),
		)
		if err != nil {
			if mutated {
				journal.State = "recovery_partial"
				recovery.Partial = append(recovery.Partial, journal)
			}
			return recovery, fmt.Errorf("rollback interrupted transaction %s: %w", journal.Manifest.TransactionID, err)
		}
		if !mutated {
			journal.State = "recovered_prepared"
			recovery.NoMutation = append(recovery.NoMutation, journal)
			continue
		}
		journal.State = "rolled_back"
		recovery.RolledBack = append(recovery.RolledBack, journal)
	}
	return recovery, nil
}
