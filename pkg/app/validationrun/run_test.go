package validationrun

import (
	"context"
	"errors"
	"testing"

	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/stretchr/testify/require"
)

func TestRunValidation_ComposesSelectionAndRunsOnlyCompletedChecks(t *testing.T) {
	t.Parallel()

	projection := &validationProjectionProbe{available: true}
	runner := &validationRunnerSpy{}
	result, err := RunValidation(context.Background(), ValidationRunRequest{
		Selectors: []string{"default"},
		Config: validate.SuiteConfig{Default: validate.SuiteOverlay{
			Add:  []string{"views"},
			Skip: []string{"broken-links"},
		}},
		Surface: validate.SurfaceLocal,
		Features: validate.VaultFeatureFacts{
			OntologyConfigured: true,
			ViewsConfigured:    false,
		},
		Probes: validate.PrerequisiteProbes{Projection: projection},
	}, runner.Run)

	require.NoError(t, err)
	require.Equal(t, []string{validate.CheckOntology, validate.CheckIdentifiers}, runner.checks)
	require.Equal(t, []string{"ontology", "identifiers", "views"}, result.EffectiveChecks)
	require.Equal(t, []validate.CheckOutcome{
		validate.CheckOutcomeCompleted,
		validate.CheckOutcomeCompleted,
		validate.CheckOutcomeNotApplicable,
	}, validationOutcomeStates(result))
	require.Equal(t, ValidationExitClean, result.ExitCode())
	require.Equal(t, map[validate.ProjectionDomain]int{
		validate.ProjectionValidation: 1,
		validate.ProjectionOntology:   1,
	}, projection.calls)
}

func TestRunValidationCarriesExactApplyCommandIntoNextActions(t *testing.T) {
	t.Parallel()

	result, err := RunValidation(context.Background(), ValidationRunRequest{
		Selectors:    []string{"broken-links"},
		ApplyCommand: "rzm validate fix broken-links --apply --vault docs --scope-note 'A B.md'",
		Surface:      validate.SurfaceLocal,
		Probes:       validate.PrerequisiteProbes{Projection: &validationProjectionProbe{available: true}},
	}, func(context.Context, []string) (validate.Result, error) {
		return validate.Result{
			IssueCount: 1,
			Checks: []validate.CheckResult{{
				Name: validate.CheckBrokenLinks, IssueCount: 1,
			}},
			FixPlan: &validate.FixPlan{
				SafeCount: 1,
				Actions:   []validate.FixAction{{Safety: validate.FixSafetySafe, InstanceCount: 1}},
			},
		}, nil
	})

	require.NoError(t, err)
	require.Equal(t, "rzm validate fix broken-links --apply --vault docs --scope-note 'A B.md'", result.NextActions.SafeAutoFixCommand)
}

func TestRunValidationPromotesDomainSoftSkipToNotApplicableOutcome(t *testing.T) {
	t.Parallel()

	result, err := RunValidation(context.Background(), ValidationRunRequest{
		Selectors: []string{"identifiers"},
		Surface:   validate.SurfaceLocal,
		Features:  validate.VaultFeatureFacts{OntologyConfigured: true},
		Probes:    validate.PrerequisiteProbes{Projection: &validationProjectionProbe{available: true}},
	}, func(context.Context, []string) (validate.Result, error) {
		return validate.Result{
			OK: true,
			Checks: []validate.CheckResult{{
				Name:    validate.CheckIdentifiers,
				OK:      true,
				Skipped: true,
				Summary: "no @identifier fields",
			}},
		}, nil
	})

	require.NoError(t, err)
	require.Equal(t, []string{"identifiers"}, result.EffectiveChecks)
	require.Empty(t, result.Checks)
	require.Len(t, result.Outcomes, 1)
	require.Equal(t, validate.CheckOutcomeNotApplicable, result.Outcomes[0].Outcome)
	require.Equal(t, "no @identifier fields", result.Outcomes[0].Summary)
	require.Equal(t, ValidationExitClean, result.ExitCode())
}

func TestRunValidationRejectsSoftSkipThatAlsoCarriesFindings(t *testing.T) {
	t.Parallel()

	_, err := RunValidation(context.Background(), ValidationRunRequest{
		Selectors: []string{"identifiers"},
		Surface:   validate.SurfaceLocal,
		Features:  validate.VaultFeatureFacts{OntologyConfigured: true},
		Probes:    validate.PrerequisiteProbes{Projection: &validationProjectionProbe{available: true}},
	}, func(context.Context, []string) (validate.Result, error) {
		return validate.Result{Checks: []validate.CheckResult{{
			Name:       validate.CheckIdentifiers,
			Skipped:    true,
			IssueCount: 1,
		}}}, nil
	})

	require.ErrorContains(t, err, `soft-skipped check "identifiers" also returned findings`)
}

func TestRunValidationPreservesSuiteApplyCommandWhenRequestOmitsIt(t *testing.T) {
	t.Parallel()

	result, err := RunValidation(context.Background(), ValidationRunRequest{
		Selectors: []string{"broken-links"},
		Surface:   validate.SurfaceLocal,
		Probes:    validate.PrerequisiteProbes{Projection: &validationProjectionProbe{available: true}},
	}, func(context.Context, []string) (validate.Result, error) {
		return validate.Result{
			IssueCount:   1,
			ApplyCommand: "rzm validate fix broken-links --apply --vault suite-owned",
			Checks:       []validate.CheckResult{{Name: validate.CheckBrokenLinks, IssueCount: 1}},
			FixPlan: &validate.FixPlan{
				SafeCount: 1,
				Actions:   []validate.FixAction{{Safety: validate.FixSafetySafe, InstanceCount: 1}},
			},
		}, nil
	})

	require.NoError(t, err)
	require.Equal(t, "rzm validate fix broken-links --apply --vault suite-owned", result.NextActions.SafeAutoFixCommand)
}

func TestRunValidationConvertsLateRepairJournalIntoBlockedOutcomes(t *testing.T) {
	t.Parallel()

	exactCommand := "rzm validate fix ontology --apply --vault docs --scope-note 'A B.md'"
	result, err := RunValidation(context.Background(), ValidationRunRequest{
		Selectors:    []string{"ontology"},
		ApplyCommand: exactCommand,
		Surface:      validate.SurfaceLocal,
		Features:     validate.VaultFeatureFacts{OntologyConfigured: true},
		Probes:       validate.PrerequisiteProbes{Projection: &validationProjectionProbe{available: true}},
	}, func(context.Context, []string) (validate.Result, error) {
		return validate.Result{
			OK:             false,
			ErrorCount:     1,
			RepairJournals: []validate.RepairJournalEvidence{{TransactionID: "late-journal"}},
			NextActions: &validate.NextActions{Actions: []validate.NextAction{{
				Category: "repair_recovery", Command: "legacy fallback",
			}}},
		}, nil
	})

	require.NoError(t, err)
	require.Empty(t, result.Checks)
	require.Equal(t, validate.CheckOutcomeBlocked, result.Outcomes[0].Outcome)
	require.Equal(t, exactCommand, result.Outcomes[0].PreparationCommand)
	require.Equal(t, exactCommand, result.NextActions.Actions[0].Command)
}

func TestRunValidation_ExplicitCheckBypassesInvalidConfiguredSuites(t *testing.T) {
	t.Parallel()

	projection := &validationProjectionProbe{available: true}
	runner := &validationRunnerSpy{}
	result, err := RunValidation(context.Background(), ValidationRunRequest{
		Selectors: []string{"broken-links"},
		Config: validate.SuiteConfig{
			Default: validate.SuiteOverlay{Add: []string{"mystery"}, Skip: []string{"mystery"}},
			All:     validate.SuiteOverlay{Add: []string{"also-mystery"}},
		},
		Surface: validate.SurfaceLocal,
		Probes:  validate.PrerequisiteProbes{Projection: projection},
	}, runner.Run)

	require.NoError(t, err)
	require.Equal(t, []string{validate.CheckBrokenLinks}, runner.checks)
	require.Equal(t, "broken-links", result.Selector)
}

func TestRunValidation_ConfigurableSelectorsValidateBothConfiguredSuites(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		selectors []string
		config    validate.SuiteConfig
	}{
		{name: "omitted", config: validate.SuiteConfig{All: validate.SuiteOverlay{Add: []string{"mystery"}}}},
		{name: "default", selectors: []string{"default"}, config: validate.SuiteConfig{All: validate.SuiteOverlay{Add: []string{"mystery"}}}},
		{name: "all", selectors: []string{"all"}, config: validate.SuiteConfig{Default: validate.SuiteOverlay{Add: []string{"mystery"}}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := RunValidation(context.Background(), ValidationRunRequest{
				Selectors: tt.selectors,
				Config:    tt.config,
			}, nil)
			require.ErrorContains(t, err, `unknown check "mystery"`)
		})
	}
}

func TestRunValidation_AuditBypassesConfiguredSuites(t *testing.T) {
	t.Parallel()

	runner := &validationRunnerSpy{}
	result, err := RunValidation(context.Background(), ValidationRunRequest{
		Selectors: []string{"audit"},
		Config: validate.SuiteConfig{
			Default: validate.SuiteOverlay{Add: []string{"mystery"}},
			All:     validate.SuiteOverlay{Skip: []string{"orphan-block-ids"}},
		},
		Features: validate.VaultFeatureFacts{OntologyConfigured: true, EffortsPresent: true},
		Probes:   validate.PrerequisiteProbes{Projection: &validationProjectionProbe{available: true}},
	}, runner.Run)

	require.NoError(t, err)
	require.Equal(t, "audit", result.Selector)
	require.Equal(t, []string{
		validate.CheckFrozenScopeDrift,
		validate.CheckFragileExternal,
		validate.CheckOrphanBlockIDs,
		validate.CheckPlaceholderLinks,
	}, runner.checks)
}

func TestRunValidation_RejectsLiteralEmptyArraySelector(t *testing.T) {
	t.Parallel()

	_, err := RunValidation(context.Background(), ValidationRunRequest{Selectors: []string{"[]"}}, nil)
	require.ErrorContains(t, err, `unknown validation selector "[]"`)
}

func TestRunValidation_ScratchCIBlocksCodeWithoutRunningIt(t *testing.T) {
	t.Parallel()

	projection := &validationProjectionProbe{available: true}
	runner := &validationRunnerSpy{}
	result, err := RunValidation(context.Background(), ValidationRunRequest{
		Selectors: []string{"code-anchors"},
		Surface:   validate.SurfaceCI,
		Features:  validate.VaultFeatureFacts{CodeConfigured: true, CodeAnchorRootsConfigured: true},
		Probes:    validate.PrerequisiteProbes{Projection: projection},
	}, runner.Run)

	require.NoError(t, err)
	require.Empty(t, runner.checks)
	require.Equal(t, ValidationExitFailure, result.ExitCode())
	require.False(t, result.OK)
	require.Equal(t, validate.CheckOutcomeBlocked, result.Outcomes[0].Outcome)
	require.Equal(t, "rzm validate code-anchors", result.Outcomes[0].PreparationCommand)
	require.Contains(t, result.Outcomes[0].Summary, "unavailable in isolated scratch CI")
	require.Empty(t, projection.calls)
}

func TestRunValidation_PropagatesSelectionAndRunnerFailures(t *testing.T) {
	t.Parallel()

	_, err := RunValidation(context.Background(), ValidationRunRequest{
		Selectors: []string{"default", "ontology"},
	}, nil)
	require.ErrorContains(t, err, "exactly one validation selector")

	runnerErr := errors.New("projection transaction failed")
	_, err = RunValidation(context.Background(), ValidationRunRequest{
		Selectors: []string{"broken-links"},
		Surface:   validate.SurfaceLocal,
		Probes: validate.PrerequisiteProbes{Projection: &validationProjectionProbe{
			available: true,
		}},
	}, func(context.Context, []string) (validate.Result, error) {
		return validate.Result{}, runnerErr
	})
	require.ErrorIs(t, err, runnerErr)
}

type validationProjectionProbe struct {
	available bool
	err       error
	calls     map[validate.ProjectionDomain]int
}

func (p *validationProjectionProbe) AutoManagedProjection(_ context.Context, _ validate.CheckDescriptor, domain validate.ProjectionDomain) (validate.AutoManagedProjectionSnapshot, error) {
	if p.calls == nil {
		p.calls = make(map[validate.ProjectionDomain]int)
	}
	p.calls[domain]++
	return validate.AutoManagedProjectionSnapshot{Domain: domain, Available: p.available, Refreshed: true}, p.err
}

type validationRunnerSpy struct {
	checks []string
}

func (s *validationRunnerSpy) Run(_ context.Context, checks []string) (validate.Result, error) {
	s.checks = append([]string(nil), checks...)
	results := make([]validate.CheckResult, 0, len(checks))
	for _, check := range checks {
		results = append(results, validate.CheckResult{Name: check, OK: true})
	}
	return validate.Result{OK: true, Checks: results}, nil
}

func validationOutcomeStates(result ValidationResult) []validate.CheckOutcome {
	states := make([]validate.CheckOutcome, 0, len(result.Outcomes))
	for _, outcome := range result.Outcomes {
		states = append(states, outcome.Outcome)
	}
	return states
}
