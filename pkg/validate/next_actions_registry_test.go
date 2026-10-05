package validate

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildNextActionsUsesStableIssueKeyCoverageNotInstanceCounts(t *testing.T) {
	t.Parallel()

	covered := Issue{Code: "identifier_not_in_aliases", Path: "covered.md", Target: "SPEC-1"}
	uncovered := Issue{Code: "identifier_not_in_aliases", Path: "uncovered.md", Target: "SPEC-2"}
	coveredKey, err := StableIssueKey(CheckIdentifiers, covered)
	require.NoError(t, err)
	uncoveredKey, err := StableIssueKey(CheckIdentifiers, uncovered)
	require.NoError(t, err)
	covered.Key = coveredKey
	uncovered.Key = uncoveredKey

	next := BuildNextActions(Result{
		IssueCount:     2,
		SelectedChecks: []string{CheckIdentifiers},
		ApplyCommand:   "rzm validate fix default --apply --vault docs --scope-note 'A B.md' --allow-historical",
		Checks: []CheckResult{{
			Name: CheckIdentifiers, IssueCount: 2,
			Issues: []Issue{covered, uncovered}, fullIssues: []Issue{covered, uncovered},
		}},
		FixPlan: &FixPlan{
			SafeCount: 1,
			Actions: []FixAction{{
				ID: "covered", Check: CheckIdentifiers, IssueCode: covered.Code,
				Safety: FixSafetySafe, InstanceCount: 999, IssueKeys: []string{coveredKey},
			}},
		},
	})

	require.NotNil(t, next)
	require.Zero(t, next.UnclassifiedIssueCount)
	require.False(t, next.ClassificationRequired)
	require.Len(t, next.Actions, 2)
	require.Equal(t, []string{coveredKey}, next.Actions[0].IssueKeys)
	require.Equal(t, []string{uncoveredKey}, next.Actions[1].IssueKeys)
	require.Equal(t, "agent_required", next.Actions[1].Category)
	require.Empty(t, next.Actions[1].Command)
	require.NotEmpty(t, next.Actions[1].NonFixableReason)
}

func TestBuildNextActionsReturnsStructuredNonFixableReasonPerIssueKey(t *testing.T) {
	t.Parallel()

	issue := Issue{Code: "view_read_error", Path: ".rhizome/views/example.yaml"}
	key, err := StableIssueKey(CheckViews, issue)
	require.NoError(t, err)
	issue.Key = key

	next := BuildNextActions(Result{
		IssueCount: 1,
		Checks: []CheckResult{{
			Name: CheckViews, IssueCount: 1, Issues: []Issue{issue}, fullIssues: []Issue{issue},
		}},
	})

	require.NotNil(t, next)
	require.Zero(t, next.UnclassifiedIssueCount)
	require.False(t, next.ClassificationRequired)
	require.Len(t, next.Actions, 1)
	require.Equal(t, "non_fixable", next.Actions[0].Category)
	require.Equal(t, []string{key}, next.Actions[0].IssueKeys)
	require.NotEmpty(t, next.Actions[0].NonFixableReason)
	require.Empty(t, next.Actions[0].Command)
}

func TestBuildNextActionsDoesNotTreatFallbackActionIdentityAsIssueCoverage(t *testing.T) {
	t.Parallel()

	issue := Issue{Code: "identifier_not_in_aliases", Path: "note.md", Target: "SPEC-1"}
	key, err := StableIssueKey(CheckIdentifiers, issue)
	require.NoError(t, err)
	issue.Key = key

	next := BuildNextActions(Result{
		IssueCount: 1, SelectedChecks: []string{CheckIdentifiers},
		Checks: []CheckResult{{
			Name: CheckIdentifiers, IssueCount: 1, Issues: []Issue{issue}, fullIssues: []Issue{issue},
		}},
		FixPlan: &FixPlan{
			SafeCount: 1,
			Actions: []FixAction{{
				Safety: FixSafetySafe, IssueKeys: []string{"issue:action:v1:not-a-finding"},
			}},
		},
	})

	require.NotNil(t, next)
	require.Len(t, next.Actions, 2)
	require.Empty(t, next.Actions[0].IssueKeys)
	require.Equal(t, []string{key}, next.Actions[1].IssueKeys)
}

func TestBuildNextActionsPreservesExactCommandForConfirmationScope(t *testing.T) {
	t.Parallel()

	issue := Issue{Code: "suffix_match", Path: "notes/code.md", Target: "pkg.Symbol"}
	key, err := StableIssueKey(CheckCodeAnchors, issue)
	require.NoError(t, err)
	issue.Key = key
	command := "rzm agent validate fix all --apply --vault docs --scope-ref 'node:one two' --allow-historical"

	next := BuildNextActions(Result{
		IssueCount: 1, SelectedChecks: []string{CheckCodeAnchors}, ApplyCommand: command,
		Checks: []CheckResult{{
			Name: CheckCodeAnchors, IssueCount: 1, Issues: []Issue{issue}, fullIssues: []Issue{issue},
		}},
		FixPlan: &FixPlan{
			ConfirmationCount: 1,
			Actions:           []FixAction{{Safety: FixSafetyConfirm, IssueKeys: []string{key}}},
		},
	})

	require.NotNil(t, next)
	require.Len(t, next.Actions, 1)
	require.Equal(t, "needs_confirmation", next.Actions[0].Category)
	require.Equal(t, "rzm validate fix all --apply --vault docs --scope-ref 'node:one two' --allow-historical", next.Actions[0].Command)
	require.Equal(t, []string{key}, next.Actions[0].IssueKeys)
}

func TestBuildNextActionsDoesNotOfferAgentApplyForActionOnlyRemediation(t *testing.T) {
	t.Parallel()

	issue := Issue{Code: "field_type_mismatch", Path: "note.md", Field: "status"}
	key, err := StableIssueKey(CheckOntology, issue)
	require.NoError(t, err)
	issue.Key = key
	next := BuildNextActions(Result{
		IssueCount: 1, ApplyCommand: "rzm agent validate fix ontology --apply",
		Checks: []CheckResult{{Name: CheckOntology, IssueCount: 1, Issues: []Issue{issue}, fullIssues: []Issue{issue}}},
		FixPlan: &FixPlan{AgentCount: 1, Actions: []FixAction{{
			Check: CheckOntology, IssueCode: issue.Code, Safety: FixSafetyAgent, IssueKeys: []string{key},
		}}},
	})

	require.Len(t, next.Actions, 1)
	require.Equal(t, "agent_required", next.Actions[0].Category)
	require.Empty(t, next.Actions[0].Command)
	require.Equal(t, []string{key}, next.Actions[0].IssueKeys)
	require.NotEmpty(t, next.Actions[0].NonFixableReason)
}

func TestNextActionJSONCarriesAdditiveIssueScopeAndReason(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal(NextAction{
		Category: "non_fixable", Title: "Inspect source", Message: "Inspect source",
		IssueKeys: []string{"issue:v1:one"}, NonFixableReason: "source cannot be read",
	})
	require.NoError(t, err)
	require.JSONEq(t, `{
		"category":"non_fixable",
		"title":"Inspect source",
		"message":"Inspect source",
		"issueKeys":["issue:v1:one"],
		"nonFixableReason":"source cannot be read"
	}`, string(payload))
}
