package validationrun

import (
	"context"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/validate"
)

// ValidationRunRequest is the surface-independent input for one read-only
// validation invocation. Storage/projection preparation stays behind probes;
// the suite runner receives only checks whose prerequisites completed.
type ValidationRunRequest struct {
	Selectors    []string
	ApplyCommand string
	Config       validate.SuiteConfig
	Surface      validate.ExecutionSurface
	Features     validate.VaultFeatureFacts
	Probes       validate.PrerequisiteProbes
}

// ValidationSuiteRunner executes one ordered set of applicable checks.
// Implementations must remain read-only; repair plan/apply has a separate path.
type ValidationSuiteRunner func(context.Context, []string) (validate.Result, error)

// RunValidation composes selection, resolves applicability/prerequisites once
// per domain, runs completed checks, and builds the shared structured result.
func RunValidation(ctx context.Context, request ValidationRunRequest, run ValidationSuiteRunner) (ValidationResult, error) {
	selection, err := validate.ResolveSelection(request.Selectors, request.Config)
	if err != nil {
		return ValidationResult{}, err
	}

	if request.Surface == "" {
		request.Surface = validate.SurfaceLocal
	}
	probes := newCachedPrerequisiteProbes(request.Probes)
	descriptors := descriptorMap()
	outcomes := make([]validate.CheckApplicabilityResult, 0, len(selection.Checks))
	completed := make([]string, 0, len(selection.Checks))
	for _, check := range selection.Checks {
		descriptor, ok := descriptors[check]
		if !ok {
			return ValidationResult{}, fmt.Errorf("selected validation check %q has no descriptor", check)
		}
		outcome := validate.ResolveCheckApplicability(ctx, descriptor, request.Surface, request.Features, probes)
		outcomes = append(outcomes, outcome)
		if outcome.Outcome == validate.CheckOutcomeCompleted {
			completed = append(completed, check)
		}
	}

	suite := validate.Result{OK: true}
	if len(completed) > 0 {
		if run == nil {
			return ValidationResult{}, fmt.Errorf("validation suite runner is required for completed checks")
		}
		suite, err = run(ctx, completed)
		if err != nil {
			return ValidationResult{}, err
		}
	}
	outcomes, suite, err = promoteSoftSkippedChecks(outcomes, suite)
	if err != nil {
		return ValidationResult{}, err
	}
	if len(suite.RepairJournals) > 0 {
		outcomes, suite, err = blockedRepairJournalComposition(request, outcomes, suite)
		if err != nil {
			return ValidationResult{}, err
		}
		return BuildValidationResult(selection, outcomes, suite)
	}
	if request.ApplyCommand != "" {
		suite.ApplyCommand = request.ApplyCommand
	}
	suite.NextActions = validate.BuildNextActions(suite)
	return BuildValidationResult(selection, outcomes, suite)
}

// promoteSoftSkippedChecks converts domain-level absence discovered only after
// prepared execution into the product contract's explicit not-applicable
// outcome. Prerequisite and feature checks should resolve before execution;
// this narrow bridge is for facts that require the prepared schema/runtime
// itself (for example, an ontology with no @identifier fields).
func promoteSoftSkippedChecks(
	outcomes []validate.CheckApplicabilityResult,
	suite validate.Result,
) ([]validate.CheckApplicabilityResult, validate.Result, error) {
	outcomeIndex := make(map[string]int, len(outcomes))
	for index, outcome := range outcomes {
		canonical, ok := validate.CanonicalCheck(outcome.Check)
		if !ok {
			return nil, validate.Result{}, fmt.Errorf("applicability outcome names unknown check %q", outcome.Check)
		}
		outcomeIndex[canonical] = index
	}

	filtered := make([]validate.CheckResult, 0, len(suite.Checks))
	for _, check := range suite.Checks {
		if !check.Skipped {
			filtered = append(filtered, check)
			continue
		}
		canonical, ok := validate.CanonicalCheck(check.Name)
		if !ok {
			return nil, validate.Result{}, fmt.Errorf("soft-skipped result names unknown check %q", check.Name)
		}
		index, exists := outcomeIndex[canonical]
		if !exists || outcomes[index].Outcome != validate.CheckOutcomeCompleted {
			return nil, validate.Result{}, fmt.Errorf("soft-skipped check %q has no completed applicability outcome", check.Name)
		}
		if check.IssueCount != 0 || strings.TrimSpace(check.Error) != "" || len(check.Issues) != 0 || len(check.Fixes) != 0 {
			return nil, validate.Result{}, fmt.Errorf("soft-skipped check %q also returned findings, errors, or repairs", check.Name)
		}
		outcomes[index].Outcome = validate.CheckOutcomeNotApplicable
		outcomes[index].Summary = strings.TrimSpace(check.Summary)
		if outcomes[index].Summary == "" {
			outcomes[index].Summary = "check is not applicable to this vault"
		}
	}

	issues := 0
	errors := 0
	for _, check := range filtered {
		issues += check.IssueCount
		if strings.TrimSpace(check.Error) != "" {
			errors++
		}
	}
	suite.Checks = filtered
	suite.IssueCount = issues
	suite.ErrorCount = errors
	suite.OK = issues == 0 && errors == 0 && len(suite.RepairJournals) == 0
	return outcomes, suite, nil
}

func blockedRepairJournalComposition(
	request ValidationRunRequest,
	outcomes []validate.CheckApplicabilityResult,
	suite validate.Result,
) ([]validate.CheckApplicabilityResult, validate.Result, error) {
	recoveryCommand := request.ApplyCommand
	if recoveryCommand == "" {
		recoveryCommand = suite.ApplyCommand
	}
	if suite.NextActions == nil {
		return nil, validate.Result{}, fmt.Errorf("pending repair journal result omitted recovery guidance")
	}
	next := *suite.NextActions
	next.Actions = append([]validate.NextAction(nil), suite.NextActions.Actions...)
	recoveryIndex := -1
	for index, action := range next.Actions {
		if action.Category != "repair_recovery" {
			continue
		}
		recoveryIndex = index
		if recoveryCommand == "" {
			recoveryCommand = action.Command
		}
		break
	}
	if recoveryIndex < 0 || recoveryCommand == "" {
		return nil, validate.Result{}, fmt.Errorf("pending repair journal result omitted its recovery command")
	}
	next.Actions[recoveryIndex].Command = recoveryCommand
	suite.ApplyCommand = recoveryCommand
	suite.NextActions = &next

	blocked := append([]validate.CheckApplicabilityResult(nil), outcomes...)
	for index := range blocked {
		blocked[index].Outcome = validate.CheckOutcomeBlocked
		blocked[index].Summary = "pending repair journal must be recovered before validation can inspect or refresh projections"
		blocked[index].PreparationCommand = recoveryCommand
		blocked[index].Evidence = []validate.ApplicabilityEvidence{{
			Code:    "pending_repair_journal",
			Message: "Interrupted repair evidence is present; recover it before running validation.",
		}}
	}
	return blocked, suite, nil
}

func descriptorMap() map[string]validate.CheckDescriptor {
	descriptors := validate.CheckDescriptors()
	byName := make(map[string]validate.CheckDescriptor, len(descriptors))
	for _, descriptor := range descriptors {
		byName[descriptor.Name] = descriptor
	}
	return byName
}

type cachedPrerequisiteProbes struct {
	upstream validate.PrerequisiteProbes

	projection map[validate.ProjectionDomain]cachedProjectionResult
	code       cachedCodeResult
}

type cachedProjectionResult struct {
	snapshot validate.AutoManagedProjectionSnapshot
	err      error
}

type cachedCodeResult struct {
	loaded   bool
	snapshot validate.CodeIndexSnapshot
	err      error
}

func newCachedPrerequisiteProbes(upstream validate.PrerequisiteProbes) validate.PrerequisiteProbes {
	cache := &cachedPrerequisiteProbes{
		upstream:   upstream,
		projection: make(map[validate.ProjectionDomain]cachedProjectionResult),
	}
	return validate.PrerequisiteProbes{Projection: cache, CodeIndex: cache}
}

func (p *cachedPrerequisiteProbes) AutoManagedProjection(ctx context.Context, descriptor validate.CheckDescriptor, domain validate.ProjectionDomain) (validate.AutoManagedProjectionSnapshot, error) {
	if cached, ok := p.projection[domain]; ok {
		return cached.snapshot, cached.err
	}
	if p.upstream.Projection == nil {
		return validate.AutoManagedProjectionSnapshot{}, fmt.Errorf("%s prerequisite probe is not configured", domain)
	}
	snapshot, err := p.upstream.Projection.AutoManagedProjection(ctx, descriptor, domain)
	p.projection[domain] = cachedProjectionResult{snapshot: snapshot, err: err}
	return snapshot, err
}

func (p *cachedPrerequisiteProbes) CodeIndexSnapshot(ctx context.Context) (validate.CodeIndexSnapshot, error) {
	if p.code.loaded {
		return p.code.snapshot, p.code.err
	}
	p.code.loaded = true
	if p.upstream.CodeIndex == nil {
		p.code.err = fmt.Errorf("persisted code-index prerequisite probe is not configured")
		return validate.CodeIndexSnapshot{}, p.code.err
	}
	p.code.snapshot, p.code.err = p.upstream.CodeIndex.CodeIndexSnapshot(ctx)
	return p.code.snapshot, p.code.err
}
