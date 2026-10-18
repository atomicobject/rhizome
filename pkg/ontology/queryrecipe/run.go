package queryrecipe

import (
	"context"
	"errors"
	"fmt"
	"strings"

	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
)

var ErrNotFound = errors.New("not found")

// Select rejects ambiguous targets and target-specific load diagnostics.
// Unrelated broken recipes remain visible in the catalog without blocking runs.
func Select(recipes []Recipe, loadIssues []Issue, id string) (Recipe, []Issue, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		if len(recipes) != 1 {
			return Recipe{}, nil, fmt.Errorf("id is required when %d recipes are available", len(recipes))
		}
		id = strings.TrimSpace(recipes[0].ID)
	}
	var matches []Recipe
	for _, recipe := range recipes {
		if strings.TrimSpace(recipe.ID) == id {
			matches = append(matches, recipe)
		}
	}
	var issues []Issue
	for _, issue := range loadIssues {
		if issue.Recipe == id {
			issues = append(issues, issue)
		}
	}
	if len(matches) == 0 {
		if len(issues) > 0 {
			return Recipe{}, issues, nil
		}
		return Recipe{}, nil, fmt.Errorf("recipe %q %w", id, ErrNotFound)
	}
	recipe := matches[0]
	if len(matches) > 1 {
		issues = append(issues, recipeIssue(recipe, "duplicate_recipe_id", fmt.Sprintf("recipe %q is declared %d times", id, len(matches)), "id"))
	}
	for _, issue := range loadIssues {
		if issue.Recipe == "" && issue.Path != "" && issue.Path == recipe.Source.Path {
			issues = append(issues, issue)
		}
	}
	return recipe, issues, nil
}

func Run(ctx context.Context, recipes []Recipe, loadIssues []Issue, id string, inputs map[string]string, anchors []string, exec *ontologyquery.ExecutableSchema, execute func(context.Context, *ontologyquery.PreparedQuery) (ontologyquery.Result, error)) (RunResult, []Issue, error) {
	recipe, issues, err := Select(recipes, loadIssues, id)
	run := RunResult{Recipe: recipe, OutputContract: recipe.OutputContract}
	if err != nil || len(issues) > 0 {
		return run, issues, err
	}
	if issues := validateMetadata(recipe); len(issues) > 0 {
		return run, issues, nil
	}
	inputs, err = MergeAnchorInput(recipe, inputs, anchors)
	if err != nil {
		return run, nil, err
	}
	compiled, issues := Compile(recipe, exec, inputs)
	if len(issues) > 0 {
		return run, issues, nil
	}
	run.Inputs, run.Variables, run.Query = inputs, compiled.Variables, compiled.Query
	if err := ctx.Err(); err != nil {
		return run, nil, err
	}
	run.Result, err = execute(ctx, compiled.Prepared)
	return run, nil, err
}
