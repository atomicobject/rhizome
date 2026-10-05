package actions

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/stretchr/testify/require"
)

func TestQueryRecipeServiceListSortsCatalog(t *testing.T) {
	service := QueryRecipeService{Load: func(string) ([]queryrecipe.Recipe, []queryrecipe.Issue, error) {
		return []queryrecipe.Recipe{{ID: "zeta"}, {ID: "alpha"}}, nil, nil
	}}
	result, err := service.List("")
	require.NoError(t, err)
	require.Equal(t, []string{"alpha", "zeta"}, []string{result.Recipes[0].ID, result.Recipes[1].ID})
}

func TestQueryRecipeAndCodeModeKeepTypedRootCoverage(t *testing.T) {
	schema := &ontology.Schema{
		Types:      map[string]*ontology.NoteType{"Project": {Name: "Project", Role: ontology.TypeRoleNote, ByName: map[string]*ontology.Field{}}},
		Interfaces: map[string]*ontology.InterfaceType{},
	}
	execSchema, err := ontologyquery.BuildExecutableSchema(schema)
	require.NoError(t, err)
	recipe := queryrecipe.Recipe{
		APIVersion: queryrecipe.APIVersion, ID: "projects", Name: "Projects", Problem: "List projects.",
		InputSpec:          queryrecipe.InputSpec{Mode: queryrecipe.InputModeNone},
		Query:              queryrecipe.QuerySpec{GraphQL: `{ project(first: 100) { path } }`},
		OutputContract:     queryrecipe.OutputContract{Empty: "No projects."},
		AdaptationGuidance: queryrecipe.AdaptationGuidance{Summary: "Review coverage."},
	}
	service := QueryRecipeService{
		Load: func(string) ([]queryrecipe.Recipe, []queryrecipe.Issue, error) {
			return []queryrecipe.Recipe{recipe}, nil, nil
		},
		Schema: func() (*ontologyquery.ExecutableSchema, error) { return execSchema, nil },
		Execute: func(context.Context, string, map[string]any) (ontologyquery.Result, error) {
			return ontologyquery.Result{Data: map[string]any{"project": []any{}}, Extensions: map[string]any{
				"typedRoots": map[string]any{"project": map[string]any{"status": "capped", "nextOffset": 100}},
			}}, nil
		},
	}
	run, validation, err := service.Run(context.Background(), "", "projects", nil, nil)
	require.NoError(t, err)
	require.Nil(t, validation)
	encoded, err := json.Marshal(run)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"extensions":{"typedRoots":{"project":{"nextOffset":100,"status":"capped"}}}`)

	outcome := (CodeModeQueryRecipeService{Recipes: service}).Call(context.Background(), map[string]any{"action": "run", "id": "projects"})
	require.True(t, outcome.OK)
	encoded, err = json.Marshal(outcome.Payload)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"extensions":{"typedRoots":{"project":{"nextOffset":100,"status":"capped"}}}`)
}

func TestQueryRecipeServiceRunRequiresIDForMultipleRecipes(t *testing.T) {
	service := QueryRecipeService{Load: func(string) ([]queryrecipe.Recipe, []queryrecipe.Issue, error) {
		return []queryrecipe.Recipe{{ID: "alpha"}, {ID: "beta"}}, nil, nil
	}}
	_, validation, err := service.Run(context.Background(), "", "", nil, nil)
	require.Nil(t, validation)
	require.EqualError(t, err, "--id is required when 2 recipes are available")
}
