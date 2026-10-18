package sourcetxn

// abortPreparedRepairTransaction undoes only mutations completed by this live
// attempt. Uninstalled sources remain the authority of their external writers.
// The complete manifest still owns cleanup artifacts and rename witnesses.
func abortPreparedRepairTransaction(run writerContext, prepared *preparedTransaction, hooks *Hooks) error {
	if err := run.Lease.Validate(run.VaultPath); err != nil {
		return err
	}
	if prepared.Manifest.Namespace != nil {
		if err := restoreNamespacePrepared(run, prepared, hooks); err != nil {
			return err
		}
		return cleanupRepairJournal(run, prepared.Directory, prepared.Manifest, hooks)
	}
	if prepared.Installed > 0 {
		plan, err := buildRepairRollbackPlan(run, prepared.Manifest, prepared.Installed)
		if err != nil {
			return err
		}
		if _, err := executeRepairRollbackPlan(run, prepared.Manifest, plan, hooks); err != nil {
			return err
		}
	}
	return cleanupFailedPrepare(run, prepared.Directory, prepared.Manifest, hooks)
}
