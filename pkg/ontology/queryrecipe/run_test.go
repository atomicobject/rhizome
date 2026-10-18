package queryrecipe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/stretchr/testify/require"
)

const runRecipeYAML = `apiVersion: rhizome.query-recipe.v1
id: target
name: Target
problem: Inspect the query schema.
inputSpec:
  mode: none
query:
  graphQL: 'query { __schema { queryType { name } } }'
outputContract:
  empty: No schema.
adaptationGuidance:
  summary: Inspect the schema.
`

func TestRunRejectsDuplicateIDAndIgnoresUnrelatedLoadIssues(t *testing.T) {
	root := t.TempDir()
	write := func(name, data string) {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(data), 0o600))
	}
	write("target.yaml", runRecipeYAML)
	write("broken.yaml", "apiVersion: rhizome.query-recipe.v1\nquery: [invalid")
	// An unrelated duplicate must not block the selected target.
	write("other.yaml", "apiVersion: rhizome.query-recipe.v1\nid: other\n---\napiVersion: rhizome.query-recipe.v1\nid: other\n")
	recipes, issues := LoadPath(root)
	require.Len(t, issues, 1)
	require.Len(t, recipes, 3)
	exec := testExecutableSchema(t)
	calls := 0
	execute := func(ctx context.Context, prepared *ontologyquery.PreparedQuery) (ontologyquery.Result, error) {
		calls++
		return ontologyquery.ExecutePrepared(ctx, ontologyquery.Deps{}, nil, exec, prepared), nil
	}
	run, diagnostics, err := Run(context.Background(), recipes, issues, "target", nil, nil, exec, execute)
	require.NoError(t, err)
	require.Empty(t, diagnostics)
	require.Empty(t, run.Result.Errors)
	require.NotEmpty(t, run.Result.Data["__schema"])
	require.Equal(t, 1, calls)
	write("duplicate.yaml", runRecipeYAML)
	recipes, issues = LoadPath(root)
	_, diagnostics, err = Run(context.Background(), recipes, issues, "target", nil, nil, exec, execute)
	require.NoError(t, err)
	require.Len(t, diagnostics, 1)
	require.Equal(t, "duplicate_recipe_id", diagnostics[0].Code)
	require.Equal(t, 1, calls)
}

func TestRunRejectsTargetLoadAndMetadataIssuesAndPreservesExecutionError(t *testing.T) {
	recipes, issues := decodeRecipeYAMLDocuments([]byte(runRecipeYAML), Source{Path: "target.yaml"})
	require.Empty(t, issues)
	exec := testExecutableSchema(t)
	failure := errors.New("readiness unavailable")
	execute := func(context.Context, *ontologyquery.PreparedQuery) (ontologyquery.Result, error) {
		return ontologyquery.Result{}, failure
	}
	_, issues, err := Run(context.Background(), recipes, []Issue{{Recipe: "target", Code: "recipe_parse_error"}}, "target", nil, nil, exec, execute)
	require.NoError(t, err)
	require.Equal(t, "recipe_parse_error", issues[0].Code)
	_, issues, err = Run(context.Background(), recipes, nil, "target", nil, nil, exec, execute)
	require.ErrorIs(t, err, failure)
	require.Empty(t, issues)
	recipes[0].APIVersion = "rhizome.ontology-query-recipe.v1"
	_, issues, err = Run(context.Background(), recipes, nil, "target", nil, nil, exec, execute)
	require.NoError(t, err)
	require.Equal(t, "unsupported_api_version", issues[0].Code)
}

func TestRegistryRefreshesRewritesAdditionsAndDeletions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "target.yaml")
	require.NoError(t, os.WriteFile(path, []byte(runRecipeYAML), 0o600))
	registry := NewRegistry([]string{root, root})
	recipes, issues := registry.Catalog()
	require.Empty(t, issues)
	require.Len(t, recipes, 1)
	info, err := os.Stat(path)
	require.NoError(t, err)
	// Preserve size and timestamps. A metadata-only cache would return stale text.
	changed := []byte(runRecipeYAML)
	for i := range changed {
		if i+6 <= len(changed) && string(changed[i:i+6]) == "Target" {
			copy(changed[i:], "Edited")
			break
		}
	}
	require.NoError(t, os.WriteFile(path, changed, 0o600))
	require.NoError(t, os.Chtimes(path, info.ModTime(), info.ModTime()))
	recipes, issues = registry.Catalog()
	require.Empty(t, issues)
	require.Equal(t, "Edited", recipes[0].Name)
	dir := filepath.Join(root, "nested")
	require.NoError(t, os.Mkdir(dir, 0o700))
	nested := filepath.Join(dir, "extra.yaml")
	require.NoError(t, os.WriteFile(nested, []byte("apiVersion: rhizome.query-recipe.v1\nid: extra\n"), 0o600))
	recipes, issues = registry.Catalog()
	require.Empty(t, issues)
	require.Len(t, recipes, 2)
	// Rename out of the source root models deletion without removing user files.
	require.NoError(t, os.Rename(nested, filepath.Join(t.TempDir(), "extra.yaml")))
	recipes, issues = registry.Catalog()
	require.Empty(t, issues)
	require.Len(t, recipes, 1)
}

func TestDefaultSourcesReportsStaleAPIVersionInSkillYAML(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agents", "skills", "example", "recipe.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("apiVersion: 'rhizome.ontology-query-recipe.v1'\nid: stale\n"), 0o600))
	recipes, issues := LoadDefaultSources(root)
	require.Empty(t, issues)
	require.Len(t, recipes, 1)
	require.Equal(t, "unsupported_api_version", Validate(recipes, nil).Issues[0].Code)
}
