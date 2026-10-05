package validate

func cloneRepairReviewResult(result Result) Result {
	result.SelectedChecks = append([]string(nil), result.SelectedChecks...)
	result.Checks = append([]CheckResult(nil), result.Checks...)
	for i := range result.Checks {
		check := &result.Checks[i]
		check.Notes = append([]string(nil), check.Notes...)
		check.Issues = cloneRepairIssues(check.Issues)
		check.fullIssues = cloneRepairIssues(check.fullIssues)
		check.allIssueKeys = append([]string(nil), check.allIssueKeys...)
		check.Fixes = cloneRepairActions(check.Fixes)
		check.Buckets = append([]MigrationBucket(nil), check.Buckets...)
	}
	if result.FixPlan != nil {
		plan := *result.FixPlan
		plan.IssueKeys = append([]string(nil), plan.IssueKeys...)
		plan.Actions = cloneRepairActions(plan.Actions)
		plan.Operations = cloneRepairOperations(plan.Operations)
		plan.Transactions = append([]RepairTransaction(nil), plan.Transactions...)
		for i := range plan.Transactions {
			transaction := &plan.Transactions[i]
			transaction.OperationIDs = append([]string(nil), transaction.OperationIDs...)
			transaction.AffectedPaths = append([]string(nil), transaction.AffectedPaths...)
			transaction.Checks = append([]string(nil), transaction.Checks...)
			transaction.Identities = append([]string(nil), transaction.Identities...)
			transaction.Conflicts = cloneRepairConflicts(transaction.Conflicts)
		}
		plan.FollowUps = append([]RepairFollowUp(nil), plan.FollowUps...)
		result.FixPlan = &plan
	}
	return result
}

func cloneRepairIssues(issues []Issue) []Issue {
	cloned := append([]Issue(nil), issues...)
	for i := range cloned {
		cloned[i].Data = append([]byte(nil), cloned[i].Data...)
		cloned[i].AffectedPaths = append([]string(nil), cloned[i].AffectedPaths...)
		cloned[i].AffectedNotePaths = append([]string(nil), cloned[i].AffectedNotePaths...)
		if cloned[i].Location != nil {
			location := *cloned[i].Location
			cloned[i].Location = &location
		}
		if cloned[i].Variant != nil {
			variant := *cloned[i].Variant
			cloned[i].Variant = &variant
		}
	}
	return cloned
}

func cloneRepairActions(actions []FixAction) []FixAction {
	cloned := append([]FixAction(nil), actions...)
	for i := range cloned {
		cloned[i].IssueKeys = append([]string(nil), cloned[i].IssueKeys...)
		cloned[i].OperationIDs = append([]string(nil), cloned[i].OperationIDs...)
		cloned[i].AffectedPaths = append([]string(nil), cloned[i].AffectedPaths...)
		cloned[i].CandidatePaths = append([]string(nil), cloned[i].CandidatePaths...)
		cloned[i].Edits = append([]FixEdit(nil), cloned[i].Edits...)
		for j := range cloned[i].Edits {
			cloned[i].Edits[j].Values = append([]string(nil), cloned[i].Edits[j].Values...)
		}
	}
	return cloned
}

func cloneRepairOperations(operations []RepairOperation) []RepairOperation {
	cloned := append([]RepairOperation(nil), operations...)
	for i := range cloned {
		operation := &cloned[i]
		operation.ActionIDs = append([]string(nil), operation.ActionIDs...)
		operation.IssueKeys = append([]string(nil), operation.IssueKeys...)
		operation.RequiredChecks = append([]string(nil), operation.RequiredChecks...)
		operation.Expected = append([]ExpectedText(nil), operation.Expected...)
		operation.Identities = append([]string(nil), operation.Identities...)
		operation.Content = append([]byte(nil), operation.Content...)
		operation.Lifecycle.ProtectedRanges = append([]LifecycleRange(nil), operation.Lifecycle.ProtectedRanges...)
		operation.LifecycleClaims = append([]LifecycleClaim(nil), operation.LifecycleClaims...)
		operation.PlanningConflicts = cloneRepairConflicts(operation.PlanningConflicts)
		if operation.DestinationVacancy != nil {
			vacancy := *operation.DestinationVacancy
			operation.DestinationVacancy = &vacancy
		}
	}
	return cloned
}

func cloneRepairConflicts(conflicts []RepairConflict) []RepairConflict {
	cloned := append([]RepairConflict(nil), conflicts...)
	for i := range cloned {
		cloned[i].OperationIDs = append([]string(nil), cloned[i].OperationIDs...)
	}
	return cloned
}
