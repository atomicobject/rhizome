package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/stretchr/testify/require"
)

func TestRenderRecipeListHuman_Empty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, renderRecipeListHuman(&buf, nil, nil))
	out := buf.String()
	require.Contains(t, out, "No saved query recipes found")
	require.Contains(t, out, ".rhizome/query-recipes/")
}

func TestRenderRecipeListHuman_Table(t *testing.T) {
	recipes := []queryrecipe.Recipe{
		{
			ID:        "alpha",
			Name:      "Alpha recipe",
			Problem:   "  load\ta single\nanchored note  ",
			InputSpec: queryrecipe.InputSpec{Mode: queryrecipe.InputModeRequiredAnchor},
		},
		{
			ID:        "beta",
			Name:      "Beta recipe",
			Problem:   strings.Repeat("x", 81),
			InputSpec: queryrecipe.InputSpec{Mode: queryrecipe.InputModeNone},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, renderRecipeListHuman(&buf, recipes, nil))
	out := buf.String()
	require.Contains(t, out, "ID")
	require.Contains(t, out, "MODE")
	require.Contains(t, out, "alpha")
	require.Contains(t, out, "beta")
	require.Contains(t, out, "required_anchor")
	require.Regexp(t, `(?m)^alpha\s+required_anchor\s+Alpha recipe\s+load a single anchored note$`, out)
	require.Regexp(t, `(?m)^beta\s+none\s+Beta recipe\s+`+strings.Repeat("x", 79)+`…$`, out)
	require.Contains(t, out, "2 recipe(s)")
}

func TestRenderRecipeValidateHuman_NoIssues(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, renderRecipeValidateHuman(&buf, []queryrecipe.Recipe{{ID: "x"}}, nil))
	require.Contains(t, buf.String(), "OK — 1 recipe(s) validated")
}

func TestRenderRecipeValidateHuman_GroupsIssuesPerRecipe(t *testing.T) {
	recipes := []queryrecipe.Recipe{{ID: "a"}, {ID: "b"}}
	issues := []queryrecipe.Issue{
		{Recipe: "a", Code: "missing_required_field", Message: "id is required", Field: "id"},
		{Recipe: "a", Code: "query_compile_error", Message: "Cannot query field foo", Path: "x.yaml", Line: 10, Field: "query.graphQL"},
		{Recipe: "b", Code: "duplicate_recipe_id", Message: "recipe id already declared"},
		{Code: "recipe_path_error", Message: "yaml decode failed", Path: "y.yaml"},
	}
	var buf bytes.Buffer
	require.NoError(t, renderRecipeValidateHuman(&buf, recipes, issues))
	out := buf.String()
	require.Contains(t, out, "Load issues")
	require.Contains(t, out, "yaml decode failed")
	require.Contains(t, out, "[a]")
	require.Contains(t, out, "[b]")
	require.Contains(t, out, "missing_required_field")
	require.Contains(t, out, "x.yaml:10")
	require.Contains(t, out, "field query.graphQL")
	require.Contains(t, out, "FAIL — 4 issue(s)")
}

func TestRenderRecipeShowHuman_RenderingAndInvocation(t *testing.T) {
	recipe := queryrecipe.Recipe{
		ID:      "spec-by-path",
		Name:    "Spec by path",
		Problem: "Load one spec and its neighborhood.",
		InputSpec: queryrecipe.InputSpec{
			Mode:         queryrecipe.InputModeRequiredAnchor,
			PrimaryInput: "path",
			Inputs: []queryrecipe.Input{
				{Name: "path", Required: true, Kind: "string", Description: "Vault-relative spec path."},
				{Name: "first", Kind: "int", Default: "10", Description: "Result limit"},
			},
		},
		Query: queryrecipe.QuerySpec{GraphQL: "query Spec($path: String!) { technicalSpec(path: $path) { path } }\n"},
		OutputContract: queryrecipe.OutputContract{
			ExpectedPaths: []string{"technicalSpec.path"},
			Empty:         "Spec did not resolve.",
			Partial:       "Read partial.",
			HighVolume:    "Stay anchored.",
		},
		AdaptationGuidance: queryrecipe.AdaptationGuidance{
			Summary: "Preserve single-anchor lookup.",
			Rules:   []string{"Keep path as the primary input"},
		},
		Examples: []queryrecipe.Example{{Name: "demo", Inputs: map[string]string{"path": "docs/specs/x.md"}}},
		Tags:     []string{"spec-driven", "ontology"},
		Source:   queryrecipe.Source{Path: "/tmp/recipe.yaml", Line: 1},
	}
	var buf bytes.Buffer
	require.NoError(t, renderRecipeShowHuman(&buf, recipe))
	out := buf.String()
	require.Contains(t, out, "# Spec by path (spec-by-path)")
	require.Contains(t, out, "Mode: required_anchor")
	require.Contains(t, out, "Primary input: path")
	require.Contains(t, out, "Tags: spec-driven, ontology")
	require.Contains(t, out, "Source: /tmp/recipe.yaml:1")
	require.Contains(t, out, "## Problem")
	require.Contains(t, out, "## Inputs")
	require.Contains(t, out, "## GraphQL")
	require.Contains(t, out, "```graphql")
	require.Contains(t, out, "## Output contract")
	require.Contains(t, out, "Expected paths:")
	require.Contains(t, out, "## Adaptation guidance")
	require.Contains(t, out, "Preserve single-anchor lookup.")
	require.Contains(t, out, "## Examples")
	require.Contains(t, out, "## Run")
	// Synthesized invocation should reuse the example anchor and skip the
	// primary input from the --input list.
	require.Contains(t, out, "rzm query-recipe run --id spec-by-path --anchor docs/specs/x.md --input first=10")
	require.NotContains(t, out, "--input path=")
	for _, tc := range []struct {
		recipe     queryrecipe.Recipe
		invocation string
	}{
		{queryrecipe.Recipe{ID: "b", InputSpec: queryrecipe.InputSpec{Mode: queryrecipe.InputModeNone}}, "rzm query-recipe run --id b"},
		{queryrecipe.Recipe{
			ID: "c",
			InputSpec: queryrecipe.InputSpec{
				Mode:         queryrecipe.InputModeRequiredAnchor,
				PrimaryInput: "path",
				Inputs:       []queryrecipe.Input{{Name: "path", Required: true, Kind: "string"}},
			},
		}, "rzm query-recipe run --id c --anchor <path>"},
	} {
		buf.Reset()
		require.NoError(t, renderRecipeShowHuman(&buf, tc.recipe))
		require.Contains(t, strings.Split(buf.String(), "\n"), "  "+tc.invocation)
	}
}

func TestRenderRecipeRunSummary_StructureAndKeys(t *testing.T) {
	run := queryrecipe.RunResult{
		Recipe: queryrecipe.Recipe{
			ID:        "demo",
			Name:      "Demo recipe",
			InputSpec: queryrecipe.InputSpec{Mode: queryrecipe.InputModeRequiredAnchor},
		},
		Inputs: map[string]string{"path": "docs/specs/x.md"},
	}
	run.Result.Data = map[string]any{
		"technicalSpec": []any{map[string]any{"path": "docs/specs/x.md"}},
		"openQuestion":  nil,
	}
	var buf bytes.Buffer
	renderRecipeRunSummary(&buf, run)
	out := buf.String()
	require.Contains(t, out, "Recipe: Demo recipe (demo)")
	require.Contains(t, out, "Mode: required_anchor")
	require.Contains(t, out, "path = docs/specs/x.md")
	require.Contains(t, out, "technicalSpec -> array(1)")
	require.Contains(t, out, "openQuestion -> null")
}
