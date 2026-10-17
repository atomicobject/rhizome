package validate

import "github.com/atomicobject/rhizome/pkg/sourcetxn"

func stageRepairFileStates(run RunContext, fingerprint string, tx RepairTransaction, operations []RepairOperation, states []repairFileState, completion *RepairCompletionArtifact) (*sourcetxn.Transaction, error) {
	changed, renamed, deleted := repairOperationDelta(operations)
	request := sourcetxn.RepairRequest{TransactionID: tx.ID, Fingerprint: fingerprint, OperationIDs: append([]string(nil), tx.OperationIDs...), ActionIDs: actionIDsForOperations(operations), IssueKeys: issueKeysForOperations(operations), Checks: append([]string(nil), tx.Checks...), Changed: changed, Renamed: renamed, Deleted: deleted, Files: sourceChanges(states)}
	if completion != nil {
		request.Completion = &sourcetxn.CompletionArtifact{Path: completion.Path, Content: completion.Content}
	}
	return run.sourceWriter.PrepareRepair(request)
}
