package validationrun

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/validate/identifierreconcile"
	"github.com/stretchr/testify/require"
)

func TestValidationResultExitCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result ValidationResult
		want   int
	}{
		{
			name: "clean completed checks",
			result: ValidationResult{
				Result:   validate.Result{OK: true},
				Outcomes: []ValidationCheckOutcome{{Outcome: validate.CheckOutcomeCompleted}},
			},
			want: ValidationExitClean,
		},
		{
			name: "not applicable is clean",
			result: ValidationResult{
				Result:   validate.Result{OK: true},
				Outcomes: []ValidationCheckOutcome{{Outcome: validate.CheckOutcomeNotApplicable}},
			},
			want: ValidationExitClean,
		},
		{
			name: "findings",
			result: ValidationResult{
				Result: validate.Result{IssueCount: 2},
			},
			want: ValidationExitFindings,
		},
		{
			name: "check findings even if aggregate is incomplete",
			result: ValidationResult{
				Result: validate.Result{Checks: []validate.CheckResult{{IssueCount: 1}}},
			},
			want: ValidationExitFindings,
		},
		{
			name: "blocked takes precedence over findings",
			result: ValidationResult{
				Result:   validate.Result{IssueCount: 2},
				Outcomes: []ValidationCheckOutcome{{Outcome: validate.CheckOutcomeBlocked}},
			},
			want: ValidationExitFailure,
		},
		{
			name: "aggregate error takes precedence over findings",
			result: ValidationResult{
				Result: validate.Result{IssueCount: 2, ErrorCount: 1},
			},
			want: ValidationExitFailure,
		},
		{
			name: "check error takes precedence over findings",
			result: ValidationResult{
				Result: validate.Result{
					IssueCount: 2,
					Checks:     []validate.CheckResult{{Error: "projection unavailable"}},
				},
			},
			want: ValidationExitFailure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, tt.result.ExitCode())
		})
	}
}

func TestRebuildAfterRepairReplacesFindingsWithoutExpandingPublicSelection(t *testing.T) {
	planned := ValidationResult{
		Result: validate.Result{
			IssueCount: 1,
			Checks:     []validate.CheckResult{{Name: "broken-links", IssueCount: 1}},
		},
		Selector:        "broken-links",
		EffectiveChecks: []string{"broken-links"},
		Outcomes: []ValidationCheckOutcome{{
			Check:   "broken-links",
			Outcome: validate.CheckOutcomeCompleted,
		}},
	}
	execution := &validate.FixExecution{Applied: []string{"repair:broken-link"}}
	postcheck := validate.Result{
		OK:             true,
		SelectedChecks: []string{"broken-links", "link-hygiene"},
		Checks: []validate.CheckResult{
			{Name: "broken-links", OK: true},
			{Name: "link-hygiene", OK: true},
		},
	}

	got, err := RebuildAfterRepair(planned, postcheck, execution)
	require.NoError(t, err)
	require.Equal(t, "broken-links", got.Selector)
	require.Equal(t, []string{"broken-links"}, got.EffectiveChecks)
	require.Equal(t, []ValidationCheckOutcome{{Check: "broken-links", Outcome: validate.CheckOutcomeCompleted}}, got.Outcomes)
	require.Equal(t, []validate.CheckResult{{Name: "broken-links", OK: true}}, got.Checks)
	require.Zero(t, got.IssueCount)
	require.Equal(t, execution, got.FixExecution)
}

func TestRebuildAfterRepairScopesFreshPlanToOriginalCompletedChecks(t *testing.T) {
	identifierIssue := validate.Issue{Code: "missing_required_field", Path: "docs/spec.md", Field: "id"}
	identifierKey, err := validate.StableIssueKey(validate.CheckIdentifiers, identifierIssue)
	require.NoError(t, err)
	identifierIssue.Key = identifierKey
	brokenLinkKey, err := validate.StableIssueKey(validate.CheckBrokenLinks, validate.Issue{Code: "note_missing", Path: "docs/search.md", Target: "search"})
	require.NoError(t, err)
	ontologyKey, err := validate.StableIssueKey(validate.CheckOntology, validate.Issue{Code: "field_type_mismatch", Path: "docs/spec.md", Field: "status"})
	require.NoError(t, err)

	planned := ValidationResult{
		Selector:        "identifiers",
		EffectiveChecks: []string{"identifiers"},
		Outcomes: []ValidationCheckOutcome{{
			Check: "identifiers", Outcome: validate.CheckOutcomeCompleted,
		}},
	}
	postcheck := validate.Result{
		IssueCount:     3,
		ApplyCommand:   "rzm validate fix identifiers --apply --allow-historical --max-issues 220",
		SelectedChecks: []string{validate.CheckIdentifiers, validate.CheckBrokenLinks, validate.CheckOntology},
		Checks: []validate.CheckResult{
			{Name: validate.CheckIdentifiers, IssueCount: 1, Issues: []validate.Issue{identifierIssue}},
			{Name: validate.CheckBrokenLinks, IssueCount: 1},
			{Name: validate.CheckOntology, IssueCount: 1},
		},
		FixPlan: &validate.FixPlan{
			TotalCount: 3, ConfirmationCount: 1, AgentCount: 2,
			IssueKeys: []string{identifierKey, brokenLinkKey, ontologyKey},
			Actions: []validate.FixAction{
				{ID: "identifier", Check: validate.CheckIdentifiers, IssueCode: identifierIssue.Code, Safety: validate.FixSafetyConfirm, IssueKeys: []string{identifierKey}},
				{ID: "broken-link", Check: validate.CheckBrokenLinks, IssueCode: "note_missing", Safety: validate.FixSafetyAgent, IssueKeys: []string{brokenLinkKey}},
				{ID: "ontology", Check: validate.CheckOntology, IssueCode: "field_type_mismatch", Safety: validate.FixSafetyAgent, IssueKeys: []string{ontologyKey}},
			},
		},
	}

	got, err := RebuildAfterRepair(planned, postcheck, &validate.FixExecution{RemainingFindings: 1})
	require.NoError(t, err)
	require.Equal(t, []string{"identifiers"}, got.EffectiveChecks)
	require.Len(t, got.Checks, 1)
	require.NotNil(t, got.FixPlan)
	require.Equal(t, 1, got.FixPlan.TotalCount)
	require.Equal(t, 1, got.FixPlan.ConfirmationCount)
	require.Zero(t, got.FixPlan.AgentCount)
	require.Equal(t, []string{"identifier"}, []string{got.FixPlan.Actions[0].ID})
	require.Equal(t, []string{identifierKey}, got.FixPlan.IssueKeys)
	require.NotNil(t, got.NextActions)
	require.Equal(t, 1, got.NextActions.NeedsConfirmationCount)
	require.Zero(t, got.NextActions.AgentRequiredCount)
	require.Equal(t, "rzm validate fix identifiers --apply --allow-historical --max-issues 220", got.NextActions.Actions[0].Command)
}

func TestValidationResultExitCodeTreatsFailedRepairAsExecutionFailure(t *testing.T) {
	result := ValidationResult{Result: validate.Result{
		OK:           true,
		FixExecution: &validate.FixExecution{Failed: []string{"repair:one"}},
	}}
	require.Equal(t, ValidationExitFailure, result.ExitCode())
}

func TestRebuildAfterRepairPreservesBlockedAndNotApplicableOutcomes(t *testing.T) {
	planned := ValidationResult{
		Selector:        "all",
		EffectiveChecks: []string{"broken-links", "code-anchors", "views"},
		Outcomes: []ValidationCheckOutcome{
			{Check: "broken-links", Outcome: validate.CheckOutcomeCompleted},
			{Check: "code-anchors", Outcome: validate.CheckOutcomeBlocked, PreparationCommand: "rzm index"},
			{Check: "views", Outcome: validate.CheckOutcomeNotApplicable},
		},
	}
	postcheck := validate.Result{
		IssueCount:     2,
		ErrorCount:     1,
		SelectedChecks: []string{"broken-links", "link-hygiene", "code-anchors"},
		Checks: []validate.CheckResult{
			{Name: "broken-links", OK: true},
			{Name: "link-hygiene", IssueCount: 1},
			{Name: "code-anchors", Error: "unexpected affected-check error"},
		},
	}

	execution := &validate.FixExecution{RemainingFindings: 1, RemainingIssueKeys: []string{"affected:key"}}
	got, err := RebuildAfterRepair(planned, postcheck, execution)
	require.NoError(t, err)
	require.Equal(t, planned.Selector, got.Selector)
	require.Equal(t, planned.EffectiveChecks, got.EffectiveChecks)
	require.Equal(t, planned.Outcomes, got.Outcomes)
	require.Equal(t, []validate.CheckResult{{Name: "broken-links", OK: true}}, got.Checks)
	require.Zero(t, got.IssueCount)
	require.Zero(t, got.ErrorCount)
	require.Equal(t, execution, got.FixExecution)
	require.Equal(t, ValidationExitFailure, got.ExitCode(), "the preserved blocked prerequisite dominates remaining findings")
}

func TestRebuildAfterRepairReplacesRecoveredJournalBarrierOutcomes(t *testing.T) {
	planned := ValidationResult{
		Result:          validate.Result{RepairJournals: []validate.RepairJournalEvidence{{State: "committed"}}},
		Selector:        "all",
		EffectiveChecks: []string{"broken-links", "link-hygiene", "code-anchors"},
		Outcomes: []ValidationCheckOutcome{
			{
				Check:              "broken-links",
				Outcome:            validate.CheckOutcomeBlocked,
				Summary:            "pending repair journal must be recovered",
				PreparationCommand: "rzm validate fix all --apply",
				Evidence: []validate.ApplicabilityEvidence{{
					Code: "pending_repair_journal",
				}},
			},
			{
				Check:              "link-hygiene",
				Outcome:            validate.CheckOutcomeBlocked,
				Summary:            "pending repair journal must be recovered",
				PreparationCommand: "rzm validate fix all --apply",
				Evidence: []validate.ApplicabilityEvidence{{
					Code: "pending_repair_journal",
				}},
			},
			{
				Check:              "code-anchors",
				Outcome:            validate.CheckOutcomeBlocked,
				Summary:            "code index is stale",
				PreparationCommand: "rzm index",
				Evidence: []validate.ApplicabilityEvidence{{
					Code: "code_index_stale",
				}},
			},
		},
	}
	postcheck := validate.Result{
		SelectedChecks: []string{validate.CheckBrokenLinks},
		Checks:         []validate.CheckResult{{Name: validate.CheckBrokenLinks, OK: true}},
	}
	execution := &validate.FixExecution{ReplanCommand: "rzm validate fix all"}

	got, err := RebuildAfterRepair(planned, postcheck, execution)
	require.NoError(t, err)
	require.Len(t, got.Checks, 1)
	require.Equal(t, validate.CheckOutcomeCompleted, got.Outcomes[0].Outcome)
	require.Empty(t, got.Outcomes[0].PreparationCommand)
	require.Empty(t, got.Outcomes[0].Evidence)
	require.Equal(t, validate.CheckOutcomeBlocked, got.Outcomes[1].Outcome)
	require.Equal(t, "rzm validate fix all", got.Outcomes[1].PreparationCommand)
	require.Equal(t, "repair_replan_required", got.Outcomes[1].Evidence[0].Code)
	require.Contains(t, got.Outcomes[1].Summary, "recovery completed")
	require.Equal(t, planned.Outcomes[2], got.Outcomes[2], "real prerequisites remain blocked")
}

func TestRebuildAfterRepairUsesExecutionRemainingFindingsForExitOne(t *testing.T) {
	planned := ValidationResult{
		Selector:        "broken-links",
		EffectiveChecks: []string{"broken-links"},
		Outcomes: []ValidationCheckOutcome{{
			Check: "broken-links", Outcome: validate.CheckOutcomeCompleted,
		}},
	}
	execution := &validate.FixExecution{
		RemainingFindings:  1,
		RemainingIssueKeys: []string{"affected:key"},
		ReplanCommand:      "rzm validate fix broken-links",
	}
	postcheck := validate.Result{
		SelectedChecks: []string{"broken-links", "link-hygiene"},
		Checks: []validate.CheckResult{
			{Name: "broken-links", OK: true},
			{Name: "link-hygiene", IssueCount: 1},
		},
	}

	got, err := RebuildAfterRepair(planned, postcheck, execution)
	require.NoError(t, err)
	require.Zero(t, got.IssueCount)
	require.Equal(t, planned.EffectiveChecks, got.EffectiveChecks)
	require.Equal(t, execution, got.FixExecution)
	require.False(t, got.OK)
	require.Equal(t, ValidationExitFindings, got.ExitCode())
}

func TestRebuildAfterNonInteractiveConfirmationSkipRetainsFreshStructuredPlan(t *testing.T) {
	stale := &validate.FixPlan{
		ConfirmationCount: 1,
		Actions: []validate.FixAction{{
			ID: "stale-action", Safety: validate.FixSafetyConfirm,
			Question: "Use the stale candidate?", CandidatePaths: []string{"stale.md"},
		}},
	}
	fresh := &validate.FixPlan{
		ConfirmationCount: 1,
		Actions: []validate.FixAction{{
			ID: "fresh-action", Safety: validate.FixSafetyConfirm,
			Question: "Use the fresh candidate?", CandidatePaths: []string{"fresh.md"},
		}},
	}
	planned := ValidationResult{
		Result: validate.Result{
			IssueCount: 1,
			Checks:     []validate.CheckResult{{Name: validate.CheckBrokenLinks, IssueCount: 1}},
			FixPlan:    stale,
		},
		Selector:        "broken-links",
		EffectiveChecks: []string{"broken-links"},
		Outcomes: []ValidationCheckOutcome{{
			Check: "broken-links", Outcome: validate.CheckOutcomeCompleted,
		}},
	}
	postcheck := validate.Result{
		IssueCount:     1,
		SelectedChecks: []string{validate.CheckBrokenLinks},
		Checks:         []validate.CheckResult{{Name: validate.CheckBrokenLinks, IssueCount: 1}},
		FixPlan:        fresh,
	}
	execution := &validate.FixExecution{
		Requested: true, NonInteractive: true,
		Skipped: []string{"fresh-action"}, RemainingFindings: 1,
	}

	got, err := RebuildAfterRepair(planned, postcheck, execution)
	require.NoError(t, err)
	require.Same(t, fresh, got.FixPlan, "use the current postcheck plan, never the reviewed pre-apply plan")
	require.Equal(t, "fresh-action", got.FixPlan.Actions[0].ID)
	require.Equal(t, "Use the fresh candidate?", got.FixPlan.Actions[0].Question)
	require.Equal(t, []string{"fresh.md"}, got.FixPlan.Actions[0].CandidatePaths)
	require.NotNil(t, got.NextActions)
	require.Equal(t, 1, got.NextActions.NeedsConfirmationCount)
	require.Equal(t, "needs_confirmation", got.NextActions.Actions[0].Category)
}

func TestBuildValidationResultPreservesIdentifierReconciliationEnvelope(t *testing.T) {
	t.Parallel()

	reconciliation := &identifierreconcile.ReconciliationResult{
		Plan: &identifierreconcile.Plan{Fingerprint: "identifier-plan"},
		Diagnostics: identifierreconcile.RunDiagnostics{
			HistoryComplete:    true,
			FallbackCollisions: 2,
		},
	}
	result, err := BuildValidationResult(
		validate.Selection{Selector: "identifiers", Checks: []string{validate.CheckIdentifiers}},
		[]validate.CheckApplicabilityResult{{Check: validate.CheckIdentifiers, Outcome: validate.CheckOutcomeCompleted}},
		validate.Result{
			Checks:                   []validate.CheckResult{{Name: validate.CheckIdentifiers, OK: true}},
			IdentifierReconciliation: reconciliation,
		},
	)
	require.NoError(t, err)
	require.Same(t, reconciliation, result.IdentifierReconciliation)

	payload, err := json.Marshal(result)
	require.NoError(t, err)
	require.Contains(t, string(payload), `"identifierReconciliation":{"plan":{"version":"","collisions":null,"fingerprint":"identifier-plan"}`)
	require.Contains(t, string(payload), `"historyComplete":true`)
}

func TestBuildValidationResultPopulatesIdentifierApplyAndPostValidationTimings(t *testing.T) {
	t.Parallel()

	reconciliation := &identifierreconcile.ReconciliationResult{
		Plan: &identifierreconcile.Plan{Fingerprint: "identifier-plan"},
	}
	result, err := BuildValidationResult(
		validate.Selection{Selector: "identifiers", Checks: []string{validate.CheckIdentifiers}},
		[]validate.CheckApplicabilityResult{{Check: validate.CheckIdentifiers, Outcome: validate.CheckOutcomeCompleted}},
		validate.Result{
			Checks:                   []validate.CheckResult{{Name: validate.CheckIdentifiers, OK: true}},
			IdentifierReconciliation: reconciliation,
			FixExecution: &validate.FixExecution{IdentifierReconciliationTimings: &identifierreconcile.StageTimings{
				Apply:          5 * time.Millisecond,
				PostValidation: 7 * time.Millisecond,
			}},
		},
	)
	require.NoError(t, err)
	require.Equal(t, 5*time.Millisecond, result.IdentifierReconciliation.Diagnostics.Timings.Apply)
	require.Equal(t, 7*time.Millisecond, result.IdentifierReconciliation.Diagnostics.Timings.PostValidation)

	payload, err := json.Marshal(result)
	require.NoError(t, err)
	require.Contains(t, string(payload), `"apply":5000000`)
	require.Contains(t, string(payload), `"postValidation":7000000`)
	require.NotContains(t, string(payload), "identifierReconciliationTimings")
}

func TestAttachFixExecutionPopulatesPostBuildTimingsWithoutMutatingPlanningResult(t *testing.T) {
	t.Parallel()

	reconciliation := &identifierreconcile.ReconciliationResult{
		Plan: &identifierreconcile.Plan{Fingerprint: "identifier-plan"},
		Diagnostics: identifierreconcile.RunDiagnostics{Timings: identifierreconcile.StageTimings{
			Inventory: 3 * time.Millisecond,
		}},
	}
	planning := ValidationResult{Result: validate.Result{IdentifierReconciliation: reconciliation}}
	execution := &validate.FixExecution{IdentifierReconciliationTimings: &identifierreconcile.StageTimings{
		Apply:          5 * time.Millisecond,
		PostValidation: 7 * time.Millisecond,
	}}

	applied := AttachFixExecution(planning, execution)

	require.Nil(t, planning.FixExecution)
	require.Same(t, reconciliation, planning.IdentifierReconciliation)
	require.Zero(t, planning.IdentifierReconciliation.Diagnostics.Timings.Apply)
	require.Zero(t, planning.IdentifierReconciliation.Diagnostics.Timings.PostValidation)
	require.Same(t, execution, applied.FixExecution)
	require.NotSame(t, planning.IdentifierReconciliation, applied.IdentifierReconciliation)
	require.Equal(t, 3*time.Millisecond, applied.IdentifierReconciliation.Diagnostics.Timings.Inventory)
	require.Equal(t, 5*time.Millisecond, applied.IdentifierReconciliation.Diagnostics.Timings.Apply)
	require.Equal(t, 7*time.Millisecond, applied.IdentifierReconciliation.Diagnostics.Timings.PostValidation)
}

func TestBuildValidationResult_StableJSONAndExactCommands(t *testing.T) {
	t.Parallel()

	result, err := BuildValidationResult(
		validate.Selection{Selector: "all", Checks: []string{validate.CheckBrokenLinks, validate.CheckCodeAnchors}},
		[]validate.CheckApplicabilityResult{
			{
				Check:   validate.CheckBrokenLinks,
				Outcome: validate.CheckOutcomeCompleted,
				Summary: "validation projection is available",
			},
			{
				Check:              validate.CheckCodeAnchors,
				Outcome:            validate.CheckOutcomeBlocked,
				Summary:            "persisted code index is unavailable",
				PreparationCommand: "rzm index",
			},
		},
		validate.Result{
			OK:             false,
			IssueCount:     1,
			ErrorCount:     0,
			DurationMs:     7,
			SelectedChecks: []string{validate.CheckBrokenLinks},
			Checks: []validate.CheckResult{{
				Name:       validate.CheckBrokenLinks,
				OK:         false,
				IssueCount: 1,
				DurationMs: 2,
			}},
		},
	)
	require.NoError(t, err)

	payload, err := json.Marshal(result)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"ok": false,
		"issueCount": 1,
		"errorCount": 0,
		"durationMs": 7,
		"selectedChecks": ["broken-links", "code-anchors"],
		"checks": [{
			"name": "broken-links",
			"ok": false,
			"issueCount": 1,
			"durationMs": 2
		}],
		"selector": "all",
		"effectiveChecks": ["broken-links", "code-anchors"],
		"outcomes": [
			{
				"check": "broken-links",
				"outcome": "completed",
				"summary": "validation projection is available",
				"remediationCommand": "rzm validate fix broken-links"
			},
			{
				"check": "code-anchors",
				"outcome": "blocked",
				"summary": "persisted code index is unavailable",
				"preparationCommand": "rzm index"
			}
		]
	}`, string(payload))
	require.Equal(t, ValidationExitFailure, result.ExitCode())
}

func TestBuildValidationResult_EmitsRemediationOnlyForActionableCompletedChecks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		outcome     validate.CheckOutcome
		checkResult *validate.CheckResult
		wantCommand string
	}{
		{
			name:        "findings require remediation",
			outcome:     validate.CheckOutcomeCompleted,
			checkResult: &validate.CheckResult{Name: validate.CheckBrokenLinks, IssueCount: 1},
			wantCommand: "rzm validate fix broken-links",
		},
		{
			name:    "fix actions require remediation even without findings",
			outcome: validate.CheckOutcomeCompleted,
			checkResult: &validate.CheckResult{
				Name:  validate.CheckBrokenLinks,
				Fixes: []validate.FixAction{{ID: "fix-1"}},
			},
			wantCommand: "rzm validate fix broken-links",
		},
		{
			name:        "clean completed check has no remediation",
			outcome:     validate.CheckOutcomeCompleted,
			checkResult: &validate.CheckResult{Name: validate.CheckBrokenLinks, OK: true},
		},
		{
			name:    "not applicable check has no remediation",
			outcome: validate.CheckOutcomeNotApplicable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			suite := validate.Result{OK: true}
			if tt.checkResult != nil {
				suite.Checks = []validate.CheckResult{*tt.checkResult}
			}
			result, err := BuildValidationResult(
				validate.Selection{Selector: "broken-links", Checks: []string{validate.CheckBrokenLinks}},
				[]validate.CheckApplicabilityResult{{Check: validate.CheckBrokenLinks, Outcome: tt.outcome}},
				suite,
			)
			require.NoError(t, err)
			require.Equal(t, tt.wantCommand, result.Outcomes[0].RemediationCommand)
		})
	}
}

func TestBuildValidationResult_UsesOnlyPublicCheckLabels(t *testing.T) {
	t.Parallel()

	selection := validate.Selection{
		Selector: "audit",
		Checks: []string{
			validate.CheckIdentifiers,
			validate.CheckFrozenScopeDrift,
			validate.CheckFragileExternal,
			validate.CheckOrphanBlockIDs,
		},
	}
	applicability := make([]validate.CheckApplicabilityResult, 0, len(selection.Checks))
	checks := make([]validate.CheckResult, 0, len(selection.Checks))
	for _, check := range selection.Checks {
		applicability = append(applicability, validate.CheckApplicabilityResult{Check: check, Outcome: validate.CheckOutcomeCompleted})
		checks = append(checks, validate.CheckResult{Name: check, OK: true})
	}

	result, err := BuildValidationResult(selection, applicability, validate.Result{OK: true, Checks: checks})
	require.NoError(t, err)
	require.Equal(t, []string{"identifiers", "frozen-scope-drift", "fragile-external", "orphan-block-ids"}, result.EffectiveChecks)
	require.Equal(t, result.EffectiveChecks, result.SelectedChecks)
	for index, check := range result.Checks {
		require.Equal(t, result.EffectiveChecks[index], check.Name)
		require.Equal(t, result.EffectiveChecks[index], result.Outcomes[index].Check)
	}
}

func TestBuildValidationResult_RejectsMissingOrUnexpectedOutcomes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		outcomes []validate.CheckApplicabilityResult
		want     string
	}{
		{name: "missing", want: `missing applicability outcome for selected check "broken-links"`},
		{
			name: "unexpected",
			outcomes: []validate.CheckApplicabilityResult{
				{Check: validate.CheckBrokenLinks, Outcome: validate.CheckOutcomeCompleted},
				{Check: validate.CheckOntology, Outcome: validate.CheckOutcomeCompleted},
			},
			want: `applicability outcome for unselected check "ontology"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := BuildValidationResult(
				validate.Selection{Selector: "default", Checks: []string{validate.CheckBrokenLinks}},
				tt.outcomes,
				validate.Result{},
			)
			require.EqualError(t, err, tt.want)
		})
	}
}

func TestBuildValidationResult_RequiresExactCompletedSuiteResults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		selection  validate.Selection
		outcomes   []validate.CheckApplicabilityResult
		checks     []validate.CheckResult
		wantErr    string
		wantChecks []string
	}{
		{
			name:      "completed check requires result",
			selection: validate.Selection{Selector: "default", Checks: []string{validate.CheckBrokenLinks}},
			outcomes: []validate.CheckApplicabilityResult{{
				Check: validate.CheckBrokenLinks, Outcome: validate.CheckOutcomeCompleted,
			}},
			wantErr: `missing suite result for completed check "broken-links"`,
		},
		{
			name:      "blocked check rejects result",
			selection: validate.Selection{Selector: "code-anchors", Checks: []string{validate.CheckCodeAnchors}},
			outcomes: []validate.CheckApplicabilityResult{{
				Check: validate.CheckCodeAnchors, Outcome: validate.CheckOutcomeBlocked,
			}},
			checks:  []validate.CheckResult{{Name: validate.CheckCodeAnchors}},
			wantErr: `suite result for blocked check "code-anchors"`,
		},
		{
			name:      "not applicable check rejects result",
			selection: validate.Selection{Selector: "ontology", Checks: []string{validate.CheckOntology}},
			outcomes: []validate.CheckApplicabilityResult{{
				Check: validate.CheckOntology, Outcome: validate.CheckOutcomeNotApplicable,
			}},
			checks:  []validate.CheckResult{{Name: validate.CheckOntology}},
			wantErr: `suite result for not_applicable check "ontology"`,
		},
		{
			name:      "unselected result is rejected",
			selection: validate.Selection{Selector: "default", Checks: []string{validate.CheckBrokenLinks}},
			outcomes: []validate.CheckApplicabilityResult{{
				Check: validate.CheckBrokenLinks, Outcome: validate.CheckOutcomeCompleted,
			}},
			checks: []validate.CheckResult{
				{Name: validate.CheckBrokenLinks},
				{Name: validate.CheckOntology},
			},
			wantErr: `suite result for unselected check "ontology"`,
		},
		{
			name:      "duplicate result is rejected",
			selection: validate.Selection{Selector: "default", Checks: []string{validate.CheckBrokenLinks}},
			outcomes: []validate.CheckApplicabilityResult{{
				Check: validate.CheckBrokenLinks, Outcome: validate.CheckOutcomeCompleted,
			}},
			checks: []validate.CheckResult{
				{Name: validate.CheckBrokenLinks},
				{Name: "broken-links"},
			},
			wantErr: `duplicate suite result for check "broken-links"`,
		},
		{
			name:      "legacy skipped result is rejected",
			selection: validate.Selection{Selector: "views", Checks: []string{validate.CheckViews}},
			outcomes: []validate.CheckApplicabilityResult{{
				Check:   validate.CheckViews,
				Outcome: validate.CheckOutcomeCompleted,
			}},
			checks:  []validate.CheckResult{{Name: validate.CheckViews, Skipped: true}},
			wantErr: `completed check "views" returned a legacy skipped result`,
		},
		{
			name: "completed and non-completed checks have exact result cardinality",
			selection: validate.Selection{Selector: "all", Checks: []string{
				validate.CheckBrokenLinks, validate.CheckIdentifiers, validate.CheckCodeAnchors, validate.CheckOntology,
			}},
			outcomes: []validate.CheckApplicabilityResult{
				{Check: validate.CheckBrokenLinks, Outcome: validate.CheckOutcomeCompleted},
				{Check: validate.CheckIdentifiers, Outcome: validate.CheckOutcomeCompleted},
				{Check: validate.CheckCodeAnchors, Outcome: validate.CheckOutcomeBlocked},
				{Check: validate.CheckOntology, Outcome: validate.CheckOutcomeNotApplicable},
			},
			checks: []validate.CheckResult{
				{Name: "identifiers", OK: true},
				{Name: "broken-links", OK: true},
			},
			wantChecks: []string{"broken-links", "identifiers"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := BuildValidationResult(tt.selection, tt.outcomes, validate.Result{Checks: tt.checks})
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			names := make([]string, 0, len(result.Checks))
			for _, check := range result.Checks {
				names = append(names, check.Name)
			}
			require.Equal(t, tt.wantChecks, names)
		})
	}
}
