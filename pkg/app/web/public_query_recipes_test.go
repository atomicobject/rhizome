package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestPublicQueryRecipeSharesTargetDiagnosticsAndRefreshesRegistry(t *testing.T) {
	root := t.TempDir()
	ontologyDir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(ontologyDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(ontologyDir, "schema.graphql"), []byte(`type Project @node(paths:["projects/*.md"]) { name:String }`), 0o600))
	dir := filepath.Join(root, ".rhizome", "query-recipes")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	recipe := []byte(`apiVersion: rhizome.query-recipe.v1
id: target
name: Target
problem: Inspect schema.
inputSpec: {mode: none}
query: {graphQL: 'query { __schema { queryType { name } } }'}
outputContract: {empty: No schema.}
adaptationGuidance: {summary: Inspect schema.}
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "target.yaml"), recipe, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.yaml"), []byte("invalid: ["), 0o600))
	srv := &Server{cfg: Config{VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}}}
	call := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		srv.handlePublicQueryRecipeExecute(response, httptest.NewRequest(http.MethodPost, "/api/v1/query-recipes/target/execute", nil))
		return response
	}
	response := call()
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var run queryrecipe.RunResult
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &run))
	require.NotEmpty(t, run.Result.Data["__schema"])
	duplicate := filepath.Join(dir, "duplicate.yaml")
	require.NoError(t, os.WriteFile(duplicate, recipe, 0o600))
	response = call()
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "duplicate_recipe_id")
	require.NoError(t, os.Rename(duplicate, filepath.Join(t.TempDir(), "duplicate.yaml")))
	response = call()
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
}

func TestPublicOntologyQueryUsesSharedRuntimeCapabilities(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`type Project @node(paths:["projects/*.md"]) {name:String}`), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "thing.go"), []byte("package pkg\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "thing_test.go"), []byte("package pkg\n"), 0o600))
	store, err := semdb.Open(filepath.Join(root, "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	srv := &Server{cfg: Config{VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}}, runtime: &Runtime{IntelStore: store}}
	result, err := srv.executeOntologyQuery(context.Background(), `{
 ontology {authoringGuide(type:"Project") {available}}
 code {testsForCode(path:"pkg/thing.go") {available tests {path}}}
 }`)
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	require.Equal(t, true, result.Data["ontology"].(map[string]any)["authoringGuide"].(map[string]any)["available"])
	code := result.Data["code"].(map[string]any)["testsForCode"].(map[string]any)
	require.Equal(t, true, code["available"])
	require.Equal(t, []any{map[string]any{"path": "pkg/thing_test.go"}}, code["tests"])
}
