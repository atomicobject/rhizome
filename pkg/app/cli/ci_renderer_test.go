package actions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/validate/identifierreconcile"
	"github.com/stretchr/testify/require"
)

func TestParseCIFormatAcceptsOnlyOneKnownRenderer(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"github", "json"} {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			format, err := ParseCIFormat(value)
			require.NoError(t, err)
			require.Equal(t, CIFormat(value), format)
		})
	}

	for _, value := range []string{"", "text", "github,json", "github json", "GitHub"} {
		value := value
		t.Run("reject_"+value, func(t *testing.T) {
			t.Parallel()
			_, err := ParseCIFormat(value)
			require.EqualError(t, err, `invalid CI format "`+value+`"; expected exactly one of github or json`)
		})
	}
}

func TestRenderCIJSONHasStableMachineValidContract(t *testing.T) {
	t.Parallel()

	result := ciRendererFixture()
	var first bytes.Buffer
	require.NoError(t, RenderCI(&first, CIFormatJSON, result))

	const want = `{"ok":false,"issueCount":2,"errorCount":0,"durationMs":7,"selectedChecks":["broken-links","code-anchors"],"checks":[{"name":"broken-links","ok":false,"issueCount":2,"summary":"2 broken links","durationMs":2,"issues":[{"code":"broken_heading_link","path":"docs,guide.md","target":"Target#Gone","message":"heading missing: 100% sure\ninspect","line":12},{"code":"broken_note_link","path":"docs/other.md","target":"Gone","message":"note missing","line":3}]}],"selector":"all","effectiveChecks":["broken-links","code-anchors"],"outcomes":[{"check":"broken-links","outcome":"completed","summary":"required validation prerequisites are available","remediationCommand":"rzm validate fix broken-links"},{"check":"code-anchors","outcome":"blocked","summary":"persisted code index is missing or incompatible","preparationCommand":"rzm index"}]}
`
	require.Equal(t, want, first.String())

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(first.Bytes(), &decoded))
	require.Equal(t, "all", decoded["selector"])
	require.Equal(t, []any{"broken-links", "code-anchors"}, decoded["effectiveChecks"])

	var second bytes.Buffer
	require.NoError(t, RenderCI(&second, CIFormatJSON, result))
	require.Equal(t, first.String(), second.String())
	require.NotContains(t, first.String(), "::error")
}

func TestRenderCIGitHubEscapesAnnotationsAndPrintsDeterministicSummary(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	require.NoError(t, RenderCI(&output, CIFormatGitHub, ciRendererFixture()))

	require.Equal(t, ""+
		"::error file=docs%2Cguide.md,line=12,title=broken-links%3Abroken_heading_link::heading missing: 100%25 sure%0Ainspect\n"+
		"::error file=docs/other.md,line=3,title=broken-links%3Abroken_note_link::note missing\n"+
		"::error title=code-anchors%3Aprerequisite::persisted code index is missing or incompatible; prepare: rzm index\n"+
		"Rhizome validation: selector=all; checks=broken-links,code-anchors; outcomes=broken-links:completed,code-anchors:blocked; issues=2; errors=0; exit=2\n"+
		"Remediate broken-links: rzm validate fix broken-links\n"+
		"Prepare code-anchors: rzm index\n",
		output.String(),
	)
}

func TestRenderCIGitHubBlockedAnnotationContainsExactEscapedPreparationCommand(t *testing.T) {
	t.Parallel()

	result := ValidationResult{
		Result:          validate.Result{OK: false, SelectedChecks: []string{"code-anchors"}},
		Selector:        "code-anchors",
		EffectiveChecks: []string{"code-anchors"},
		Outcomes: []ValidationCheckOutcome{{
			Check:              "code-anchors",
			Outcome:            validate.CheckOutcomeBlocked,
			Summary:            "persisted index unavailable\nfor this vault",
			PreparationCommand: "rzm index --scope src:pkg,cmd%",
		}},
	}

	var output bytes.Buffer
	require.NoError(t, RenderCI(&output, CIFormatGitHub, result))
	require.Equal(t, ""+
		"::error title=code-anchors%3Aprerequisite::persisted index unavailable for this vault; prepare: rzm index --scope src:pkg,cmd%25\n"+
		"Rhizome validation: selector=code-anchors; checks=code-anchors; outcomes=code-anchors:blocked; issues=0; errors=0; exit=2\n"+
		"Prepare code-anchors: rzm index --scope src:pkg,cmd%\n",
		output.String(),
	)
}

func TestRenderCIGitHubCleanResultDoesNotAdviseRemediation(t *testing.T) {
	t.Parallel()

	result, err := BuildValidationResult(
		validate.Selection{Selector: "broken-links", Checks: []string{validate.CheckBrokenLinks}},
		[]validate.CheckApplicabilityResult{{Check: validate.CheckBrokenLinks, Outcome: validate.CheckOutcomeCompleted}},
		validate.Result{OK: true, Checks: []validate.CheckResult{{Name: validate.CheckBrokenLinks, OK: true}}},
	)
	require.NoError(t, err)

	var output bytes.Buffer
	require.NoError(t, RenderCI(&output, CIFormatGitHub, result))
	require.NotContains(t, output.String(), "Remediate")
	require.NotContains(t, output.String(), "validate fix")
}

func TestRenderIdentifierReconciliationReportsAllSixStages(t *testing.T) {
	t.Parallel()

	result := ValidationResult{
		Result: validate.Result{
			OK:                       true,
			IdentifierReconciliation: identifierReconciliationRendererFixture(),
		},
		Selector: "identifiers",
	}

	t.Run("json", func(t *testing.T) {
		t.Parallel()
		var output bytes.Buffer
		require.NoError(t, RenderCI(&output, CIFormatJSON, result))
		for _, key := range []string{
			`"inventory":1000000`,
			`"gitProvenance":2000000`,
			`"referenceDiscovery":3000000`,
			`"transactionConstruction":4000000`,
			`"apply":5000000`,
			`"postValidation":6000000`,
			`"git":{"commands":4,"uniquePaths":3,"historicalBlobsParsed":2,"structuredBlobsScanned":1}`,
		} {
			require.Contains(t, output.String(), key)
		}
	})

	t.Run("human", func(t *testing.T) {
		t.Parallel()
		var output bytes.Buffer
		require.NoError(t, RenderValidationResultHuman(&output, result))
		require.Contains(t, output.String(), "Identifier reconciliation: collisions=1; fingerprint=plan-fingerprint; history-complete=false; fallback-collisions=1\n")
		require.Contains(t, output.String(), "  Timings: inventory=1ms; git-provenance=2ms; reference-discovery=3ms; transaction-construction=4ms; apply=5ms; post-validation=6ms\n")
	})

	t.Run("github", func(t *testing.T) {
		t.Parallel()
		var output bytes.Buffer
		require.NoError(t, RenderCI(&output, CIFormatGitHub, result))
		require.Contains(t, output.String(), "Identifier reconciliation: collisions=1; fingerprint=plan-fingerprint; history-complete=false; fallback-collisions=1; timings=inventory:1ms; git-provenance:2ms; reference-discovery:3ms; transaction-construction:4ms; apply:5ms; post-validation:6ms\n")
	})
}

func TestRenderRepairFollowUpsAsSourceBoundNonFailureWarnings(t *testing.T) {
	t.Parallel()

	planFollowUp := validate.RepairFollowUp{
		MembershipKeys: []string{"collision-plan"},
		SourcePath:     "notes/plan.md",
		SourceHash:     "sha-plan",
		Diagnostic: identifierreconcile.RepairDiagnostic{
			MembershipKeys: []string{"collision-plan"},
			Kind:           string(reference.IdentifierRewriteDiagnosticReviewOnly),
			Field: &reference.IdentifierRewriteDiagnostic{
				Kind:     reference.IdentifierRewriteDiagnosticReviewOnly,
				OwnerRef: ontology.NodeRef{NotePath: "notes/plan.md"},
				Value:    "SPEC-0001",
				Message:  "plain-text occurrence needs review",
			},
		},
	}
	executionFollowUp := validate.RepairFollowUp{
		MembershipKeys: []string{"collision-apply"},
		SourcePath:     "notes/apply.md",
		SourceHash:     "sha-apply",
		Diagnostic: identifierreconcile.RepairDiagnostic{
			MembershipKeys: []string{"collision-apply"},
			Kind:           string(reference.LinkRewriteDiagnosticUnresolvedTarget),
			Link: &reference.LinkRewriteDiagnostic{
				Kind:     reference.LinkRewriteDiagnosticUnresolvedTarget,
				NotePath: "notes/apply.md",
				Target:   "SPEC-0001",
				Message:  "unresolved link target needs review",
			},
		},
	}
	result := ValidationResult{
		Result: validate.Result{
			OK: true,
			FixPlan: &validate.FixPlan{
				FollowUps: []validate.RepairFollowUp{planFollowUp},
			},
			FixExecution: &validate.FixExecution{
				Requested: true,
				FollowUps: []validate.RepairFollowUp{executionFollowUp},
			},
		},
		Selector:        "identifiers",
		EffectiveChecks: []string{"identifiers"},
	}

	t.Run("human", func(t *testing.T) {
		t.Parallel()
		var output bytes.Buffer
		require.NoError(t, RenderValidationResultHuman(&output, result))
		require.Contains(t, output.String(), "Repair follow-ups: 2 review items\n")
		require.Contains(t, output.String(), "  Follow-up [plan] notes/plan.md [REVIEW_ONLY]: plain-text occurrence needs review; source-hash=sha-plan; membership=collision-plan")
		require.Contains(t, output.String(), `diagnostic={"membershipKeys":["collision-plan"],"kind":"REVIEW_ONLY"`)
		require.Contains(t, output.String(), "  Follow-up [apply] notes/apply.md [UNRESOLVED_TARGET]: unresolved link target needs review; source-hash=sha-apply; membership=collision-apply")
		require.NotContains(t, output.String(), "Action [")
		require.Contains(t, output.String(), "Validation summary: 0 issues; 0 errors; exit=0\n")
	})

	t.Run("github", func(t *testing.T) {
		t.Parallel()
		var output bytes.Buffer
		require.NoError(t, RenderCI(&output, CIFormatGitHub, result))
		require.Contains(t, output.String(), "::warning file=notes/plan.md,title=repair-follow-up%3AREVIEW_ONLY::phase=plan; plain-text occurrence needs review; source-hash=sha-plan; membership=collision-plan")
		require.Contains(t, output.String(), `diagnostic={"membershipKeys":["collision-plan"],"kind":"REVIEW_ONLY"`)
		require.Contains(t, output.String(), "::warning file=notes/apply.md,title=repair-follow-up%3AUNRESOLVED_TARGET::phase=apply; unresolved link target needs review; source-hash=sha-apply; membership=collision-apply")
		require.NotContains(t, output.String(), "::error")
		require.Contains(t, output.String(), "issues=0; errors=0; exit=0\n")
	})

	t.Run("json preserves both envelopes", func(t *testing.T) {
		t.Parallel()
		var output bytes.Buffer
		require.NoError(t, RenderCI(&output, CIFormatJSON, result))
		var decoded ValidationResult
		require.NoError(t, json.Unmarshal(output.Bytes(), &decoded))
		require.Equal(t, []validate.RepairFollowUp{planFollowUp}, decoded.FixPlan.FollowUps)
		require.Equal(t, []validate.RepairFollowUp{executionFollowUp}, decoded.FixExecution.FollowUps)
	})

	t.Run("identical plan and apply evidence renders once", func(t *testing.T) {
		t.Parallel()
		deduplicated := result
		deduplicated.FixExecution = &validate.FixExecution{
			Requested: true,
			FollowUps: []validate.RepairFollowUp{planFollowUp},
		}
		var human bytes.Buffer
		require.NoError(t, RenderValidationResultHuman(&human, deduplicated))
		require.Contains(t, human.String(), "Repair follow-ups: 1 review item\n")
		require.Contains(t, human.String(), "Follow-up [plan/apply]")

		var github bytes.Buffer
		require.NoError(t, RenderCI(&github, CIFormatGitHub, deduplicated))
		require.Equal(t, 1, bytes.Count(github.Bytes(), []byte("::warning")))
		require.Contains(t, github.String(), "phase=plan/apply")
	})
}

func TestRenderCIGitHubUsesStableFallbackAnnotationFields(t *testing.T) {
	t.Parallel()

	result := ValidationResult{
		Result: validate.Result{
			IssueCount:     1,
			SelectedChecks: []string{"ontology"},
			Checks: []validate.CheckResult{{
				Name:       "ontology",
				IssueCount: 1,
				Issues: []validate.Issue{{
					Source: "notes/source.md",
					Target: "SPEC-0001",
				}},
			}},
		},
		Selector:        "ontology",
		EffectiveChecks: []string{"ontology"},
		Outcomes: []ValidationCheckOutcome{{
			Check:              "ontology",
			Outcome:            validate.CheckOutcomeCompleted,
			RemediationCommand: "rzm validate fix ontology",
		}},
	}

	var output bytes.Buffer
	require.NoError(t, RenderCI(&output, CIFormatGitHub, result))
	require.Equal(t, ""+
		"::error file=notes/source.md,title=ontology::validation issue targeting SPEC-0001\n"+
		"Rhizome validation: selector=ontology; checks=ontology; outcomes=ontology:completed; issues=1; errors=0; exit=1\n"+
		"Remediate ontology: rzm validate fix ontology\n",
		output.String(),
	)
}

func TestRenderCIRejectsUnknownFormatBeforeWriting(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	err := RenderCI(&output, CIFormat("text"), ciRendererFixture())
	require.EqualError(t, err, `invalid CI format "text"; expected exactly one of github or json`)
	require.Empty(t, output.String())
}

func TestRenderValidationResultHumanShowsEveryOutcomeAndAction(t *testing.T) {
	t.Parallel()

	result := ciRendererFixture()
	result.EffectiveChecks = append(result.EffectiveChecks, "views")
	result.SelectedChecks = append(result.SelectedChecks, "views")
	result.Outcomes = append(result.Outcomes, ValidationCheckOutcome{
		Check:   "views",
		Outcome: validate.CheckOutcomeNotApplicable,
		Summary: "vault feature is not configured or present",
	})
	result.FixPlan = &validate.FixPlan{
		TotalCount: 3, SafeCount: 1, ConfirmationCount: 1, AgentCount: 1,
		Actions: []validate.FixAction{{
			Safety:  validate.FixSafetyAgent,
			Summary: "inspect the target\nnext: rzm agent node-link --target docs/guide#Missing --ensure plan",
		}},
	}
	result.FixExecution = &validate.FixExecution{
		Requested: true,
		Applied:   []string{"repair-1"},
		Skipped:   []string{"repair-2", "repair-3"},
	}

	var output bytes.Buffer
	require.NoError(t, RenderValidationResultHuman(&output, result))
	require.Equal(t, ""+
		"Validation selector: all\n"+
		"Effective checks: broken-links, code-anchors, views\n"+
		"[completed] broken-links: required validation prerequisites are available\n"+
		"  Check: 2 broken links (2 issues)\n"+
		"  - docs,guide.md:12 [broken_heading_link] heading missing: 100% sure inspect\n"+
		"  - docs/other.md:3 [broken_note_link] note missing\n"+
		"  Remediate: rzm validate fix broken-links\n"+
		"[blocked] code-anchors: persisted code index is missing or incompatible\n"+
		"  Prepare: rzm index\n"+
		"[not_applicable] views: vault feature is not configured or present\n"+
		"Repair plan: total=3; safe=1; confirmation=1; agent-required=1\n"+
		"  Action [agent_required]: inspect the target next: rzm agent node-link --target docs/guide#Missing --ensure plan\n"+
		"Repair apply: applied=1; skipped=2; failed=0; remaining=0\n"+
		"Validation summary: 2 issues; 0 errors; exit=2\n",
		output.String(),
	)
}

func TestRenderValidationResultHumanShowsRepairFailureEvidence(t *testing.T) {
	t.Parallel()
	result := ValidationResult{
		Result: validate.Result{FixExecution: &validate.FixExecution{
			Failed:            []string{"repair:one"},
			RemainingFindings: 2,
			ReplanCommand:     "rzm validate fix broken-links",
		}},
		Selector:       "broken-links",
		ExecutionError: "journal cleanup failed\nretry",
	}

	var output bytes.Buffer
	require.NoError(t, RenderValidationResultHuman(&output, result))
	require.Contains(t, output.String(), "Repair apply: applied=0; skipped=0; failed=1; remaining=2")
	require.Contains(t, output.String(), "Replan: rzm validate fix broken-links")
	require.Contains(t, output.String(), "Repair execution error: journal cleanup failed retry")
	require.Contains(t, output.String(), "exit=2")
}

func TestRenderValidationResultHumanShowsCheckErrorsAndIssueFallbacks(t *testing.T) {
	t.Parallel()

	result := ValidationResult{
		Result: validate.Result{
			IssueCount:     1,
			ErrorCount:     1,
			SelectedChecks: []string{"ontology"},
			Checks: []validate.CheckResult{{
				Name:       "ontology",
				Error:      "projection failed",
				IssueCount: 1,
				Issues:     []validate.Issue{{Code: "invalid_type", Target: "Spec"}},
			}},
		},
		Selector:        "ontology",
		EffectiveChecks: []string{"ontology"},
		Outcomes: []ValidationCheckOutcome{{
			Check:   "ontology",
			Outcome: validate.CheckOutcomeCompleted,
		}},
	}

	var output bytes.Buffer
	require.NoError(t, RenderValidationResultHuman(&output, result))
	require.Equal(t, ""+
		"Validation selector: ontology\n"+
		"Effective checks: ontology\n"+
		"[completed] ontology\n"+
		"  Check: 1 issue\n"+
		"  Error: projection failed\n"+
		"  - [invalid_type] validation issue targeting Spec\n"+
		"Validation summary: 1 issue; 1 error; exit=2\n",
		output.String(),
	)
}

func TestRenderCIErrorKeepsRendererStreamsExclusive(t *testing.T) {
	t.Parallel()

	t.Run("json", func(t *testing.T) {
		t.Parallel()
		var output bytes.Buffer
		require.NoError(t, RenderCIError(&output, CIFormatJSON, fmt.Errorf("bad config: 100%%\nfix it")))
		require.Equal(t, "{\"ok\":false,\"error\":\"bad config: 100%\\nfix it\",\"exitCode\":2}\n", output.String())
		require.NotContains(t, output.String(), "::error")
		var decoded map[string]any
		require.NoError(t, json.Unmarshal(output.Bytes(), &decoded))
	})

	t.Run("github", func(t *testing.T) {
		t.Parallel()
		var output bytes.Buffer
		require.NoError(t, RenderCIError(&output, CIFormatGitHub, fmt.Errorf("bad config: 100%%\nfix it")))
		require.Equal(t, ""+
			"::error title=Rhizome validation::bad config: 100%25%0Afix it\n"+
			"Rhizome validation: execution/configuration failure; exit=2\n",
			output.String(),
		)
		require.NotContains(t, output.String(), "{\"ok\"")
	})
}

func TestRenderCIErrorRejectsInvalidInputBeforeWriting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format CIFormat
		err    error
		want   string
	}{
		{name: "nil error", format: CIFormatJSON, want: "cannot render a nil CI error"},
		{name: "unknown format", format: "text", err: fmt.Errorf("failure"), want: `invalid CI format "text"; expected exactly one of github or json`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			err := RenderCIError(&output, tt.format, tt.err)
			require.EqualError(t, err, tt.want)
			require.Empty(t, output.String())
		})
	}
}

func ciRendererFixture() ValidationResult {
	return ValidationResult{
		Result: validate.Result{
			OK:             false,
			IssueCount:     2,
			DurationMs:     7,
			SelectedChecks: []string{"broken-links", "code-anchors"},
			Checks: []validate.CheckResult{
				{
					Name:       "broken-links",
					IssueCount: 2,
					Summary:    "2 broken links",
					DurationMs: 2,
					Issues: []validate.Issue{
						{
							Code:    "broken_heading_link",
							Path:    "docs,guide.md",
							Target:  "Target#Gone",
							Message: "heading missing: 100% sure\ninspect",
							Line:    12,
						},
						{
							Code:    "broken_note_link",
							Path:    "docs/other.md",
							Target:  "Gone",
							Message: "note missing",
							Line:    3,
						},
					},
				},
			},
		},
		Selector:        "all",
		EffectiveChecks: []string{"broken-links", "code-anchors"},
		Outcomes: []ValidationCheckOutcome{
			{
				Check:              "broken-links",
				Outcome:            validate.CheckOutcomeCompleted,
				Summary:            "required validation prerequisites are available",
				RemediationCommand: "rzm validate fix broken-links",
			},
			{
				Check:              "code-anchors",
				Outcome:            validate.CheckOutcomeBlocked,
				Summary:            "persisted code index is missing or incompatible",
				PreparationCommand: "rzm index",
			},
		},
	}
}

func identifierReconciliationRendererFixture() *identifierreconcile.ReconciliationResult {
	return &identifierreconcile.ReconciliationResult{
		Plan: &identifierreconcile.Plan{
			Fingerprint: "plan-fingerprint",
			Collisions:  []identifierreconcile.PlannedCollision{{Key: "collision-1"}},
		},
		Diagnostics: identifierreconcile.RunDiagnostics{
			Timings: identifierreconcile.StageTimings{
				Inventory:               time.Millisecond,
				GitProvenance:           2 * time.Millisecond,
				ReferenceDiscovery:      3 * time.Millisecond,
				TransactionConstruction: 4 * time.Millisecond,
				Apply:                   5 * time.Millisecond,
				PostValidation:          6 * time.Millisecond,
			},
			Git: identifierreconcile.GitProvenanceStats{
				Commands:               4,
				UniquePaths:            3,
				HistoricalBlobsParsed:  2,
				StructuredBlobsScanned: 1,
			},
			FallbackCollisions: 1,
		},
	}
}
