package sourcetxn

func preflightNativeNamespace(runCtx writerContext, journal journal) error {
	if journal.State != repairJournalPrepared {
		return nil
	}
	if _, err := preflightRepairRollback(runCtx, journal.Manifest); err != nil {
		return err
	}
	return preflightNamespaceGitRollback(runCtx, journal.Manifest)
}

func stabilizeNativeNamespace(runCtx writerContext, journal *journal, hooks *Hooks) error {
	if journal.State == repairJournalPrepared {
		prepared := &preparedTransaction{Directory: journal.Directory, Manifest: journal.Manifest}
		err := restoreNamespacePrepared(runCtx, prepared, hooks)
		journal.Manifest = prepared.Manifest
		journal.DecisionSynced = prepared.DecisionSynced
		if decision, decisionErr := nativeJournalDecision(journal.Directory); decisionErr == nil {
			journal.State = decision
		}
		return err
	}
	return settleNamespaceGitRelease(runCtx, journal)
}
