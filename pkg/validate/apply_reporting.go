package validate

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

func cleanupPendingTransactionID(journal recoveredRepairJournal) string {
	if journal.manifest.TransactionID != "" {
		return journal.manifest.TransactionID
	}
	return "cleanup:" + strings.TrimPrefix(filepath.Base(journal.dir), repairJournalCleanupPrefix)
}

func exactReviewedCleanupMembership(
	journal recoveredRepairJournal,
	plan *FixPlan,
) (operationIDs, actionIDs, issueKeys []string, ok bool) {
	if plan == nil || journal.manifest.PlanFingerprint == "" || journal.manifest.PlanFingerprint != plan.Fingerprint {
		return nil, nil, nil, false
	}
	operationsByID := make(map[string]RepairOperation, len(plan.Operations))
	for _, operation := range plan.Operations {
		operationsByID[operation.ID] = operation
	}
	for _, transaction := range plan.Transactions {
		if transaction.ID != journal.manifest.TransactionID {
			continue
		}
		operations := operationsForTransaction(transaction, operationsByID)
		operationIDs = append([]string(nil), transaction.OperationIDs...)
		actionIDs = actionIDsForOperations(operations)
		issueKeys = issueKeysForOperations(operations)
		if !slices.Equal(operationIDs, journal.manifest.OperationIDs) {
			return nil, nil, nil, false
		}
		return operationIDs, actionIDs, issueKeys, true
	}
	return nil, nil, nil, false
}

func recordUnbackedRepairActions(exec *FixExecution, actions []FixAction, operations []RepairOperation) {
	backed := make(map[string]struct{}, len(actions))
	for _, operation := range operations {
		for _, actionID := range operation.ActionIDs {
			backed[actionID] = struct{}{}
		}
	}
	for _, action := range actions {
		if _, represented := backed[action.ID]; represented {
			continue
		}
		exec.Skipped = append(exec.Skipped, action.ID)
		exec.RemainingIssueKeys = append(exec.RemainingIssueKeys, action.IssueKeys...)
	}
}

func recoveryExecution(journal recoveredRepairJournal, status string) RepairTransactionExecution {
	paths := make([]string, 0, len(journal.manifest.Entries))
	for _, entry := range journal.manifest.Entries {
		if entry.Internal {
			continue
		}
		paths = append(paths, entry.Path)
	}
	return RepairTransactionExecution{
		TransactionID: journal.manifest.TransactionID,
		Status:        status,
		OperationIDs:  append([]string(nil), journal.manifest.OperationIDs...),
		AffectedPaths: sortedUnique(paths),
	}
}

type recoveredRepairOutcome int

const (
	recoveredRepairApplied recoveredRepairOutcome = iota
	recoveredRepairFailed
)

func recordRecoveredRepair(
	exec *FixExecution,
	journal recoveredRepairJournal,
	status string,
	outcome recoveredRepairOutcome,
) {
	exec.Transactions = append(exec.Transactions, recoveryExecution(journal, status))
	switch outcome {
	case recoveredRepairApplied:
		exec.AppliedTransactions++
		exec.AppliedWrites += len(journal.manifest.OperationIDs)
		exec.Applied = append(exec.Applied, journal.manifest.ActionIDs...)
		exec.resolvedRecoveryIssueKeys = append(exec.resolvedRecoveryIssueKeys, journal.manifest.IssueKeys...)
	case recoveredRepairFailed:
		exec.Failed = append(exec.Failed, journal.manifest.ActionIDs...)
		exec.RemainingIssueKeys = append(exec.RemainingIssueKeys, journal.manifest.IssueKeys...)
	}
}

func rollbackRepairDelta(manifest repairJournalManifest) ([]string, []PathRename, []string) {
	destinationToSource := make(map[string]string, len(manifest.Renamed))
	entries := make(map[string]repairJournalEntry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		entries[entry.Path] = entry
	}
	var renamed []PathRename
	for _, rename := range manifest.Renamed {
		destinationToSource[rename.To] = rename.From
		renamed = append(renamed, PathRename{From: rename.To, To: rename.From})
	}
	changed := append([]string(nil), manifest.Deleted...)
	var deleted []string
	for _, path := range manifest.Changed {
		if source := destinationToSource[path]; source != "" {
			changed = append(changed, source)
			continue
		}
		entry := entries[path]
		if !entry.OriginalExists {
			deleted = append(deleted, path)
			continue
		}
		changed = append(changed, path)
	}
	return sortedUnique(changed), sortedUniqueRenames(renamed), sortedUnique(deleted)
}

func selectRepairActions(actions []FixAction, opts Options) (map[string]bool, error) {
	if len(opts.ApplySelection) > 0 {
		return selectReviewedRepairActions(actions, opts.ApplySelection)
	}
	selected := make(map[string]bool, len(actions))
	for _, action := range actions {
		apply := action.Safety == FixSafetySafe
		if action.Safety == FixSafetyConfirm && !opts.NonInteractive && opts.Confirm != nil {
			ok, err := opts.Confirm(action.Question)
			if err != nil {
				return nil, err
			}
			apply = ok
		}
		selected[action.ID] = apply
	}
	return selected, nil
}

// selectReviewedRepairActions applies exactly the caller-reviewed actions,
// regardless of safety tier or confirmation callback. It fails closed before
// any lease or journal work when an entry matches nothing or names an action
// without deterministic edits.
func selectReviewedRepairActions(actions []FixAction, entries []string) (map[string]bool, error) {
	matched := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry = strings.TrimSpace(entry); entry != "" {
			matched[entry] = false
		}
	}
	if len(matched) == 0 {
		return nil, fmt.Errorf("repair action selection is empty")
	}
	selected := make(map[string]bool, len(actions))
	var agentRequired []string
	for _, action := range actions {
		apply := false
		for _, identity := range append([]string{action.ID}, action.IssueKeys...) {
			if _, requested := matched[identity]; requested {
				matched[identity] = true
				apply = true
			}
		}
		if apply && action.Safety == FixSafetyAgent {
			agentRequired = append(agentRequired, action.ID)
		}
		selected[action.ID] = apply
	}
	var unmatched []string
	for entry, found := range matched {
		if !found {
			unmatched = append(unmatched, entry)
		}
	}
	if len(unmatched) > 0 {
		sort.Strings(unmatched)
		return nil, fmt.Errorf("selected repair action(s) not found in the current plan: %s; replan and select action IDs or issue keys from it", strings.Join(unmatched, ", "))
	}
	if len(agentRequired) > 0 {
		return nil, fmt.Errorf("selected repair action(s) are agent_required and have no deterministic edits: %s", strings.Join(sortedUnique(agentRequired), ", "))
	}
	return selected, nil
}

func operationsForTransaction(transaction RepairTransaction, byID map[string]RepairOperation) []RepairOperation {
	operations := make([]RepairOperation, 0, len(transaction.OperationIDs))
	for _, id := range transaction.OperationIDs {
		operations = append(operations, byID[id])
	}
	return operations
}

func actionIDsForOperations(operations []RepairOperation) []string {
	ids := make([]string, 0, len(operations))
	for _, operation := range operations {
		ids = append(ids, operation.ActionID)
		ids = append(ids, operation.ActionIDs...)
	}
	return sortedUnique(ids)
}

func issueKeysForOperations(operations []RepairOperation) []string {
	keys := make([]string, 0, len(operations))
	for _, operation := range operations {
		keys = append(keys, operation.IssueKey)
		keys = append(keys, operation.IssueKeys...)
	}
	return sortedUnique(keys)
}

func allActionsSelected(actionIDs []string, selected map[string]bool) bool {
	for _, actionID := range actionIDs {
		if !selected[actionID] {
			return false
		}
	}
	return true
}

func repairReplanCommand(opts Options) string {
	if command := strings.TrimSpace(opts.ReplanCommand); command != "" {
		return command
	}
	if len(opts.Checks) == 1 {
		return "rzm validate fix " + opts.Checks[0]
	}
	return "rzm validate fix default"
}

func appendNotAttemptedTransactions(
	exec *FixExecution,
	transactions []RepairTransaction,
	interruptedID string,
	operationsByID map[string]RepairOperation,
	selected map[string]bool,
) {
	found := false
	for _, transaction := range transactions {
		if transaction.ID == interruptedID {
			found = true
			continue
		}
		if !found {
			continue
		}
		operations := operationsForTransaction(transaction, operationsByID)
		actionIDs := actionIDsForOperations(operations)
		status := "not_attempted"
		reason := "earlier transaction was interrupted"
		if allActionsSelected(actionIDs, selected) {
			exec.Failed = append(exec.Failed, actionIDs...)
		} else {
			status = "skipped"
			reason = "safety selection skipped one or more connected actions"
			exec.Skipped = append(exec.Skipped, actionIDs...)
		}
		exec.Transactions = append(exec.Transactions, RepairTransactionExecution{
			TransactionID: transaction.ID,
			Status:        status,
			Reason:        reason,
			OperationIDs:  transaction.OperationIDs,
			AffectedPaths: transaction.AffectedPaths,
		})
		exec.RemainingIssueKeys = append(exec.RemainingIssueKeys, issueKeysForOperations(operations)...)
	}
}

func sortedUniqueRenames(values []PathRename) []PathRename {
	seen := make(map[string]struct{}, len(values))
	result := make([]PathRename, 0, len(values))
	for _, value := range values {
		key := value.From + "\x00" + value.To
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].From != result[j].From {
			return result[i].From < result[j].From
		}
		return result[i].To < result[j].To
	})
	return result
}

func normalizeFixExecution(exec *FixExecution) *FixExecution {
	exec.Applied = sortedUnique(exec.Applied)
	exec.Skipped = sortedUniqueExcluding(exec.Skipped, exec.Applied)
	exec.Failed = sortedUniqueExcluding(exec.Failed, append(exec.Applied, exec.Skipped...))
	exec.RemainingIssueKeys = sortedUniqueExcluding(exec.RemainingIssueKeys, exec.resolvedRecoveryIssueKeys)
	if exec.RemainingFindings == 0 {
		exec.RemainingFindings = len(exec.RemainingIssueKeys)
	}
	return exec
}

func sortedUniqueExcluding(values, excluded []string) []string {
	excludedSet := make(map[string]struct{}, len(excluded))
	for _, value := range excluded {
		excludedSet[value] = struct{}{}
	}
	result := sortedUnique(values)
	result = slices.DeleteFunc(result, func(value string) bool {
		_, found := excludedSet[value]
		return found
	})
	return result
}
