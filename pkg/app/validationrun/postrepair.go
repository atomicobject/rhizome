package validationrun

import (
	"fmt"
	"github.com/atomicobject/rhizome/pkg/validate"
	"slices"
	"strings"
)

// RebuildAfterRepair replaces completed findings with the exact prepared
// postcheck while preserving the caller's selector and prerequisite evidence.
// Extra affected checks verify the transaction but do not change public
// selection; their remaining evidence stays on FixExecution.
func RebuildAfterRepair(planned ValidationResult, postcheck validate.Result, execution *validate.FixExecution) (ValidationResult, error) {
	checks := slices.Clone(planned.EffectiveChecks)
	selection := validate.Selection{Selector: planned.Selector, Checks: checks}
	postchecked := make(map[string]struct{}, len(postcheck.Checks))
	for _, check := range postcheck.Checks {
		canonical, ok := validate.CanonicalCheck(check.Name)
		if !ok {
			return ValidationResult{}, fmt.Errorf("post-repair result names unknown check %q", check.Name)
		}
		postchecked[canonical] = struct{}{}
	}
	recoveryCompleted := len(planned.RepairJournals) > 0 && len(postcheck.RepairJournals) == 0
	replanCommand := ""
	if execution != nil {
		replanCommand = strings.TrimSpace(execution.ReplanCommand)
	}

	prior := make(map[string]ValidationCheckOutcome, len(planned.Outcomes))
	completed := make(map[string]struct{}, len(planned.Outcomes))
	for _, outcome := range planned.Outcomes {
		canonical, ok := validate.CanonicalCheck(outcome.Check)
		if ok {
			if recoveryCompleted && pendingRepairJournalOutcome(outcome) {
				if _, ran := postchecked[canonical]; ran {
					outcome.Outcome = validate.CheckOutcomeCompleted
					outcome.Summary = ""
					outcome.PreparationCommand = ""
					outcome.RemediationCommand = ""
					outcome.Evidence = nil
				} else {
					if replanCommand == "" {
						return ValidationResult{}, fmt.Errorf("recovered repair result omitted its replan command")
					}
					outcome.Summary = "repair journal recovery completed; rerun validation from a fresh plan"
					outcome.PreparationCommand = replanCommand
					outcome.RemediationCommand = ""
					outcome.Evidence = []validate.ApplicabilityEvidence{{
						Code:    "repair_replan_required",
						Message: "Recovery completed, but this selected check was not executed; replan to validate the current vault state.",
					}}
				}
			}
			prior[canonical] = outcome
			if outcome.Outcome == validate.CheckOutcomeCompleted {
				completed[canonical] = struct{}{}
			}
		}
	}
	applicability := make([]validate.CheckApplicabilityResult, 0, len(checks))
	for _, check := range checks {
		canonical, ok := validate.CanonicalCheck(check)
		if !ok {
			return ValidationResult{}, fmt.Errorf("post-repair check names unknown check %q", check)
		}
		if outcome, exists := prior[canonical]; exists {
			applicability = append(applicability, validate.CheckApplicabilityResult{
				Check:              canonical,
				Outcome:            outcome.Outcome,
				Summary:            outcome.Summary,
				PreparationCommand: outcome.PreparationCommand,
				Evidence:           slices.Clone(outcome.Evidence),
			})
			continue
		}
		applicability = append(applicability, validate.CheckApplicabilityResult{
			Check:   canonical,
			Outcome: validate.CheckOutcomeCompleted,
		})
	}
	filteredChecks := make([]validate.CheckResult, 0, len(postcheck.Checks))
	for _, check := range postcheck.Checks {
		canonical, _ := validate.CanonicalCheck(check.Name)
		// The names were validated while deriving recovery outcome state above.
		if _, keep := completed[canonical]; keep {
			filteredChecks = append(filteredChecks, check)
		}
	}
	postcheck.Checks = filteredChecks
	postcheck.SelectedChecks = slices.Clone(checks)
	postcheck.SummarizeChecks()
	scopedPlan, scopeErr := scopeRepairPlanToChecks(postcheck.FixPlan, completed)
	if scopeErr != nil {
		return ValidationResult{}, scopeErr
	}
	postcheck.FixPlan = scopedPlan
	postcheck.FixExecution = execution
	postcheck.NextActions = validate.BuildNextActions(postcheck)
	if postcheck.IdentifierReconciliation == nil {
		postcheck.IdentifierReconciliation = planned.IdentifierReconciliation
	}
	rebuilt, err := BuildValidationResult(selection, applicability, postcheck)
	if err != nil {
		return ValidationResult{}, err
	}
	rebuilt.OK = rebuilt.ExitCode() == ValidationExitClean
	return rebuilt, nil
}

// scopeRepairPlanToChecks removes repair guidance produced by transaction-
// required postchecks that were not part of the caller's public selection.
// Executable operations may be dropped only as complete action-owned units;
// mixed selected/unselected ownership indicates an invalid plan boundary.
func scopeRepairPlanToChecks(plan *validate.FixPlan, allowed map[string]struct{}) (*validate.FixPlan, error) {
	if plan == nil {
		return nil, nil
	}
	retainedActions := make([]validate.FixAction, 0, len(plan.Actions))
	retainedIDs := make(map[string]struct{}, len(plan.Actions))
	removed := false
	for _, action := range plan.Actions {
		check := strings.TrimSpace(action.Check)
		if check != "" {
			canonical, ok := validate.CanonicalCheck(check)
			if !ok {
				return nil, fmt.Errorf("post-repair plan action %q names unknown check %q", action.ID, action.Check)
			}
			if _, keep := allowed[canonical]; !keep {
				removed = true
				continue
			}
		}
		retainedActions = append(retainedActions, action)
		retainedIDs[action.ID] = struct{}{}
	}
	if !removed {
		return plan, nil
	}

	retainedOperations := make([]validate.RepairOperation, 0, len(plan.Operations))
	for _, operation := range plan.Operations {
		retainedCount := 0
		for _, actionID := range operation.ActionIDs {
			if _, keep := retainedIDs[actionID]; keep {
				retainedCount++
			}
		}
		switch {
		case retainedCount == 0:
			continue
		case retainedCount != len(operation.ActionIDs):
			return nil, fmt.Errorf("post-repair operation %q mixes selected and unselected action ownership", operation.ID)
		default:
			retainedOperations = append(retainedOperations, operation)
		}
	}
	if len(retainedActions) == 0 && len(retainedOperations) == 0 {
		return nil, nil
	}

	scoped := *plan
	scoped.Actions = retainedActions
	scoped.Operations = retainedOperations
	scoped.Transactions = nil
	scoped.IssueKeys = nil
	scoped.TotalCount = len(retainedActions)
	scoped.SafeCount = 0
	scoped.ConfirmationCount = 0
	scoped.AgentCount = 0
	for _, action := range retainedActions {
		scoped.IssueKeys = append(scoped.IssueKeys, action.IssueKeys...)
		switch action.Safety {
		case validate.FixSafetySafe:
			scoped.SafeCount++
		case validate.FixSafetyConfirm:
			scoped.ConfirmationCount++
		case validate.FixSafetyAgent:
			scoped.AgentCount++
		}
	}
	if scoped.PreparationRequired {
		fingerprint, err := validate.RepairPlanFingerprint(scoped)
		if err != nil {
			return nil, fmt.Errorf("scope post-repair action summary to selected checks: %w", err)
		}
		scoped.Fingerprint = fingerprint
		return &scoped, nil
	}
	finalized, err := validate.FinalizeRepairPlan(scoped)
	if err != nil {
		return nil, fmt.Errorf("scope post-repair plan to selected checks: %w", err)
	}
	return &finalized, nil
}

func pendingRepairJournalOutcome(outcome ValidationCheckOutcome) bool {
	if outcome.Outcome != validate.CheckOutcomeBlocked {
		return false
	}
	for _, evidence := range outcome.Evidence {
		if evidence.Code == "pending_repair_journal" {
			return true
		}
	}
	return false
}
