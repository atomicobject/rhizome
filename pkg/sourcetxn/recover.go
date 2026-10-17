package sourcetxn

import (
	"fmt"
	"os"
	"path/filepath"
)

func discoverRecoverableRepairJournals(run writerContext, policy func(Manifest) error, hookOptions ...*Hooks) ([]journal, error) {
	if err := reconcileQuarantine(run); err != nil {
		return nil, err
	}
	journalRoot, err := repairJournalRoot(run)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(journalRoot)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var eligible []journal
	for _, entry := range entries {
		dir := filepath.Join(journalRoot, entry.Name())
		discarded, discardErr := discardUnpublishedPrepare(run, dir)
		if discardErr != nil {
			return nil, discardErr
		}
		if discarded {
			continue
		}
		scoped := run
		scoped.onlyDirectory = dir
		scoped.includeQuarantined = true
		found, discoverErr := discoverRepairJournals(scoped)
		if discoverErr != nil {
			if err := quarantineRepair(run, dir, discoverErr); err != nil {
				return nil, err
			}
			continue
		}
		for _, journal := range found {
			if run.role == SourceEditor && journal.Manifest.Namespace != nil && journal.State != repairJournalCleanupPending {
				decision := journal.Manifest.Namespace.SettledDecision
				if (journal.State == repairJournalCommitted || journal.State == repairJournalRestored) && decision == journal.State {
					// The namespace owner already durably released its source and Git
					// authority. Ordinary editors leave its remaining convergence work
					// to that owner and may edit the settled source.
					continue
				}
				return nil, fmt.Errorf("namespace transaction %s requires recovery by its namespace owner before source editing; preserve the evidence and review source recovery before writing", journal.Manifest.TransactionID)
			}
			if settled, settleErr := sourceIsSettled(dir, journal.Manifest); settleErr != nil {
				if err := quarantineRepair(run, dir, settleErr); err != nil {
					return nil, err
				}
				continue
			} else if settled {
				if _, err := classifySourceSettlement(run, dir, journal.Manifest, nil, nil); err != nil {
					return nil, err
				}
				continue
			}
			preflightErr := validateRepairJournalMetadata(journal.Directory)
			if preflightErr == nil && policy != nil {
				preflightErr = policy(journal.Manifest)
			}
			if preflightErr != nil {
				if err := quarantineRepair(run, dir, preflightErr); err != nil {
					return nil, err
				}
				continue
			}
			if journal.State == repairJournalPrepared {
				preflightErr = adoptUnownedPrepareArtifacts(run, dir, &journal.Manifest)
			}
			switch {
			case preflightErr != nil:
			case journal.Manifest.Namespace != nil && journal.State != repairJournalCleanupPending:
				preflightErr = preflightNativeNamespace(run, journal)
			case journal.State == repairJournalCleanupPending:
				preflightErr = validateRepairJournalMetadata(journal.Directory)
			case journal.State == repairJournalCommitted:
				preflightErr = validateRepairArtifactOwnership(journal.Manifest)
			case journal.State != repairJournalCommitted:
				_, preflightErr = preflightRepairRollback(run, journal.Manifest)
			}
			if journal.State == repairJournalPrepared && preflightErr != nil {
				settled, err := classifySourceSettlement(run, dir, journal.Manifest, preflightErr, firstRepairExecutionHooks(hookOptions))
				if err != nil {
					return nil, err
				}
				if settled {
					continue
				}
			}
			if preflightErr != nil {
				if err := quarantineRepair(run, dir, preflightErr); err != nil {
					return nil, err
				}
				continue
			}
			if err := clearQuarantine(run, dir); err != nil {
				return nil, err
			}
			eligible = append(eligible, journal)
		}
	}
	if err := reconcileQuarantine(run); err != nil {
		return nil, err
	}
	return outsideQuarantine(run, eligible)
}
