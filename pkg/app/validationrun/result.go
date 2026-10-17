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
	validate.CheckApplicabilityResult
	RemediationCommand string `json:"remediationCommand,omitempty"`
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
	checks, outcomes, effective, blocked, err := validateComposition(selection, applicability, suite.Checks)
	if err != nil {
		return ValidationResult{}, err
	}
	suite.SelectedChecks = slices.Clone(effective)
	suite.Checks = checks
	if blocked {
		suite.OK = false
	}
	return AttachFixExecution(ValidationResult{
		Result: suite, Selector: selection.Selector, EffectiveChecks: effective, Outcomes: outcomes,
	}, suite.FixExecution), nil
}

// validateComposition enforces exact selection/outcome/result membership at the
// product boundary. Individual checks never decide which selected checks disappear.
func validateComposition(selection validate.Selection, applicability []validate.CheckApplicabilityResult, results []validate.CheckResult) ([]validate.CheckResult, []ValidationCheckOutcome, []string, bool, error) {
	descriptors := descriptorMap()
	fail := func(format string, args ...any) ([]validate.CheckResult, []ValidationCheckOutcome, []string, bool, error) {
		return nil, nil, nil, false, fmt.Errorf(format, args...)
	}
	selected := make(map[string]bool, len(selection.Checks))
	order := make([]string, 0, len(selection.Checks))
	for _, check := range selection.Checks {
		canonical, ok := validate.CanonicalCheck(check)
		if !ok {
			return fail("selection names unknown check %q", check)
		}
		if selected[canonical] {
			return fail("selection contains duplicate check %q", descriptors[canonical].CLIName)
		}
		selected[canonical] = true
		order = append(order, canonical)
	}
	outcomeByCheck := make(map[string]validate.CheckApplicabilityResult, len(applicability))
	for _, outcome := range applicability {
		canonical, ok := validate.CanonicalCheck(outcome.Check)
		if !ok {
			return fail("applicability outcome names unknown check %q", outcome.Check)
		}
		name := descriptors[canonical].CLIName
		if _, exists := outcomeByCheck[canonical]; exists {
			return fail("duplicate applicability outcome for check %q", name)
		}
		if !selected[canonical] {
			return fail("applicability outcome for unselected check %q", name)
		}
		switch outcome.Outcome {
		case validate.CheckOutcomeCompleted, validate.CheckOutcomeBlocked, validate.CheckOutcomeNotApplicable:
		default:
			return fail("applicability outcome for check %q has invalid state %q", name, outcome.Outcome)
		}
		outcomeByCheck[canonical] = outcome
	}
	resultByCheck := make(map[string]validate.CheckResult, len(results))
	for _, result := range results {
		canonical, ok := validate.CanonicalCheck(result.Name)
		if !ok {
			return fail("suite result names unknown check %q", result.Name)
		}
		name := descriptors[canonical].CLIName
		if _, exists := resultByCheck[canonical]; exists {
			return fail("duplicate suite result for check %q", name)
		}
		if !selected[canonical] {
			return fail("suite result for unselected check %q", name)
		}
		resultByCheck[canonical] = result
	}
	checks := make([]validate.CheckResult, 0, len(results))
	outcomes := make([]ValidationCheckOutcome, 0, len(order))
	effective := make([]string, 0, len(order))
	blocked := false
	for _, canonical := range order {
		descriptor := descriptors[canonical]
		outcome, exists := outcomeByCheck[canonical]
		if !exists {
			return fail("missing applicability outcome for selected check %q", descriptor.CLIName)
		}
		result, ran := resultByCheck[canonical]
		public := ValidationCheckOutcome{CheckApplicabilityResult: outcome}
		public.Check = descriptor.CLIName
		public.Evidence = slices.Clone(outcome.Evidence)
		if outcome.Outcome == validate.CheckOutcomeCompleted {
			if !ran {
				return fail("missing suite result for completed check %q", descriptor.CLIName)
			}
			if result.NotApplicable != "" {
				return fail("completed check %q returned a not-applicable result", descriptor.CLIName)
			}
			if result.IssueCount > 0 || len(result.Fixes) > 0 {
				public.RemediationCommand = descriptor.Remediation.Command
			}
			result.Name = descriptor.CLIName
			checks = append(checks, result)
		} else if ran {
			return fail("suite result for %s check %q", outcome.Outcome, descriptor.CLIName)
		}
		blocked = blocked || outcome.Outcome == validate.CheckOutcomeBlocked
		effective = append(effective, descriptor.CLIName)
		outcomes = append(outcomes, public)
	}
	return checks, outcomes, effective, blocked, nil
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
