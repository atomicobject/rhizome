package validate

import "fmt"

func recoverRepairJournals(runCtx RunContext, hookOptions ...*repairExecutionHooks) (repairJournalRecovery, error) {
	journals, err := discoverRepairJournals(runCtx)
	if err != nil {
		return repairJournalRecovery{}, err
	}
	for _, journal := range journals {
		if journal.manifest.Namespace == nil && journal.state == repairJournalCommitted {
			if err := verifyCommittedRepairJournal(runCtx, journal.manifest); err != nil {
				return repairJournalRecovery{}, fmt.Errorf("recover committed transaction %s: %w", journal.manifest.TransactionID, err)
			}
		}
	}
	return stabilizeRepairJournals(runCtx, journals, hookOptions...)
}

// Startup needs stable source files, not proof that a completed repair still
// matches later user edits. Strict recovery above owns that proof before any
// repair refresh, postcheck, or cleanup.
func stabilizeRepairJournals(runCtx RunContext, journals []recoveredRepairJournal, hookOptions ...*repairExecutionHooks) (repairJournalRecovery, error) {
	var recovery repairJournalRecovery
	rollbackPlans := make(map[string]repairRollbackPlan, len(journals))
	// Preflight every journal before mutating any vault path. A later corrupt
	// journal must not strand an earlier rollback without its refresh delta.
	for _, journal := range journals {
		if journal.manifest.Namespace != nil && journal.state != repairJournalCleanupPending {
			if err := preflightNativeNamespace(runCtx, journal); err != nil {
				recovery.native = append(recovery.native, journal)
				return recovery, fmt.Errorf("preflight native transaction %s: %w", journal.manifest.TransactionID, err)
			}
			continue
		}
		if journal.state == repairJournalCleanupPending {
			if err := validateRepairJournalMetadata(journal.dir); err != nil {
				return recovery, fmt.Errorf("resume repair cleanup %s: %w", journal.dir, err)
			}
			continue
		}
		if journal.state == repairJournalCommitted {
			continue
		}
		plan, err := buildRepairRollbackPlan(runCtx, journal.manifest)
		if err != nil {
			return recovery, fmt.Errorf("preflight interrupted transaction %s: %w", journal.manifest.TransactionID, err)
		}
		rollbackPlans[journal.dir] = plan
	}
	for _, journal := range journals {
		if journal.manifest.Namespace != nil && journal.state != repairJournalCleanupPending {
			err := stabilizeNativeNamespace(runCtx, &journal, firstRepairExecutionHooks(hookOptions))
			recovery.native = append(recovery.native, journal)
			if err != nil {
				return recovery, fmt.Errorf("stabilize native transaction %s: %w", journal.manifest.TransactionID, err)
			}
			continue
		}
		if journal.state == repairJournalCleanupPending {
			if err := cleanupDetachedRepairJournal(runCtx, journal.dir, nil); err != nil {
				if journal.nativeCleanupDecision != "" {
					recovery.nativeCleanup = append(recovery.nativeCleanup, journal)
				}
				return recovery, fmt.Errorf("resume repair cleanup %s: %w", journal.dir, err)
			}
			if journal.nativeCleanupDecision != "" {
				recovery.nativeCleanup = append(recovery.nativeCleanup, journal)
			} else {
				recovery.cleanupPending = append(recovery.cleanupPending, journal)
			}
			continue
		}
		if journal.state == repairJournalCommitted {
			recovery.committed = append(recovery.committed, journal)
			continue
		}
		mutated, err := executeRepairRollbackPlan(
			runCtx, journal.manifest, rollbackPlans[journal.dir], firstRepairExecutionHooks(hookOptions),
		)
		if err != nil {
			if mutated {
				journal.state = "recovery_partial"
				recovery.partial = append(recovery.partial, journal)
			}
			return recovery, fmt.Errorf("rollback interrupted transaction %s: %w", journal.manifest.TransactionID, err)
		}
		if !mutated {
			journal.state = "recovered_prepared"
			recovery.noMutation = append(recovery.noMutation, journal)
			continue
		}
		journal.state = "rolled_back"
		recovery.rolledBack = append(recovery.rolledBack, journal)
	}
	return recovery, nil
}
