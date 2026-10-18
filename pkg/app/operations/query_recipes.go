package operations

import (
	"context"
	"sort"

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
	Execute func(context.Context, *ontologyquery.PreparedQuery) (ontologyquery.Result, error)
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
	recipes = append([]queryrecipe.Recipe(nil), recipes...)
	sort.SliceStable(recipes, func(i, j int) bool { return recipes[i].ID < recipes[j].ID })
	return recipes, issues, nil
}

func (s QueryRecipeService) Find(path, id string) (QueryRecipeSelection, error) {
	recipes, issues, err := s.Catalog(path)
	if err != nil {
		return QueryRecipeSelection{}, err
	}
	recipe, targetIssues, err := queryrecipe.Select(recipes, issues, id)
	return QueryRecipeSelection{Recipe: recipe, Recipes: recipes, Issues: targetIssues}, err
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
	// Select before loading the schema so selection failures remain runtime-free.
	if recipe, selectedIssues, err := queryrecipe.Select(recipes, issues, id); err != nil || len(selectedIssues) > 0 {
		if err != nil {
			return queryrecipe.RunResult{}, nil, err
		}
		return queryrecipe.RunResult{}, &QueryRecipeValidateResponse{IssueCount: len(selectedIssues), Recipes: []queryrecipe.Recipe{recipe}, Issues: selectedIssues}, nil
	}
	execSchema, err := s.Schema()
	if err != nil {
		return queryrecipe.RunResult{}, nil, err
	}
	run, recipeIssues, err := queryrecipe.Run(ctx, recipes, issues, id, inputs, anchors, execSchema, s.Execute)
	if len(recipeIssues) > 0 {
		return run, &QueryRecipeValidateResponse{IssueCount: len(recipeIssues), Recipes: []queryrecipe.Recipe{run.Recipe}, Issues: recipeIssues}, err
	}
	return run, nil, err
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
