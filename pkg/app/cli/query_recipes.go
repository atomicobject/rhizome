package actions

import (
	"context"
	"fmt"
	"sort"
	"strings"

	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
)

type QueryRecipeListResponse struct {
	Recipes []queryrecipe.RecipeSummary `json:"recipes"`
	Issues  []queryrecipe.Issue         `json:"issues,omitempty"`
}

type QueryRecipeValidateResponse struct {
	OK           bool                                `json:"ok"`
	IssueCount   int                                 `json:"issueCount"`
	Recipes      []queryrecipe.Recipe                `json:"recipes"`
	Issues       []queryrecipe.Issue                 `json:"issues,omitempty"`
	Dependencies map[string]queryrecipe.Dependencies `json:"dependencies,omitempty"`
}

type QueryRecipeSelection struct {
	Recipe  queryrecipe.Recipe
	Recipes []queryrecipe.Recipe
	Issues  []queryrecipe.Issue
}

type QueryRecipeService struct {
	Load    func(path string) ([]queryrecipe.Recipe, []queryrecipe.Issue, error)
	Schema  func() (*ontologyquery.ExecutableSchema, error)
	Execute func(context.Context, string, map[string]any) (ontologyquery.Result, error)
}

func (s QueryRecipeService) List(path string) (QueryRecipeListResponse, error) {
	recipes, issues, err := s.Catalog(path)
	if err != nil {
		return QueryRecipeListResponse{}, err
	}
	return QueryRecipeListResponse{Recipes: queryrecipe.Summaries(recipes), Issues: issues}, nil
}

func (s QueryRecipeService) Catalog(path string) ([]queryrecipe.Recipe, []queryrecipe.Issue, error) {
	recipes, issues, err := s.Load(path)
	if err != nil {
		return nil, nil, err
	}
	sort.SliceStable(recipes, func(i, j int) bool { return recipes[i].ID < recipes[j].ID })
	return recipes, issues, nil
}

func (s QueryRecipeService) Find(path, id string) (QueryRecipeSelection, error) {
	recipes, issues, err := s.Catalog(path)
	if err != nil || len(issues) > 0 {
		return QueryRecipeSelection{Recipes: recipes, Issues: issues}, err
	}
	recipe, err := selectRecipe(recipes, id)
	return QueryRecipeSelection{Recipe: recipe, Recipes: recipes}, err
}

func (s QueryRecipeService) Validate(path string) (QueryRecipeValidateResponse, error) {
	recipes, issues, err := s.Catalog(path)
	if err != nil {
		return QueryRecipeValidateResponse{}, err
	}
	execSchema, err := s.Schema()
	if err != nil {
		return QueryRecipeValidateResponse{}, err
	}
	result := queryrecipe.Validate(recipes, execSchema)
	issues = append(issues, result.Issues...)
	return QueryRecipeValidateResponse{
		OK: len(issues) == 0, IssueCount: len(issues), Recipes: result.Recipes,
		Issues: issues, Dependencies: queryRecipeDependencies(result.Recipes, execSchema),
	}, nil
}

func (s QueryRecipeService) Run(ctx context.Context, path, id string, inputs map[string]string, anchors []string) (queryrecipe.RunResult, *QueryRecipeValidateResponse, error) {
	recipes, issues, err := s.Load(path)
	if err != nil {
		return queryrecipe.RunResult{}, nil, err
	}
	if len(issues) > 0 {
		validation := &QueryRecipeValidateResponse{OK: false, IssueCount: len(issues), Recipes: recipes, Issues: issues}
		return queryrecipe.RunResult{}, validation, nil
	}
	recipe, err := selectRecipe(recipes, id)
	if err != nil {
		return queryrecipe.RunResult{}, nil, err
	}
	inputs, err = queryrecipe.MergeAnchorInput(recipe, inputs, anchors)
	if err != nil {
		return queryrecipe.RunResult{}, nil, err
	}
	execSchema, err := s.Schema()
	if err != nil {
		return queryrecipe.RunResult{}, nil, err
	}
	compiled, recipeIssues := queryrecipe.Compile(recipe, execSchema, inputs)
	if len(recipeIssues) > 0 {
		validation := &QueryRecipeValidateResponse{OK: false, IssueCount: len(recipeIssues), Recipes: []queryrecipe.Recipe{recipe}, Issues: recipeIssues}
		return queryrecipe.RunResult{}, validation, nil
	}
	result, err := s.Execute(ctx, compiled.Query, compiled.Variables)
	if err != nil {
		return queryrecipe.RunResult{}, nil, err
	}
	return queryrecipe.RunResult{
		Recipe: recipe, Inputs: inputs, Variables: compiled.Variables, Query: compiled.Query,
		OutputContract: recipe.OutputContract, Result: result,
	}, nil, nil
}

func selectRecipe(recipes []queryrecipe.Recipe, id string) (queryrecipe.Recipe, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		if len(recipes) == 1 {
			return recipes[0], nil
		}
		return queryrecipe.Recipe{}, fmt.Errorf("--id is required when %d recipes are available", len(recipes))
	}
	for _, recipe := range recipes {
		if recipe.ID == id {
			return recipe, nil
		}
	}
	return queryrecipe.Recipe{}, fmt.Errorf("recipe %q not found", id)
}

func queryRecipeDependencies(recipes []queryrecipe.Recipe, execSchema *ontologyquery.ExecutableSchema) map[string]queryrecipe.Dependencies {
	out := map[string]queryrecipe.Dependencies{}
	for _, recipe := range recipes {
		inputs := map[string]string{}
		for _, input := range recipe.InputSpec.Inputs {
			if input.Default != "" {
				inputs[input.Name] = input.Default
			} else {
				inputs[input.Name] = "example"
			}
		}
		compiled, issues := queryrecipe.Compile(recipe, execSchema, inputs)
		if len(issues) == 0 && compiled != nil {
			out[recipe.ID] = compiled.Dependencies
		}
	}
	return out
}
