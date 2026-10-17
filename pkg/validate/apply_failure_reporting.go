package validate

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
