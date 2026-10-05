package validationrun

import (
	"fmt"
	"slices"
	"strings"

	"github.com/atomicobject/rhizome/pkg/validate"
)

// Validation process exit codes are shared by human, agent, and CI shells.
const (
	ValidationExitClean    = 0
	ValidationExitFindings = 1
	ValidationExitFailure  = 2
)

// ValidationCheckOutcome records applicability and prerequisite resolution for
// one effective public check. Commands are exact, executable follow-ups rather
// than prose that a renderer must interpret.
type ValidationCheckOutcome struct {
	Check              string                           `json:"check"`
	Outcome            validate.CheckOutcome            `json:"outcome"`
	Summary            string                           `json:"summary,omitempty"`
	PreparationCommand string                           `json:"preparationCommand,omitempty"`
	RemediationCommand string                           `json:"remediationCommand,omitempty"`
	Evidence           []validate.ApplicabilityEvidence `json:"evidence,omitempty"`
}

// ValidationResult is the shared structured result for local, agent, and CI
// validation. Embedding the existing suite result preserves its JSON fields;
// selector, effective checks, and prerequisite outcomes are additive.
type ValidationResult struct {
	validate.Result
	Selector        string                   `json:"selector"`
	EffectiveChecks []string                 `json:"effectiveChecks"`
	Outcomes        []ValidationCheckOutcome `json:"outcomes"`
	ExecutionError  string                   `json:"executionError,omitempty"`
}

// BuildValidationResult combines selection, prerequisite outcomes, and the
// existing suite result without executing checks or preparing projections.
func BuildValidationResult(
	selection validate.Selection,
	applicability []validate.CheckApplicabilityResult,
	suite validate.Result,
) (ValidationResult, error) {
	descriptors := make(map[string]validate.CheckDescriptor)
	for _, descriptor := range validate.CheckDescriptors() {
		descriptors[descriptor.Name] = descriptor
	}

	outcomesByCheck := make(map[string]validate.CheckApplicabilityResult, len(applicability))
	for _, outcome := range applicability {
		canonical, ok := validate.CanonicalCheck(outcome.Check)
		if !ok {
			return ValidationResult{}, fmt.Errorf("applicability outcome names unknown check %q", outcome.Check)
		}
		if _, duplicate := outcomesByCheck[canonical]; duplicate {
			return ValidationResult{}, fmt.Errorf("duplicate applicability outcome for check %q", descriptors[canonical].CLIName)
		}
		switch outcome.Outcome {
		case validate.CheckOutcomeCompleted, validate.CheckOutcomeNotApplicable, validate.CheckOutcomeBlocked:
		default:
			return ValidationResult{}, fmt.Errorf("applicability outcome for check %q has invalid state %q", descriptors[canonical].CLIName, outcome.Outcome)
		}
		outcomesByCheck[canonical] = outcome
	}

	effectiveChecks := make([]string, 0, len(selection.Checks))
	outcomes := make([]ValidationCheckOutcome, 0, len(selection.Checks))
	outcomeIndexes := make(map[string]int, len(selection.Checks))
	selectedChecks := make([]string, 0, len(selection.Checks))
	selectedOutcomes := make(map[string]validate.CheckApplicabilityResult, len(selection.Checks))
	selected := make(map[string]struct{}, len(selection.Checks))
	hasBlockedOutcome := false
	for _, selectedCheck := range selection.Checks {
		canonical, ok := validate.CanonicalCheck(selectedCheck)
		if !ok {
			return ValidationResult{}, fmt.Errorf("selection names unknown check %q", selectedCheck)
		}
		descriptor := descriptors[canonical]
		if _, duplicate := selected[canonical]; duplicate {
			return ValidationResult{}, fmt.Errorf("selection contains duplicate check %q", descriptor.CLIName)
		}
		selected[canonical] = struct{}{}

		applicabilityResult, ok := outcomesByCheck[canonical]
		if !ok {
			return ValidationResult{}, fmt.Errorf("missing applicability outcome for selected check %q", descriptor.CLIName)
		}
		delete(outcomesByCheck, canonical)

		selectedChecks = append(selectedChecks, canonical)
		selectedOutcomes[canonical] = applicabilityResult
		if applicabilityResult.Outcome == validate.CheckOutcomeBlocked {
			hasBlockedOutcome = true
		}
		effectiveChecks = append(effectiveChecks, descriptor.CLIName)
		outcomeIndexes[canonical] = len(outcomes)
		outcomes = append(outcomes, ValidationCheckOutcome{
			Check:              descriptor.CLIName,
			Outcome:            applicabilityResult.Outcome,
			Summary:            applicabilityResult.Summary,
			PreparationCommand: applicabilityResult.PreparationCommand,
			Evidence:           slices.Clone(applicabilityResult.Evidence),
		})
	}
	for _, outcome := range applicability {
		canonical, _ := validate.CanonicalCheck(outcome.Check)
		if _, unexpected := outcomesByCheck[canonical]; unexpected {
			return ValidationResult{}, fmt.Errorf("applicability outcome for unselected check %q", descriptors[canonical].CLIName)
		}
	}

	suiteResultsByCheck := make(map[string]validate.CheckResult, len(suite.Checks))
	for _, checkResult := range suite.Checks {
		canonical, ok := validate.CanonicalCheck(checkResult.Name)
		if !ok {
			return ValidationResult{}, fmt.Errorf("suite result names unknown check %q", checkResult.Name)
		}
		if _, duplicate := suiteResultsByCheck[canonical]; duplicate {
			return ValidationResult{}, fmt.Errorf("duplicate suite result for check %q", descriptors[canonical].CLIName)
		}
		suiteResultsByCheck[canonical] = checkResult
	}

	normalizedChecks := make([]validate.CheckResult, 0, len(suite.Checks))
	for _, canonical := range selectedChecks {
		outcome := selectedOutcomes[canonical]
		checkResult, exists := suiteResultsByCheck[canonical]
		if outcome.Outcome == validate.CheckOutcomeCompleted {
			if !exists {
				return ValidationResult{}, fmt.Errorf("missing suite result for completed check %q", descriptors[canonical].CLIName)
			}
			if checkResult.Skipped {
				return ValidationResult{}, fmt.Errorf("completed check %q returned a legacy skipped result", descriptors[canonical].CLIName)
			}
			if checkResult.IssueCount > 0 || len(checkResult.Fixes) > 0 {
				outcomes[outcomeIndexes[canonical]].RemediationCommand = descriptors[canonical].Remediation.Command
			}
			checkResult.Name = descriptors[canonical].CLIName
			normalizedChecks = append(normalizedChecks, checkResult)
			delete(suiteResultsByCheck, canonical)
			continue
		}
		if exists {
			return ValidationResult{}, fmt.Errorf("suite result for %s check %q", outcome.Outcome, descriptors[canonical].CLIName)
		}
	}
	for _, checkResult := range suite.Checks {
		canonical, _ := validate.CanonicalCheck(checkResult.Name)
		if _, unexpected := suiteResultsByCheck[canonical]; unexpected {
			return ValidationResult{}, fmt.Errorf("suite result for unselected check %q", descriptors[canonical].CLIName)
		}
	}

	suite.SelectedChecks = slices.Clone(effectiveChecks)
	suite.Checks = normalizedChecks
	if hasBlockedOutcome {
		suite.OK = false
	}

	result := ValidationResult{
		Result:          suite,
		Selector:        selection.Selector,
		EffectiveChecks: effectiveChecks,
		Outcomes:        outcomes,
	}
	return AttachFixExecution(result, suite.FixExecution), nil
}

// AttachFixExecution returns a result with apply evidence attached. Identifier
// timings are copied into a fresh reconciliation envelope so the reviewed
// planning result remains immutable when apply happens later.
func AttachFixExecution(result ValidationResult, execution *validate.FixExecution) ValidationResult {
	result.FixExecution = execution
	if result.IdentifierReconciliation == nil || execution == nil || execution.IdentifierReconciliationTimings == nil {
		return result
	}
	reconciliation := *result.IdentifierReconciliation
	timings := execution.IdentifierReconciliationTimings
	reconciliation.Diagnostics.Timings.Apply = timings.Apply
	reconciliation.Diagnostics.Timings.PostValidation = timings.PostValidation
	result.IdentifierReconciliation = &reconciliation
	return result
}

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
	issueCount := 0
	errorCount := 0
	for _, check := range postcheck.Checks {
		canonical, _ := validate.CanonicalCheck(check.Name)
		// The names were validated while deriving recovery outcome state above.
		if _, keep := completed[canonical]; keep {
			filteredChecks = append(filteredChecks, check)
			issueCount += check.IssueCount
			if strings.TrimSpace(check.Error) != "" {
				errorCount++
			}
		}
	}
	postcheck.Checks = filteredChecks
	postcheck.SelectedChecks = slices.Clone(checks)
	postcheck.IssueCount = issueCount
	postcheck.ErrorCount = errorCount
	postcheck.OK = issueCount == 0 && errorCount == 0
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

// ExitCode applies the frozen clean/findings/failure contract. Applicable
// blocked prerequisites and execution errors take precedence over findings.
func (result ValidationResult) ExitCode() int {
	if result.ErrorCount > 0 || strings.TrimSpace(result.ExecutionError) != "" {
		return ValidationExitFailure
	}
	if result.FixExecution != nil && len(result.FixExecution.Failed) > 0 {
		return ValidationExitFailure
	}
	for _, outcome := range result.Outcomes {
		if outcome.Outcome == validate.CheckOutcomeBlocked {
			return ValidationExitFailure
		}
	}
	for _, check := range result.Checks {
		if strings.TrimSpace(check.Error) != "" {
			return ValidationExitFailure
		}
	}
	if result.FixExecution != nil && result.FixExecution.RemainingFindings > 0 {
		return ValidationExitFindings
	}
	if result.IssueCount > 0 {
		return ValidationExitFindings
	}
	for _, check := range result.Checks {
		if check.IssueCount > 0 {
			return ValidationExitFindings
		}
	}
	return ValidationExitClean
}
