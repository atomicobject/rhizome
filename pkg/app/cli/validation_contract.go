package actions

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/app/validationrun"
	"github.com/atomicobject/rhizome/pkg/validate"
)

// Validation contract aliases keep renderers and command shells source-stable
// while neutral orchestration lives below the CLI and projection adapters.
type ValidationRunRequest = validationrun.ValidationRunRequest
type ValidationSuiteRunner = validationrun.ValidationSuiteRunner
type ValidationCheckOutcome = validationrun.ValidationCheckOutcome
type ValidationResult = validationrun.ValidationResult

const (
	ValidationExitClean    = validationrun.ValidationExitClean
	ValidationExitFindings = validationrun.ValidationExitFindings
	ValidationExitFailure  = validationrun.ValidationExitFailure
)

// RunValidation delegates to the neutral validation orchestration contract.
func RunValidation(ctx context.Context, request ValidationRunRequest, run ValidationSuiteRunner) (ValidationResult, error) {
	return validationrun.RunValidation(ctx, request, run)
}

// BuildValidationResult delegates result normalization to the neutral layer.
func BuildValidationResult(selection validate.Selection, applicability []validate.CheckApplicabilityResult, suite validate.Result) (ValidationResult, error) {
	return validationrun.BuildValidationResult(selection, applicability, suite)
}
