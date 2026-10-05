package validate

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/stretchr/testify/require"
)

func TestBuildQueryRecipeFixesKeepsCompileDiagnosticsDistinct(t *testing.T) {
	issues := []queryrecipe.Issue{
		{Code: "query_compile_error", Path: "recipes/a.yaml", Recipe: "a", Field: "query.graphQL", Line: 17, Message: "unknown field alpha"},
		{Code: "query_compile_error", Path: "recipes/a.yaml", Recipe: "a", Field: "query.graphQL", Line: 17, Message: "unknown field beta"},
	}
	validationIssues := make([]Issue, 0, len(issues))
	for _, issue := range issues {
		validationIssues = append(validationIssues, queryRecipeValidationIssue(issue))
	}
	fixes, err := buildQueryRecipeFixes(issues)
	require.NoError(t, err)
	check := CheckResult{Name: CheckQueryRecipes, Issues: validationIssues, Fixes: fixes}

	keyed, err := attachStableRepairIssueKeys([]CheckResult{check})
	require.NoError(t, err)
	require.Len(t, keyed[0].Issues, 2)
	require.NotEqual(t, keyed[0].Issues[0].Key, keyed[0].Issues[1].Key)
	require.NotEqual(t, keyed[0].Fixes[0].ID, keyed[0].Fixes[1].ID)
	require.Equal(t, []string{keyed[0].Issues[0].Key}, keyed[0].Fixes[0].IssueKeys)
	require.Equal(t, []string{keyed[0].Issues[1].Key}, keyed[0].Fixes[1].IssueKeys)

	var firstData QueryRecipeIssueData
	require.NoError(t, json.Unmarshal(keyed[0].Issues[0].Data, &firstData))
	require.Equal(t, QueryRecipeIssueData{
		Recipe: "a", Line: 17, Diagnostic: "unknown field alpha",
	}, firstData)

	plan, err := BuildRepairPlan(context.Background(), RunContext{}, keyed)
	require.NoError(t, err)
	require.Len(t, plan.Actions, 2)
}

func TestBuildQueryRecipeFixesPreservesUnderlyingIssueCodes(t *testing.T) {
	actions, err := buildQueryRecipeFixes([]queryrecipe.Issue{
		{Code: "unsupported_api_version", Path: "recipes/b.yaml", Recipe: "b", Field: "apiVersion", Message: "version drift"},
		{Code: "query_compile_error", Path: "recipes/a.yaml", Recipe: "a", Field: "query.graphQL", Message: "unknown field"},
		{Code: "missing_output_path", Path: "recipes/a.yaml", Recipe: "a", Field: "outputContract.expectedPaths", Message: "missing path"},
	})
	require.NoError(t, err)

	require.Len(t, actions, 3)
	require.Equal(t, []string{"missing_output_path", "query_compile_error", "unsupported_api_version"}, []string{
		actions[0].IssueCode, actions[1].IssueCode, actions[2].IssueCode,
	})
	for _, action := range actions {
		require.Equal(t, "adapt_query_recipe", action.Kind)
		require.Equal(t, FixSafetyAgent, action.Safety)
		require.Equal(t, 1, action.InstanceCount)
		require.Len(t, action.AffectedPaths, 1)
		require.Contains(t, action.Summary, action.IssueCode)
	}
}
