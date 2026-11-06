package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/stretchr/testify/require"
)

func TestPublicQueryRecipeExecutionChoosesPreparedQueryGate(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	recipeDir := filepath.Join(fixture.root, ".rhizome", "query-recipes")
	require.NoError(t, os.MkdirAll(recipeDir, 0o755))
	writeRecipe := func(name, id, selection string) {
		t.Helper()
		body := fmt.Sprintf(`apiVersion: rhizome.query-recipe.v1
id: %s
name: %s
problem: Verify prepared-query readiness routing.
inputSpec:
  mode: required_anchor
  primaryInput: path
  inputs:
    - name: path
      required: true
      kind: path
query:
  graphQL: |
    query RecipeRead($path: String!) {
      note(path: $path) { %s }
    }
outputContract:
  expectedPaths: [note.path]
  empty: No note found.
adaptationGuidance:
  summary: Use the selected note.
`, id, name, selection)
		require.NoError(t, os.WriteFile(filepath.Join(recipeDir, id+".yaml"), []byte(body), 0o644))
	}
	writeRecipe("Exact note recipe", "exact-note-recipe", "path title")
	writeRecipe("Type-dependent note recipe", "typed-note-recipe", "__typename path")

	runtime := &Runtime{IntelStore: fixture.intelStore}
	runtime.EnableIndexGate()
	// An unavailable gate fails immediately; this test checks routing, not elapsed time.
	runtime.indexReady = nil
	runtime.MarkNoteReadReady()
	srv := newFixtureServer(t, fixture, runtime)
	require.False(t, runtime.IndexReady())

	exactReq := newApplicationRequest(http.MethodPost, "/api/v1/query-recipes/exact-note-recipe/execute", bytes.NewBufferString(`{"inputs":{"path":"specs/100-demo/plan.md"}}`))
	exactReq.Header.Set("Content-Type", "application/json")
	exactRes := httptest.NewRecorder()
	srv.Handler().ServeHTTP(exactRes, exactReq)
	require.Equal(t, http.StatusOK, exactRes.Code)
	var exactRun queryrecipe.RunResult
	require.NoError(t, json.Unmarshal(exactRes.Body.Bytes(), &exactRun))
	require.Empty(t, exactRun.Result.Errors)
	require.Equal(t, "specs/100-demo/plan.md", exactRun.Result.Data["note"].(map[string]any)["path"])

	typedReq := newApplicationRequest(http.MethodPost, "/api/v1/query-recipes/typed-note-recipe/execute", bytes.NewBufferString(`{"inputs":{"path":"specs/100-demo/plan.md"}}`))
	typedReq.Header.Set("Content-Type", "application/json")
	typedRes := httptest.NewRecorder()
	srv.Handler().ServeHTTP(typedRes, typedReq)
	require.Equal(t, http.StatusServiceUnavailable, typedRes.Code)
	require.Equal(t, "5", typedRes.Header().Get("Retry-After"))
	require.Contains(t, typedRes.Body.String(), PublicErrorIndexInitializing)
}
