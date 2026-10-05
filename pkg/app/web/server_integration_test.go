package web

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/indexing"
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"
	"github.com/atomicobject/rhizome/pkg/app/validationproduct"
	appviews "github.com/atomicobject/rhizome/pkg/app/views"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
	"github.com/stretchr/testify/require"
)

func TestServerFixtureEndpoints(t *testing.T) {
	t.Parallel()

	fixture := prepareWebFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var status StatusResponse
	getJSON(t, httpSrv.URL+"/api/v1/status", &status)
	require.True(t, status.CodeIndex)
	require.Equal(t, fixture.root, status.VaultPath)

	var tree TreeResponse
	getJSON(t, httpSrv.URL+"/api/v1/files/tree", &tree)
	require.NotEmpty(t, tree.Entries)

	var note FileViewResponse
	getJSON(t, httpSrv.URL+"/api/v1/files/view?path=notes/task-flow.md", &note)
	require.Equal(t, "note", note.Kind)
	require.NotEmpty(t, note.Content)
	require.NotEmpty(t, note.Links)

	var code FileViewResponse
	getJSON(t, httpSrv.URL+"/api/v1/files/view?path=src/todoapp/main.py", &code)
	require.Equal(t, "code", code.Kind)
	require.NotEmpty(t, code.Content)
	require.NotEmpty(t, code.RelatedNotes)

	var graph GraphResponse
	getJSON(t, httpSrv.URL+"/api/v1/graphs/global", &graph)
	require.False(t, graph.NeedsIndex)
	require.NotEmpty(t, graph.Nodes)
	require.NotEmpty(t, graph.Edges)
}

func TestSearchHTTPMatchesInteractiveApplicationAdapter(t *testing.T) {
	fixture := prepareWebFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	query := "calculate total"
	var httpResult SearchResponse
	getJSON(t, httpSrv.URL+"/api/v1/search?q="+url.QueryEscape(query)+"&scope=code&limit=1&budget=24000", &httpResult)
	direct, err := agentapi.SemanticQueryUnifiedWithOptions(context.Background(), srv.semanticConfig(), agentapi.SemanticQueryOptions{
		Profile: searchapplication.ProfileInteractive, Query: query, Scope: "code", Limit: 1, BudgetChars: 24000,
	})
	require.NoError(t, err)
	require.NotEmpty(t, direct.Matches)
	require.NotEmpty(t, httpResult.Matches)
	directIdentities := make([]string, len(direct.Matches))
	for i, match := range direct.Matches {
		if match.NodeRefJSON != "" {
			directIdentities[i] = "node:" + match.NodeRefJSON
		} else if match.Type == "code" && match.FQN != "" {
			directIdentities[i] = "code:" + match.Path + "\x00" + match.FQN
		} else {
			directIdentities[i] = match.Type + ":" + match.Path
		}
	}
	require.Equal(t, directIdentities, webSearchIdentities(httpResult.Matches))
	require.Equal(t, direct.Total, httpResult.Total)
	require.Equal(t, direct.Returned, httpResult.Returned)
	require.Equal(t, direct.Profile, httpResult.Profile)
	require.Equal(t, direct.Policy.Profile, httpResult.Policy.Profile)
	require.Equal(t, direct.Policy.VisibleLimit, httpResult.Policy.VisibleLimit)
	require.Equal(t, direct.Policy.CandidateWindow, httpResult.Policy.CandidateWindow)
	require.Equal(t, direct.Policy.MaxPerOwner, httpResult.Policy.MaxPerOwner)
	require.Equal(t, direct.Policy.BudgetChars, httpResult.Policy.BudgetChars)
	require.Equal(t, direct.Policy.DeadlineMillis, httpResult.Policy.DeadlineMillis)
	require.Equal(t, direct.TargetStatus, httpResult.TargetStatus)
	require.Equal(t, direct.Confidence, httpResult.Confidence)
	require.Equal(t, direct.Coverage, httpResult.Coverage)
	require.Greater(t, direct.Total, direct.Returned)
	require.NotEmpty(t, direct.ContinuationToken)
	require.Equal(t, direct.ContinuationToken, httpResult.ContinuationToken)

	var httpNext SearchResponse
	getJSON(t, httpSrv.URL+"/api/v1/search?q="+url.QueryEscape(query)+"&continuationToken="+url.QueryEscape(httpResult.ContinuationToken)+"&scope=code&limit=1&budget=24000", &httpNext)
	directNext, err := agentapi.SemanticQueryUnifiedWithOptions(context.Background(), srv.semanticConfig(), agentapi.SemanticQueryOptions{
		Profile: searchapplication.ProfileInteractive, Query: query, ContinuationToken: direct.ContinuationToken, Scope: "code", Limit: 1, BudgetChars: 24000,
	})
	require.NoError(t, err)
	require.NotEmpty(t, directNext.Matches)
	directNextIdentities := make([]string, len(directNext.Matches))
	for i, match := range directNext.Matches {
		if match.NodeRefJSON != "" {
			directNextIdentities[i] = "node:" + match.NodeRefJSON
		} else if match.Type == "code" && match.FQN != "" {
			directNextIdentities[i] = "code:" + match.Path + "\x00" + match.FQN
		} else {
			directNextIdentities[i] = match.Type + ":" + match.Path
		}
	}
	require.Equal(t, directNextIdentities, webSearchIdentities(httpNext.Matches))
	require.Equal(t, directNext.TargetStatus, httpNext.TargetStatus)
	require.Equal(t, directNext.Confidence, httpNext.Confidence)
	require.Equal(t, directNext.Coverage, httpNext.Coverage)
}

func webSearchIdentities(matches []SearchMatch) []string {
	out := make([]string, len(matches))
	for i, match := range matches {
		if match.NodeRefJSON != "" {
			out[i] = "node:" + match.NodeRefJSON
		} else if match.Type == "code" && match.FQN != "" {
			out[i] = "code:" + match.Path + "\x00" + match.FQN
		} else {
			out[i] = match.Type + ":" + match.Path
		}
	}
	return out
}

func TestDuplicateUnversionedPublicAliasesAreNotRegistered(t *testing.T) {
	t.Parallel()

	fixture := prepareWebFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	aliases := []string{
		"/api/status",
		"/api/suggest?q=task",
		"/api/search?q=task",
		"/api/search/notes?q=task",
		"/api/graph/global",
		"/api/graph/local?path=notes/task-flow.md",
		"/api/graph/expand?path=notes/task-flow.md",
		"/api/file/tree",
		"/api/file/view?path=notes/task-flow.md",
		"/api/file/rendered?path=notes/task-flow.md",
		"/api/ontology/summary",
		"/api/ontology/atlas",
		"/api/ontology/types",
		"/api/ontology/inspect?path=notes/task-flow.md",
		"/api/ontology/query-schema",
		"/api/ontology/query",
		"/api/ontology/edit",
		"/api/ontology/edit-sessions",
		"/api/ontology/nodes/resolve",
		"/api/ontology/events",
		"/api/validate",
		"/api/events",
	}

	for _, alias := range aliases {
		resp, err := http.Get(httpSrv.URL + alias)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, http.StatusNotFound, resp.StatusCode, alias)
	}
}

func TestHandleOpenAPIYAMLUsesEmbeddedSpecOutsideRepoCWD(t *testing.T) {
	t.Chdir(t.TempDir())

	req := newApplicationRequest(http.MethodGet, "/openapi.yaml", nil)
	rec := httptest.NewRecorder()
	(&Server{}).handleOpenAPIYAML(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/yaml")
	require.Contains(t, rec.Body.String(), "Rhizome Public REST API")
}

func TestPublicQueryRecipeExecuteSupportsIntrospection(t *testing.T) {
	t.Parallel()

	fixture := prepareOntologyFixtureVault(t)
	require.NoError(t, os.MkdirAll(filepath.Join(fixture.root, ".rhizome", "query-recipes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, ".rhizome", "query-recipes", "introspection.yaml"), []byte(`apiVersion: rhizome.query-recipe.v1
id: public-schema-introspection
name: Public schema introspection
problem: Load public GraphQL schema type names through a recipe.
inputSpec:
  mode: none
query:
  graphQL: |
    query PublicSchemaIntrospection {
      __schema {
        queryType { name }
      }
    }
outputContract:
  expectedPaths: ["__schema.queryType.name"]
  empty: No schema found.
`), 0o644))

	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var run queryrecipe.RunResult
	postJSON(t, httpSrv.URL+"/api/v1/query-recipes/public-schema-introspection/execute", PublicQueryRecipeExecuteRequest{}, &run)

	require.Empty(t, run.Result.Errors)
	schemaData, ok := run.Result.Data["__schema"].(map[string]any)
	require.True(t, ok)
	queryType, ok := schemaData["queryType"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "Query", queryType["name"])
}

func TestPublicQueryRecipeExecuteReturnsPublicCompileError(t *testing.T) {
	t.Parallel()

	fixture := prepareOntologyFixtureVault(t)
	require.NoError(t, os.MkdirAll(filepath.Join(fixture.root, ".rhizome", "query-recipes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, ".rhizome", "query-recipes", "invalid.yaml"), []byte(`apiVersion: rhizome.query-recipe.v1
id: invalid-public-query
name: Invalid public query
problem: Exercise public query recipe compile failures.
inputSpec:
  mode: none
query:
  graphQL: |
    query InvalidPublicQuery {
      noSuchPublicField
    }
outputContract:
  expectedPaths: ["noSuchPublicField"]
  empty: No result.
`), 0o644))

	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	resp, err := http.Post(httpSrv.URL+"/api/v1/query-recipes/invalid-public-query/execute", "application/json", bytes.NewReader(nil))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var body ErrorResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, PublicErrorRecipeCompileFailed, body.Code)
	details, ok := body.Details.(map[string]any)
	require.True(t, ok)
	require.NotEmpty(t, details["issues"])
}

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
	typedCtx, typedCancel := context.WithTimeout(typedReq.Context(), 10*time.Millisecond)
	defer typedCancel()
	typedRes := httptest.NewRecorder()
	srv.Handler().ServeHTTP(typedRes, typedReq.WithContext(typedCtx))
	require.Equal(t, http.StatusServiceUnavailable, typedRes.Code)
	require.Equal(t, "5", typedRes.Header().Get("Retry-After"))
	require.Contains(t, typedRes.Body.String(), PublicErrorIndexInitializing)
}

func TestPublicConfiguredViews(t *testing.T) {
	t.Parallel()

	fixture := prepareOntologyFixtureVault(t)
	require.NoError(t, os.MkdirAll(filepath.Join(fixture.root, ".rhizome", "views"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, ".rhizome", "views", "public.yaml"), []byte(`apiVersion: rhizome.view.v1
id: public.plans
name: Public plans
source:
  kind: ontology_type
  type: Plan
mount:
  kind: standalone
defaults:
  variant: table
variants:
  table:
    columns:
      - field: title
      - field: path
      - field: summary
  card:
    title: title
    preview: summary
    fields: []
  kanban:
    columnField: summary
`), 0o644))

	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var catalog appviews.Catalog
	getJSON(t, httpSrv.URL+"/api/v1/views", &catalog)
	require.NotEmpty(t, catalog.Views)
	require.Contains(t, viewIDs(catalog.Views), "public.plans")
	require.Contains(t, viewIDs(catalog.Views), "generated.type.Plan.table")

	var loaded appviews.CatalogEntry
	getJSON(t, httpSrv.URL+"/api/v1/views/public.plans", &loaded)
	require.Equal(t, "public.plans", loaded.ID)
	require.Equal(t, "standalone", string(loaded.Mount.Kind))
	require.Equal(t, []string{"table", "kanban", "card"}, loaded.AvailableVariants)

	var executed appviews.ExecuteResponse
	postJSON(t, httpSrv.URL+"/api/v1/views/public.plans/execute", appviews.ExecuteRequest{
		Page: appviews.PageRequest{First: 5},
	}, &executed)
	require.Equal(t, "public.plans", executed.View.ID)
	require.Equal(t, "table", executed.Variant)
	require.NotEmpty(t, executed.Rows)
	require.NotEmpty(t, executed.Rows[0].Ref.NotePath)
	require.Equal(t, "Plan", executed.Rows[0].ResolvedType)
	require.NotEmpty(t, executed.DefinitionFingerprint)
	require.NotEmpty(t, executed.ExecutionFingerprint)

	var cards appviews.ExecuteResponse
	postJSON(t, httpSrv.URL+"/api/v1/views/public.plans/execute", map[string]any{"variant": "card"}, &cards)
	require.Equal(t, "card", cards.Variant)
	require.NotNil(t, cards.Card)
	require.Nil(t, cards.Board)
	require.Equal(t, "title", cards.Card.Title.Field)
	require.NotNil(t, cards.Card.Preview)
	require.Equal(t, "summary", cards.Card.Preview.Field)

	var board appviews.ExecuteResponse
	postJSON(t, httpSrv.URL+"/api/v1/views/public.plans/execute", map[string]any{"variant": "kanban"}, &board)
	require.Equal(t, "kanban", board.Variant)
	require.Equal(t, 500, board.PageInfo.First)
	require.NotNil(t, board.Card)
	require.NotNil(t, board.Board)
	require.Equal(t, "summary", board.Board.ColumnField)
	require.Len(t, board.Board.Columns, 1)
	require.Equal(t, 1, board.Board.Columns[0].Count)
	require.Equal(t, 0, board.Board.Columns[0].RowStart)
	require.Equal(t, 1, board.Board.Columns[0].RowEnd)

	var created OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{}, &created)
	var staged OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/stage", OntologyEditSessionStageRequest{
		Ops: []OntologyEditOp{{
			Kind:  "setField",
			Path:  "specs/100-demo/plan.md",
			Field: "summary",
			Value: "Overlay plan summary",
		}},
	}, &staged)
	require.Equal(t, OntologyEditSessionStatusDirty, staged.Status)

	var overlaidGraphQL map[string]any
	postJSON(t, httpSrv.URL+"/api/v1/graphql", map[string]any{
		"editSession": map[string]any{"sessionId": created.SessionID},
		"query":       `query { plan(path: "specs/100-demo/plan.md") { path summary } }`,
	}, &overlaidGraphQL)
	overlaidData, ok := overlaidGraphQL["data"].(map[string]any)
	require.True(t, ok)
	overlaidPlans, ok := overlaidData["plan"].([]any)
	require.True(t, ok)
	require.Len(t, overlaidPlans, 1)
	overlaidPlan, ok := overlaidPlans[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "Overlay plan summary", overlaidPlan["summary"])

	var overlaidView appviews.ExecuteResponse
	postJSON(t, httpSrv.URL+"/api/v1/views/public.plans/execute", appviews.ExecuteRequest{
		Page:        appviews.PageRequest{First: 5},
		EditSession: &appviews.EditSessionRequest{SessionID: created.SessionID},
	}, &overlaidView)
	require.NotEmpty(t, overlaidView.Rows)
	require.Equal(t, "Overlay plan summary", overlaidView.Rows[0].Fields["summary"])

	var replaced OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/stage", OntologyEditSessionStageRequest{
		Replace: true,
		Ops: []OntologyEditOp{
			{Kind: "setField", Path: "specs/100-demo/plan.md", Field: "summary", Value: "Replacement plan summary"},
			{Kind: "setField", Path: "specs/100-demo/tasks.md", Field: "summary", Value: "Replacement tasks summary"},
		},
	}, &replaced)
	require.Equal(t, OntologyEditSessionStatusDirty, replaced.Status)

	var replacedGraphQL map[string]any
	postJSON(t, httpSrv.URL+"/api/v1/graphql", map[string]any{
		"editSession": map[string]any{"sessionId": created.SessionID},
		"query":       `query { plan(path: "specs/100-demo/plan.md") { path summary } }`,
	}, &replacedGraphQL)
	replacedData, ok := replacedGraphQL["data"].(map[string]any)
	require.True(t, ok)
	replacedPlans, ok := replacedData["plan"].([]any)
	require.True(t, ok)
	require.Len(t, replacedPlans, 1)
	replacedPlan, ok := replacedPlans[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "Replacement plan summary", replacedPlan["summary"])

	emptyBodyResp, err := http.Post(httpSrv.URL+"/api/v1/views/public.plans/execute", "application/json", bytes.NewReader(nil))
	require.NoError(t, err)
	defer emptyBodyResp.Body.Close()
	require.Equal(t, http.StatusOK, emptyBodyResp.StatusCode)
	var defaultExecuted appviews.ExecuteResponse
	require.NoError(t, json.NewDecoder(emptyBodyResp.Body).Decode(&defaultExecuted))
	require.Equal(t, "public.plans", defaultExecuted.View.ID)
	require.NotEmpty(t, defaultExecuted.Rows)
	require.NotEqual(t, "Replacement plan summary", defaultExecuted.Rows[0].Fields["summary"])

	// An explicit empty editSession object reads committed state, not a session.
	var emptySessionExecuted appviews.ExecuteResponse
	postJSON(t, httpSrv.URL+"/api/v1/views/public.plans/execute", map[string]any{"editSession": map[string]any{}}, &emptySessionExecuted)
	require.Equal(t, defaultExecuted.Rows, emptySessionExecuted.Rows)

	missingResp, err := http.Get(httpSrv.URL + "/api/v1/views/not-there")
	require.NoError(t, err)
	defer missingResp.Body.Close()
	require.Equal(t, http.StatusNotFound, missingResp.StatusCode)
	var missingErr ErrorResponse
	require.NoError(t, json.NewDecoder(missingResp.Body).Decode(&missingErr))
	require.Equal(t, PublicErrorNotFound, missingErr.Code)

	methodResp, err := http.Post(httpSrv.URL+"/api/v1/views", "application/json", bytes.NewReader(nil))
	require.NoError(t, err)
	defer methodResp.Body.Close()
	require.Equal(t, http.StatusMethodNotAllowed, methodResp.StatusCode)
	var methodErr ErrorResponse
	require.NoError(t, json.NewDecoder(methodResp.Body).Decode(&methodErr))
	require.Equal(t, PublicErrorMethodNotAllowed, methodErr.Code)

	badBodyResp, err := http.Post(httpSrv.URL+"/api/v1/views/public.plans/execute", "application/json", strings.NewReader("{"))
	require.NoError(t, err)
	defer badBodyResp.Body.Close()
	require.Equal(t, http.StatusBadRequest, badBodyResp.StatusCode)
	var badBodyErr ErrorResponse
	require.NoError(t, json.NewDecoder(badBodyResp.Body).Decode(&badBodyErr))
	require.Equal(t, PublicErrorBadRequest, badBodyErr.Code)

	oversizedResp, err := http.Post(
		httpSrv.URL+"/api/v1/views/public.plans/execute",
		"application/json",
		strings.NewReader(strings.Repeat(" ", maxPublicViewExecuteBodyBytes+1)),
	)
	require.NoError(t, err)
	defer oversizedResp.Body.Close()
	require.Equal(t, http.StatusRequestEntityTooLarge, oversizedResp.StatusCode)
}

func TestServerProvidedRuntimeUsesExistingIntelDBBeforeLiveIndexArrives(t *testing.T) {
	t.Parallel()

	fixture := prepareWebFixtureVault(t)
	runtime := &Runtime{}
	srv := newFixtureServer(t, fixture, runtime)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var before GraphResponse
	getJSON(t, httpSrv.URL+"/api/v1/graphs/global", &before)
	require.False(t, before.NeedsIndex)
	require.NotEmpty(t, before.Nodes)
	require.NotEmpty(t, before.Edges)

	var beforeStatus StatusResponse
	getJSON(t, httpSrv.URL+"/api/v1/status", &beforeStatus)
	require.True(t, beforeStatus.CodeIndex)

	runtime.SetIntelStore(fixture.intelStore)

	var after GraphResponse
	getJSON(t, httpSrv.URL+"/api/v1/graphs/global", &after)
	require.False(t, after.NeedsIndex)
	require.NotEmpty(t, after.Nodes)
	require.NotEmpty(t, after.Edges)

	var afterStatus StatusResponse
	getJSON(t, httpSrv.URL+"/api/v1/status", &afterStatus)
	require.True(t, afterStatus.CodeIndex)
}

func TestOntologyEndpointsWithoutSchema(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "a.md"), []byte("# Untyped note\n"), 0o644))
	fixture := fixtureVault{
		root:     root,
		vault:    &obsidian.Vault{Name: "no-schema"},
		vaultDef: obsidian.VaultDefinition{Name: "no-schema", Path: root, Links: obsidian.LinkTypeBoth},
	}
	srv := newFixtureServer(t, fixture, &Runtime{})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var summary OntologySummaryResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/summary", &summary)
	require.False(t, summary.SchemaPresent)

	var querySchema OntologyQuerySchemaResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/query-schema", &querySchema)
	require.False(t, querySchema.SchemaPresent)
	require.False(t, querySchema.QuerySchemaPresent)

	var atlas OntologyAtlasResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/atlas", &atlas)
	require.False(t, atlas.SchemaPresent)
	require.Empty(t, atlas.Types)

	for _, endpoint := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/edit-sessions"},
		{http.MethodGet, "/api/v1/edit-sessions/missing"},
		{http.MethodPost, "/api/v1/edit-sessions/missing/stage"},
		{http.MethodPost, "/api/v1/edit-sessions/missing/preview"},
		{http.MethodPost, "/api/v1/edit-sessions/missing/commit"},
	} {
		t.Run(endpoint.method+" "+endpoint.path, func(t *testing.T) {
			req, err := http.NewRequest(endpoint.method, httpSrv.URL+endpoint.path, strings.NewReader(`{}`))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusBadRequest, resp.StatusCode)
			var response ErrorResponse
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&response))
			require.Equal(t, ErrorResponse{Error: "ontology is unavailable", Code: PublicErrorBadRequest}, response)
		})
	}
}

func TestOntologyWorkspaceFallsBackToUntypedIndexedNotesWithoutSchema(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "a.md"), []byte("# Untyped note\n\nBody.\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "b.md"), []byte("# Second note\n\n[[a]]\n"), 0o644))

	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = store.Close()
	})

	vaultDef := obsidian.VaultDefinition{
		Name:  "no-schema",
		Path:  root,
		Links: obsidian.LinkTypeBoth,
	}
	require.NoError(t, testNoteMetadataIndexer(t).SyncPaths(context.Background(), vaultDef, &obsidian.Note{}, store, []string{
		"notes/a.md",
		"notes/b.md",
	}, nil))

	fixture := fixtureVault{
		root:       root,
		vault:      &obsidian.Vault{Name: "no-schema"},
		vaultDef:   vaultDef,
		intelStore: store,
	}
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var summary OntologySummaryResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/summary", &summary)
	require.False(t, summary.SchemaPresent)
	require.Equal(t, 2, summary.TotalNotes)
	require.Equal(t, 0, summary.TypedNotes)
	require.Equal(t, 2, summary.UntypedNotes)
	require.Empty(t, summary.Types)

	var allNotes OntologyTypeResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/types/__all__", &allNotes)
	require.Equal(t, 2, allNotes.Count)
	require.Len(t, allNotes.Notes, 2)
	require.Empty(t, allNotes.Notes[0].ResolvedType)

	workspace := requireNodeWorkspace(t, srv, "notes/a.md", defaultNodeWorkspaceIncludes())
	require.Equal(t, "notes/a.md", workspace.RequestedRef)
	require.Equal(t, ontology.NodeKindNote, workspace.Node.Ref.Kind)
	require.Equal(t, "Untyped note", workspace.Content.Title)
	require.NotEmpty(t, workspace.Content.Markdown)
	require.NotNil(t, workspace.Content.Rendered)
	require.Nil(t, workspace.Content.Structural)
	require.NotEmpty(t, workspace.Version)
	require.NotEmpty(t, workspace.FocusedNodeID)
	require.NotEmpty(t, workspace.Nodes)
}

func TestOntologyEndpointsWithSchema(t *testing.T) {
	t.Parallel()

	fixture := prepareOntologyFixtureVault(t)
	require.NoError(t, os.MkdirAll(filepath.Join(fixture.root, ".rhizome", "query-recipes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, ".rhizome", "query-recipes", "public.yaml"), []byte(`apiVersion: rhizome.query-recipe.v1
id: public-plan-by-path
name: Public plan by path
problem: Load one plan through the public API.
inputSpec:
  mode: required_anchor
  primaryInput: path
  inputs:
    - name: path
      required: true
      kind: path
query:
  graphQL: |
    query PlanByPath($path: String!) {
      plan(path: $path) {
        path
        title
        summary
      }
    }
outputContract:
  expectedPaths: [plan.path]
  empty: No plan found.
adaptationGuidance:
  summary: Use the plan row as the custom workflow source record.
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, "specs", "100-demo", "task-flow.md"), []byte(`---
title: Task Flow
---

# Task Flow

Task creation validates required fields.
`), 0o644))
	_, err := ontology.EnsureFreshRuntimeWithStore(context.Background(), testNoteMetadataIndexer(t), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
	require.NoError(t, err)
	require.NoError(t, fixture.intelStore.ReplaceIntelCodeFile(context.Background(), "specs/100-demo/task-flow.md", nil, nil, nil))
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var summary OntologySummaryResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/summary", &summary)
	require.True(t, summary.SchemaPresent)
	require.True(t, summary.QuerySchemaPresent)
	require.Equal(t, 5, summary.TypedNotes)
	require.NotEmpty(t, summary.Types)

	var inspect OntologyInspectResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/inspect?path=specs/100-demo/plan.md", &inspect)
	require.True(t, inspect.OntologyAvailable)
	require.NotNil(t, inspect.Note)
	require.Equal(t, "Plan", inspect.Note.ResolvedType)

	var rendered RenderedFileResponse
	getJSON(t, httpSrv.URL+"/api/v1/files/rendered?path=specs/100-demo/spec.md", &rendered)
	require.Equal(t, "Spec", rendered.ResolvedType)
	require.NotEmpty(t, rendered.Sections)
	require.NotEmpty(t, rendered.Embeds)

	workspace := requireNodeWorkspace(t, srv, "specs/100-demo/plan.md", defaultNodeWorkspaceIncludes())
	require.Equal(t, "Plan", workspace.Content.ResolvedType)
	require.NotEmpty(t, workspace.Relations)
	require.NotEmpty(t, workspace.Content.Structural)
	require.NotEmpty(t, workspace.SectionRelationGroups)
	foundSectionTitle := false
	for _, groups := range workspace.SectionRelationGroups {
		if len(groups) == 0 || len(groups[0].Items) == 0 {
			continue
		}
		if groups[0].Items[0].Title == "Demo Spec" {
			foundSectionTitle = true
			break
		}
	}
	require.True(t, foundSectionTitle)

	require.Equal(t, "specs/100-demo/plan.md", workspace.RequestedRef)
	require.Equal(t, "specs/100-demo/plan.md", workspace.Node.NotePath)
	require.Equal(t, ontology.NodeKindNote, workspace.Node.Ref.Kind)
	require.Equal(t, "NOTE", workspace.Node.Locator)
	require.Equal(t, "Plan", workspace.Node.ResolvedType)
	require.NotEmpty(t, workspace.Content.Title)
	require.NotNil(t, workspace.Content.Rendered)
	require.NotEmpty(t, workspace.Fields)
	require.NotEmpty(t, workspace.Relations)
	require.True(t, workspace.Loaded.Rendered)
	require.True(t, workspace.Loaded.Assessment)
	require.True(t, workspace.Loaded.Structure)
	require.True(t, workspace.Loaded.Relations)
	require.True(t, workspace.Capabilities.CanEdit)
	require.True(t, workspace.Capabilities.CanNavigateChildren)
	require.NotEmpty(t, workspace.Version)
	require.NotEmpty(t, workspace.FocusedNodeID)
	require.NotEmpty(t, workspace.Nodes)
	require.NotEmpty(t, workspace.Edges)
	require.NotNil(t, workspace.Views)
	require.NotNil(t, workspace.Views.RenderedOutline)
	require.NotNil(t, workspace.Views.StructuralOutline)
	require.NotEmpty(t, workspace.Views.RelationGroups)
	require.Equal(t, workspace.FocusedNodeID, workspace.Views.RelationGroups[0].ScopeNodeID)

	nodeByID := map[string]WorkspaceNodeResponse{}
	for _, node := range workspace.Nodes {
		nodeByID[node.ID] = node
	}
	require.Contains(t, nodeByID, workspace.FocusedNodeID)
	require.Equal(t, WorkspaceNodeKindNote, nodeByID[workspace.FocusedNodeID].Kind)
	require.NotEmpty(t, nodeByID[workspace.FocusedNodeID].ChildIDs)

	containsFocused := false
	relatesFocused := false
	for _, edge := range workspace.Edges {
		if edge.FromID == workspace.FocusedNodeID && edge.Kind == WorkspaceEdgeKindContains {
			containsFocused = true
		}
		if edge.FromID == workspace.FocusedNodeID && edge.Kind == WorkspaceEdgeKindRelatesTo {
			relatesFocused = true
		}
	}
	require.True(t, containsFocused)
	require.True(t, relatesFocused)

	coreWorkspace := requireNodeWorkspace(t, srv, "specs/100-demo/plan.md", nodeWorkspaceIncludes{Rendered: true})
	require.True(t, coreWorkspace.Loaded.Rendered)
	require.False(t, coreWorkspace.Loaded.Assessment)
	require.False(t, coreWorkspace.Loaded.Structure)
	require.False(t, coreWorkspace.Loaded.Relations)
	require.NotNil(t, coreWorkspace.Content.Rendered)
	require.Nil(t, coreWorkspace.Content.Assessment)
	require.Nil(t, coreWorkspace.Content.TypeDoc)
	require.Nil(t, coreWorkspace.Content.Structural)
	require.Empty(t, coreWorkspace.Relations)
	require.Empty(t, coreWorkspace.SectionRelationGroups)

	structuralSectionWorkspace := requireNodeWorkspace(t, srv, "specs/100-demo/plan.md#Validation", defaultNodeWorkspaceIncludes())
	require.Equal(t, "specs/100-demo/plan.md#Validation", structuralSectionWorkspace.RequestedRef)
	require.Equal(t, ontology.NodeKindSection, structuralSectionWorkspace.Node.Ref.Kind)
	require.Equal(t, "Validation", structuralSectionWorkspace.Node.Title)
	require.Equal(t, "Section", structuralSectionWorkspace.Node.ResolvedType)
	require.Equal(t, "Validation", structuralSectionWorkspace.Content.Title)
	require.NotEmpty(t, structuralSectionWorkspace.Content.Markdown)
	require.NotNil(t, structuralSectionWorkspace.Content.Structural)

	require.Len(t, rendered.Sections, 1)
	require.Len(t, rendered.Sections[0].Children, 2)
	fragment := strings.TrimPrefix(rendered.Sections[0].Children[0].ID, rendered.Path+"#")
	exactSectionWorkspace := requireNodeWorkspace(t, srv, rendered.Path+"#"+fragment, defaultNodeWorkspaceIncludes())
	require.Equal(t, rendered.Sections[0].Children[0].ID, exactSectionWorkspace.Node.Ref.String())
	require.Equal(t, rendered.Sections[0].Children[0].Title, exactSectionWorkspace.Content.Title)
	require.NotNil(t, exactSectionWorkspace.Content.Rendered)
	require.Equal(t, rendered.Path+"#"+fragment, exactSectionWorkspace.Content.Rendered.Path)
	require.Len(t, exactSectionWorkspace.Content.Rendered.Sections, 1)
	require.Equal(t, rendered.Sections[0].Children[0].ID, exactSectionWorkspace.Content.Rendered.Sections[0].ID)
	require.NotNil(t, exactSectionWorkspace.Views)

	embeddedWorkspace := requireNodeWorkspace(t, srv, "specs/100-demo/spec.md#^validation", defaultNodeWorkspaceIncludes())
	require.Equal(t, "specs/100-demo/spec.md#^validation", embeddedWorkspace.Content.Path)
	require.Equal(t, "Validation story", embeddedWorkspace.Content.Title)
	require.Equal(t, "Story", embeddedWorkspace.Content.ResolvedType)
	require.NotNil(t, embeddedWorkspace.Content.TypeDoc)
	require.Equal(t, "Story", embeddedWorkspace.Content.TypeDoc.Name)
	require.Nil(t, embeddedWorkspace.Content.Assessment)
	require.NotNil(t, embeddedWorkspace.Content.Rendered)
	require.Len(t, embeddedWorkspace.Content.Rendered.Sections, 1)
	require.Equal(t, "Validation story", embeddedWorkspace.Content.Rendered.Sections[0].Title)
	require.Equal(t, "Story", embeddedWorkspace.Content.Rendered.Sections[0].TypeName)
	require.NotNil(t, embeddedWorkspace.Content.Structural)
	require.Equal(t, "Validation story", embeddedWorkspace.Content.Structural.Root.Title)
	require.Equal(t, "EMBEDDED", embeddedWorkspace.Content.Structural.Root.Locator)
	require.Equal(t, "specs/100-demo/spec.md", embeddedWorkspace.Content.Structural.Root.NotePath)
	require.NotNil(t, embeddedWorkspace.LinkTarget)
	require.Equal(t, "[[spec#^validation]]", embeddedWorkspace.LinkTarget.Wikilink)
	require.True(t, embeddedWorkspace.LinkTarget.Exists)
	require.NotNil(t, embeddedWorkspace.NodeLocator)
	require.Equal(t, ontology.NodeLocatorLinkable, embeddedWorkspace.NodeLocator.Status)
	require.Equal(t, "[[spec#^validation]]", embeddedWorkspace.NodeLocator.LinkTarget.Wikilink)
	require.NotNil(t, embeddedWorkspace.Node.NodeLocator)
	require.Equal(t, ontology.NodeLocatorLinkable, embeddedWorkspace.Node.NodeLocator.Status)
	require.Empty(t, embeddedWorkspace.LinkFixOps)
	require.NotEmpty(t, embeddedWorkspace.Relations)
	foundPlanLink := false
	for _, group := range embeddedWorkspace.Relations {
		for _, item := range group.Items {
			if item.Path == "specs/100-demo/plan.md" {
				foundPlanLink = true
				break
			}
		}
	}
	require.True(t, foundPlanLink)

	require.Equal(t, "specs/100-demo/spec.md#^validation", embeddedWorkspace.RequestedRef)
	require.Equal(t, "specs/100-demo/spec.md", embeddedWorkspace.Node.NotePath)
	require.Equal(t, "^validation", embeddedWorkspace.Node.Ref.Fragment)
	require.Equal(t, "Validation story", embeddedWorkspace.Node.Title)
	require.Equal(t, "Story", embeddedWorkspace.Node.ResolvedType)
	require.Equal(t, "Validation story", embeddedWorkspace.Content.Title)
	require.NotEmpty(t, embeddedWorkspace.Content.Markdown)
	require.NotNil(t, embeddedWorkspace.Content.Structural)
	require.NotEmpty(t, embeddedWorkspace.Relations)
	require.NotEmpty(t, embeddedWorkspace.Version)
	require.NotNil(t, embeddedWorkspace.Views)
	focusedNodeHasLocator := false
	for _, node := range embeddedWorkspace.Nodes {
		if node.ID == embeddedWorkspace.FocusedNodeID && node.NodeLocator != nil {
			focusedNodeHasLocator = true
			require.Equal(t, ontology.NodeLocatorLinkable, node.NodeLocator.Status)
			break
		}
	}
	require.True(t, focusedNodeHasLocator)

	var editSession OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{
		Ops: []OntologyEditOp{{
			Kind:  "setField",
			Path:  "specs/100-demo/spec.md#^validation",
			Field: "status",
			Value: "DONE",
		}},
	}, &editSession)
	require.Equal(t, OntologyEditSessionStatusDirty, editSession.Status)

	var previewResp OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+editSession.SessionID+"/preview", map[string]any{}, &previewResp)
	require.NotEmpty(t, previewResp.Plan.Files)
	require.Contains(t, previewResp.Plan.Files[0].UpdatedContentPreview, "status:: DONE")
	require.Contains(t, previewResp.Plan.Files[0].Diff, "DONE")

	var commitResp OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+editSession.SessionID+"/commit", map[string]any{}, &commitResp)
	require.Equal(t, OntologyEditSessionStatusClean, commitResp.Status)

	data, err := os.ReadFile(filepath.Join(fixture.root, "specs/100-demo/spec.md"))
	require.NoError(t, err)
	require.Contains(t, string(data), "status:: DONE")
	require.Contains(t, string(data), "^validation")
	require.Contains(t, string(data), "- Follow [[plan]] while validating.")

	embeddedWorkspace = requireNodeWorkspace(t, srv, "specs/100-demo/spec.md#^validation", defaultNodeWorkspaceIncludes())
	require.Equal(t, "Story", embeddedWorkspace.Content.ResolvedType)
	require.NotNil(t, embeddedWorkspace.Content.Rendered)
	require.Len(t, embeddedWorkspace.Content.Rendered.Sections, 1)
	require.Equal(t, "DONE", embeddedWorkspace.Content.Rendered.Sections[0].Properties["status"])

	var search NoteSearchResponse
	getJSON(t, httpSrv.URL+"/api/v1/search/notes?q=plan+spec:specs/100-demo/spec.md&type=Plan", &search)
	require.Equal(t, 1, search.Total)
	require.Equal(t, "Plan", search.Matches[0].ResolvedType)
	require.Equal(t, "specs/100-demo/plan.md", search.Matches[0].Path)

	var searchGrouped NoteSearchResponse
	getJSON(t, httpSrv.URL+"/api/v1/search/notes?q=%28plan+OR+quickstart%29+AND+NOT+find:tasks", &searchGrouped)
	require.Equal(t, 2, searchGrouped.Total)
	require.Equal(t, "specs/100-demo/plan.md", searchGrouped.Matches[0].Path)
	require.Equal(t, "specs/100-demo/quickstart.md", searchGrouped.Matches[1].Path)

	var queryResp map[string]any
	postJSON(t, httpSrv.URL+"/api/v1/graphql", map[string]string{
		"query": `query { notes(type: "Plan", first: 10) { nodes { path title } } }`,
	}, &queryResp)
	queryData, ok := queryResp["data"].(map[string]any)
	require.True(t, ok)
	notesResult, ok := queryData["notes"].(map[string]any)
	require.True(t, ok)
	notes, ok := notesResult["nodes"].([]any)
	require.True(t, ok)
	require.Len(t, notes, 1)

	var publicQueryResp map[string]any
	postJSON(t, httpSrv.URL+"/api/v1/graphql", map[string]any{
		"operationName": "Plans",
		"variables": map[string]any{
			"path": "specs/100-demo/plan.md",
		},
		"query": `query Ignored { notes(type: "Research", first: 10) { nodes { path title } } }
query Plans($path: String!) {
  ... on Query {
    plans: plan(path: $path) {
      path
      title
      summary
    }
    planNode: node(ref: $path) {
      nodeId
      nodeKind
      path
      title
      ... on Note {
        resolvedType
      }
    }
    resolvedPlanRef: resolve(ref: $path) {
      found
      ref {
        kind
        path
      }
    }
    taskFlow: node(ref: "specs/100-demo/task-flow.md") {
      nodeKind
      title
      path
      resolvedType
      ... on Note {
        content
      }
      ... on CodeFile {
        language
      }
    }
  }
  ontology { nextId(type: "Spec") { available } }
}`,
	}, &publicQueryResp)
	publicData, ok := publicQueryResp["data"].(map[string]any)
	require.True(t, ok)
	publicNotes, ok := publicData["plans"].([]any)
	require.True(t, ok)
	require.Len(t, publicNotes, 1)
	publicPlan, ok := publicNotes[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "specs/100-demo/plan.md", publicPlan["path"])
	require.Equal(t, "Demo plan summary", publicPlan["summary"])
	planNode, ok := publicData["planNode"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "NOTE", planNode["nodeKind"])
	require.Equal(t, "specs/100-demo/plan.md", planNode["path"])
	require.Equal(t, "Plan", planNode["resolvedType"])
	resolvedPlanRef, ok := publicData["resolvedPlanRef"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, resolvedPlanRef["found"])
	taskFlow, ok := publicData["taskFlow"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "NOTE", taskFlow["nodeKind"])
	require.Equal(t, "Task Flow", taskFlow["title"])
	require.Equal(t, "specs/100-demo/task-flow.md", taskFlow["path"])
	require.Equal(t, "", taskFlow["resolvedType"])
	require.Contains(t, taskFlow["content"], "Task creation validates required fields.")
	require.NotContains(t, taskFlow, "language")
	require.NotNil(t, publicData["ontology"])

	resp, err := http.Get(httpSrv.URL + "/api/v1/graphql/schema")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "text/plain")
	sdl, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(sdl), "type Query")
	require.Contains(t, string(sdl), "interface Node")
	require.Contains(t, string(sdl), "type CodeFile implements Node")
	require.Contains(t, string(sdl), "type Plan")
	require.Contains(t, string(sdl), "ontology: OntologyRuntime!")
	require.NotContains(t, string(sdl), "agent: AgentRuntime!")
	require.NotContains(t, string(sdl), "type AgentRuntime")

	var publicWorkflowResp map[string]any
	postJSON(t, httpSrv.URL+"/api/v1/graphql", map[string]any{
		"operationName": "WorkflowReads",
		"variables": map[string]any{
			"refs": []string{"specs/100-demo/plan.md", "missing.md"},
		},
		"query": `query WorkflowReads($refs: [String!]!) {
  refs: nodes(refs: $refs, first: 10) {
    items {
      requestedRef
      node {
        nodeKind
        title
      }
      error {
        code
        message
      }
    }
    pageInfo {
      returnedCount
      truncated
    }
  }
  validation(firstIssues: 5) {
    ok
    issueCount
    selectedChecks
    checks {
      name
      ok
      issueCount
      issues {
        code
        path
        message
      }
    }
  }
  ontology {
    schemaHash
    types(first: 20) {
      name
      role
      fields {
        name
        type
        identifierFormat {
          prefix
          pad
        }
      }
    }
  }
}`,
	}, &publicWorkflowResp)
	workflowData, ok := publicWorkflowResp["data"].(map[string]any)
	require.True(t, ok)
	refBatch, ok := workflowData["refs"].(map[string]any)
	require.True(t, ok)
	refItems, ok := refBatch["items"].([]any)
	require.True(t, ok)
	require.Len(t, refItems, 2)
	firstRef := refItems[0].(map[string]any)
	require.Equal(t, "specs/100-demo/plan.md", firstRef["requestedRef"])
	require.Equal(t, "Implementation Plan", firstRef["node"].(map[string]any)["title"])
	secondRef := refItems[1].(map[string]any)
	require.Equal(t, "missing.md", secondRef["requestedRef"])
	require.NotNil(t, secondRef["error"])
	require.NotNil(t, workflowData["validation"])
	ontologyRuntime := workflowData["ontology"].(map[string]any)
	require.NotEmpty(t, ontologyRuntime["schemaHash"])
	require.NotEmpty(t, ontologyRuntime["types"])

	var introspectionResp map[string]any
	postJSON(t, httpSrv.URL+"/api/v1/graphql", map[string]any{
		"operationName": "IntrospectionQuery",
		"query": `query IntrospectionQuery {
  __schema {
    queryType { name }
    types {
      ...FullType
    }
  }
}

fragment FullType on __Type {
  kind
  name
  description
  fields {
    name
    description
    args {
      name
      type { ...TypeRef }
    }
    type { ...TypeRef }
  }
  enumValues {
    name
    description
  }
}

fragment TypeRef on __Type {
  kind
  name
  ofType {
    kind
    name
    ofType {
      kind
      name
    }
  }
	}`,
	}, &introspectionResp)
	require.NotContains(t, introspectionResp, "errors")
	introspectionData, ok := introspectionResp["data"].(map[string]any)
	require.True(t, ok)
	schemaData, ok := introspectionData["__schema"].(map[string]any)
	require.True(t, ok)
	queryType, ok := schemaData["queryType"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "Query", queryType["name"])
	introspectionTypes, ok := schemaData["types"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, introspectionTypes)
	var planType map[string]any
	var nodeType map[string]any
	for _, item := range introspectionTypes {
		typeData, ok := item.(map[string]any)
		require.True(t, ok)
		if typeData["name"] == "Plan" {
			planType = typeData
		}
		if typeData["name"] == "Node" {
			nodeType = typeData
		}
	}
	require.NotNil(t, planType)
	require.Equal(t, "OBJECT", planType["kind"])
	fields, ok := planType["fields"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, fields)
	var summaryField map[string]any
	for _, item := range fields {
		fieldData, ok := item.(map[string]any)
		require.True(t, ok)
		if fieldData["name"] == "summary" {
			summaryField = fieldData
			break
		}
	}
	require.NotNil(t, summaryField)
	require.NotNil(t, nodeType)
	nodeFields := nodeType["fields"].([]any)
	workspaceIntrospected := false
	for _, item := range nodeFields {
		field, ok := item.(map[string]any)
		if ok && field["name"] == "workspace" {
			workspaceIntrospected = true
			break
		}
	}
	require.True(t, workspaceIntrospected)

	var publicStatus StatusResponse
	getJSON(t, httpSrv.URL+"/api/v1/status", &publicStatus)
	require.Equal(t, fixture.vaultDef.Name, publicStatus.VaultName)
	require.Equal(t, fixture.root, publicStatus.VaultPath)

	var capabilities PublicCapabilitiesResponse
	getJSON(t, httpSrv.URL+"/api/v1/capabilities", &capabilities)
	require.Equal(t, publicAPIVersion, capabilities.APIVersion)
	require.NotEmpty(t, capabilities.RhizomeVersion)
	require.Equal(t, fixture.vaultDef.Name, capabilities.Vault.Name)
	require.Equal(t, fixture.root, capabilities.Vault.Path)
	require.True(t, capabilities.GraphQL.Available)
	require.Equal(t, "/api/v1/graphql", capabilities.GraphQL.Endpoint)
	require.Equal(t, "/api/v1/graphql/schema", capabilities.GraphQL.SchemaEndpoint)
	require.Equal(t, "/graphql", capabilities.GraphQL.ExplorerURL)
	require.True(t, capabilities.GraphQL.Introspection)
	require.NotEmpty(t, capabilities.GraphQL.SchemaHash)
	require.NotEmpty(t, capabilities.GraphQL.OntologyHash)
	require.Equal(t, 1, capabilities.QueryRecipes.Count)
	require.True(t, capabilities.Views.Available)
	require.GreaterOrEqual(t, capabilities.Views.Count, 1)
	require.Contains(t, capabilities.Views.Endpoints, "/api/v1/views/{id}/execute")
	require.Equal(t, "/api/v1/events", capabilities.Events.Endpoint)
	require.Equal(t, "/api/v1/nodes/events", capabilities.Events.NodeEndpoint)
	require.Contains(t, capabilities.Events.Heartbeat, "heartbeat")
	require.Contains(t, capabilities.Events.Reconnect, "refetch")
	require.Contains(t, capabilities.Events.SupportedKinds, GlobalEventNodeChanged)
	require.Contains(t, capabilities.Events.SupportedKinds, GlobalEventValidationInvalidated)
	require.Contains(t, capabilities.Events.SupportedKinds, GlobalEventSchemaInvalidated)
	require.Contains(t, capabilities.Events.SupportedKinds, GlobalEventQueryRecipeInvalidated)
	require.Contains(t, capabilities.Events.SupportedKinds, GlobalEventIndexInvalidated)
	require.Contains(t, capabilities.Events.SupportedKinds, GlobalEventCapabilitiesInvalidated)
	require.Contains(t, capabilities.Events.SupportedKinds, GlobalEventEditSessionInvalidated)
	require.Contains(t, capabilities.Events.NodeStreamedKinds, PublicNodeEventUpdated)
	require.Contains(t, capabilities.Events.NodeStreamedKinds, PublicNodeEventStale)
	require.Contains(t, capabilities.Events.NodeStreamedKinds, PublicNodeEventDeleted)
	require.Contains(t, capabilities.Events.SupportedKinds, PublicNodeEventUpdated)
	require.Contains(t, capabilities.REST.Resources, "edit-sessions")
	require.Contains(t, capabilities.REST.Resources, "nodes")
	require.Contains(t, capabilities.REST.Resources, "views")
	require.Contains(t, capabilities.EditSessions.Endpoints, "/api/v1/edit-sessions")
	require.Contains(t, capabilities.EditSessions.Endpoints, "/api/v1/edit-sessions/{id}/stage")
	require.NotContains(t, capabilities.EditSessions.Endpoints, "/api/ontology/edit-sessions")
	require.Contains(t, capabilities.Errors.Codes, PublicErrorGraphQLValidation)

	openAPIResp, err := http.Get(httpSrv.URL + capabilities.REST.OpenAPI)
	require.NoError(t, err)
	defer openAPIResp.Body.Close()
	require.Equal(t, http.StatusOK, openAPIResp.StatusCode)
	openAPIBody, err := io.ReadAll(openAPIResp.Body)
	require.NoError(t, err)
	require.Contains(t, string(openAPIBody), "Rhizome Public REST API")
	require.Contains(t, string(openAPIBody), "name: limit")
	require.Contains(t, string(openAPIBody), "/api/v1/views/{id}/execute")
	require.NotContains(t, string(openAPIBody), "NodeWorkspaceResponse")
	for _, retiredPath := range []string{
		"/api/ontology/node-workspace?ref=specs/100-demo/plan.md",
		"/api/ontology/node-workspace/graph?ref=specs/100-demo/plan.md",
	} {
		retiredResp, getErr := http.Get(httpSrv.URL + retiredPath)
		require.NoError(t, getErr)
		var retiredErr ErrorResponse
		require.NoError(t, json.NewDecoder(retiredResp.Body).Decode(&retiredErr))
		require.NoError(t, retiredResp.Body.Close())
		require.Equal(t, http.StatusNotFound, retiredResp.StatusCode)
		require.Equal(t, PublicErrorNotFound, retiredErr.Code)
	}

	eventsPostResp, err := http.Post(httpSrv.URL+"/api/v1/events", "application/json", bytes.NewReader(nil))
	require.NoError(t, err)
	defer eventsPostResp.Body.Close()
	require.Equal(t, http.StatusMethodNotAllowed, eventsPostResp.StatusCode)
	var eventErr ErrorResponse
	require.NoError(t, json.NewDecoder(eventsPostResp.Body).Decode(&eventErr))
	require.Equal(t, PublicErrorMethodNotAllowed, eventErr.Code)

	searchPostResp, err := http.Post(httpSrv.URL+"/api/v1/search", "application/json", bytes.NewReader(nil))
	require.NoError(t, err)
	defer searchPostResp.Body.Close()
	require.Equal(t, http.StatusMethodNotAllowed, searchPostResp.StatusCode)
	var searchMethodErr ErrorResponse
	require.NoError(t, json.NewDecoder(searchPostResp.Body).Decode(&searchMethodErr))
	require.Equal(t, PublicErrorMethodNotAllowed, searchMethodErr.Code)

	missingPathResp, err := http.Get(httpSrv.URL + "/api/v1/files/view")
	require.NoError(t, err)
	defer missingPathResp.Body.Close()
	require.Equal(t, http.StatusBadRequest, missingPathResp.StatusCode)
	var missingPathErr ErrorResponse
	require.NoError(t, json.NewDecoder(missingPathResp.Body).Decode(&missingPathErr))
	require.Equal(t, PublicErrorBadRequest, missingPathErr.Code)

	retiredResponse, err := http.Get(httpSrv.URL + "/api/v1/validate")
	require.NoError(t, err)
	defer retiredResponse.Body.Close()
	require.Equal(t, http.StatusGone, retiredResponse.StatusCode)
	var retiredError ErrorResponse
	require.NoError(t, json.NewDecoder(retiredResponse.Body).Decode(&retiredError))
	require.Equal(t, PublicErrorValidationAPIRetired, retiredError.Code)
	require.Contains(t, retiredError.Error, "/api/v2/validate")

	var validation ValidationEnvelope
	getJSON(t, httpSrv.URL+"/api/v2/validate", &validation)
	require.Equal(t, "never_ran", validation.Status)

	var recipeList PublicQueryRecipeListResponse
	getJSON(t, httpSrv.URL+"/api/v1/query-recipes", &recipeList)
	require.Len(t, recipeList.Recipes, 1)
	require.Equal(t, "public-plan-by-path", recipeList.Recipes[0].ID)

	var loadedRecipe queryrecipe.Recipe
	getJSON(t, httpSrv.URL+"/api/v1/query-recipes/public-plan-by-path", &loadedRecipe)
	require.Equal(t, "public-plan-by-path", loadedRecipe.ID)

	var publicRun queryrecipe.RunResult
	postJSON(t, httpSrv.URL+"/api/v1/query-recipes/public-plan-by-path/execute", PublicQueryRecipeExecuteRequest{
		Inputs: map[string]string{"path": "specs/100-demo/plan.md"},
	}, &publicRun)
	require.Equal(t, "public-plan-by-path", publicRun.Recipe.ID)
	runData, ok := publicRun.Result.Data["plan"].([]any)
	require.True(t, ok)
	require.Len(t, runData, 1)

	var invalidGraphQL ontologyquery.Result
	postJSON(t, httpSrv.URL+"/api/v1/graphql", map[string]any{
		"query": `query { noSuchPublicField }`,
	}, &invalidGraphQL)
	require.NotEmpty(t, invalidGraphQL.Errors)
	require.Equal(t, PublicErrorGraphQLValidation, invalidGraphQL.Errors[0].Extensions["code"])

	emptyGraphQLResp, err := http.Post(httpSrv.URL+"/api/v1/graphql", "application/json", bytes.NewReader([]byte(`{}`)))
	require.NoError(t, err)
	defer emptyGraphQLResp.Body.Close()
	require.Equal(t, http.StatusBadRequest, emptyGraphQLResp.StatusCode)
	var restErr ErrorResponse
	require.NoError(t, json.NewDecoder(emptyGraphQLResp.Body).Decode(&restErr))
	require.Equal(t, PublicErrorBadRequest, restErr.Code)

	service, defs, err := srv.ontologyContext()
	require.NoError(t, err)
	require.NotNil(t, service)
	require.NotNil(t, defs)
	require.NotNil(t, defs.exec)
	recipe := queryrecipe.Recipe{
		APIVersion: queryrecipe.APIVersion,
		ID:         "public-plan-by-path",
		Name:       "Public plan by path",
		Problem:    "Load one plan through the public GraphQL transport.",
		InputSpec: queryrecipe.InputSpec{
			Mode: queryrecipe.InputModeRequiredAnchor,
			Inputs: []queryrecipe.Input{{
				Name:     "path",
				Required: true,
				Kind:     "path",
			}},
		},
		Query: queryrecipe.QuerySpec{GraphQL: `query PlanByPath($path: String!) {
  plan(path: $path) {
    path
    title
    summary
  }
}`},
		OutputContract:     queryrecipe.OutputContract{ExpectedPaths: []string{"plan.path"}, Empty: "No plan found."},
		AdaptationGuidance: queryrecipe.AdaptationGuidance{Summary: "Use the plan list as the source rows."},
	}
	compiled, compileIssues := queryrecipe.Compile(recipe, defs.exec, map[string]string{"path": "specs/100-demo/plan.md"})
	require.Empty(t, compileIssues)
	directRecipeResult := ontologyquery.ExecutePrepared(context.Background(), srv.ontologyQueryDeps(service, nil), defs.schema, defs.exec, compiled.Prepared)

	var publicRecipeResp ontologyquery.Result
	postJSON(t, httpSrv.URL+"/api/v1/graphql", map[string]any{
		"query":     compiled.Query,
		"variables": compiled.Variables,
	}, &publicRecipeResp)
	require.Equal(t, directRecipeResult.Data, publicRecipeResp.Data)
	require.Equal(t, directRecipeResult.Errors, publicRecipeResp.Errors)

	var publicGraphResp ontologyquery.Result
	postJSON(t, httpSrv.URL+"/api/v1/graphql", map[string]any{
		"query": `query PublicNodeGraph($ref: String!) {
  node(ref: $ref) {
    nodeKind
    title
    workspace {
      fields { name present values }
      collections { name items { ref { ref kind } } }
      capabilities { canEdit canEditCollections }
      status { validation { issueCount } hasWarnings }
      version
      assessment { resolvedType }
      structure { ref { ref kind } parentRef { ref } title level }
      relationGroups { key label items { ref { ref } relationName provenance structural } }
      loaded { rendered assessment structure relations }
    }
    neighborhood(first: 10) {
      edges { target { kind notePath fragment typeName } relation structural }
      truncated
    }
    localGraph(nodeLimit: 20, edgeLimit: 40) {
      nodes { id nodeKind title typeName sourceLocator }
      edges { source target kind relation structural }
      truncated
    }
  }
  nodes(refs: [$ref, "missing.md"]) {
    pageInfo { returnedCount truncated }
    items {
      node { nodeKind title }
      error { code }
    }
  }
  notes(type: "Spec", first: 1) {
    pageInfo { returnedCount truncated }
    nodes { title }
    warnings { code }
  }
  validation(firstIssues: 1) {
    ok
    checks { name issueCount issues { code path } }
  }
}`,
		"variables": map[string]any{"ref": "specs/100-demo/spec.md"},
	}, &publicGraphResp)
	require.Empty(t, publicGraphResp.Errors)
	publicGraphNode := publicGraphResp.Data["node"].(map[string]any)
	require.Equal(t, "NOTE", publicGraphNode["nodeKind"])
	publicWorkspace := publicGraphNode["workspace"].(map[string]any)
	require.NotEmpty(t, publicWorkspace["fields"].([]any))
	require.NotEmpty(t, publicWorkspace["version"])
	require.NotEmpty(t, publicWorkspace["structure"].([]any))
	require.NotNil(t, publicWorkspace["assessment"])
	publicLoaded := publicWorkspace["loaded"].(map[string]any)
	require.Equal(t, true, publicLoaded["rendered"])
	require.Equal(t, true, publicLoaded["assessment"])
	require.Equal(t, true, publicLoaded["structure"])
	require.Equal(t, true, publicLoaded["relations"])
	publicGraph := publicGraphNode["localGraph"].(map[string]any)
	require.NotEmpty(t, publicGraph["nodes"].([]any))
	require.NotEmpty(t, publicGraph["edges"].([]any))
	publicBatch := publicGraphResp.Data["nodes"].(map[string]any)
	require.EqualValues(t, 2, publicBatch["pageInfo"].(map[string]any)["returnedCount"])
	publicBatchItems := publicBatch["items"].([]any)
	require.Equal(t, "NOTE", publicBatchItems[0].(map[string]any)["node"].(map[string]any)["nodeKind"])
	require.Equal(t, "unresolved_ref", publicBatchItems[1].(map[string]any)["error"].(map[string]any)["code"])
	publicPage := publicGraphResp.Data["notes"].(map[string]any)
	require.EqualValues(t, 1, publicPage["pageInfo"].(map[string]any)["returnedCount"])
	require.Empty(t, publicPage["warnings"].([]any))
	publicValidation := publicGraphResp.Data["validation"].(map[string]any)
	require.Contains(t, publicValidation, "ok")

	var overlaySession OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{
		Ops: []OntologyEditOp{{
			Kind:  "setField",
			Path:  "specs/100-demo/plan.md",
			Field: "summary",
			Value: "Overlay summary",
		}},
	}, &overlaySession)
	require.Equal(t, OntologyEditSessionStatusDirty, overlaySession.Status)

	var overlayGraphQL ontologyquery.Result
	postJSON(t, httpSrv.URL+"/api/v1/graphql", map[string]any{
		"query": `query OverlayWorkspace($ref: String!) {
  node(ref: $ref) {
    workspace { fields { name values } loaded { rendered assessment structure relations } }
  }
}`,
		"variables":   map[string]any{"ref": "specs/100-demo/plan.md"},
		"editSession": map[string]any{"sessionId": overlaySession.SessionID},
	}, &overlayGraphQL)
	require.Empty(t, overlayGraphQL.Errors)
	overlayNode := overlayGraphQL.Data["node"].(map[string]any)
	overlayFields := overlayNode["workspace"].(map[string]any)["fields"].([]any)
	var overlaySummaryValues []any
	for _, raw := range overlayFields {
		field := raw.(map[string]any)
		if field["name"] == "summary" {
			overlaySummaryValues = field["values"].([]any)
		}
	}
	require.Equal(t, []any{"Overlay summary"}, overlaySummaryValues)
	diskAfterOverlay, err := os.ReadFile(filepath.Join(fixture.root, "specs/100-demo/plan.md"))
	require.NoError(t, err)
	require.NotContains(t, string(diskAfterOverlay), "Overlay summary")

	var localGraph GraphResponse
	getJSON(t, httpSrv.URL+"/api/v1/graphs/local?path=specs/100-demo/plan.md&notesOnly=true", &localGraph)
	for _, node := range localGraph.Nodes {
		require.NotEqual(t, "section", node.Kind)
	}
	for _, edge := range localGraph.Edges {
		require.NotEqual(t, "contains", edge.Kind)
		require.NotEqual(t, "section-link", edge.Kind)
		require.NotEqual(t, "section-backlink", edge.Kind)
	}

	var embeddedLocalGraph GraphResponse
	getJSON(t, httpSrv.URL+"/api/v1/graphs/local?ref=specs/100-demo/spec.md%23%5Evalidation&kind=EMBEDDED&notesOnly=true&diagnostics=true", &embeddedLocalGraph)
	require.NotEmpty(t, embeddedLocalGraph.CenterID)
	require.NotNil(t, embeddedLocalGraph.Diagnostics)
	center := graphNodeByID(embeddedLocalGraph.Nodes, embeddedLocalGraph.CenterID)
	require.Equal(t, "embedded", center.Kind)
	require.Equal(t, "specs/100-demo/spec.md#^validation", center.SourceLocator)
	parentEmbedsCenter := false
	for _, edge := range embeddedLocalGraph.Edges {
		if edge.Source == "note:specs/100-demo/spec.md" && edge.Target == embeddedLocalGraph.CenterID && edge.Kind == "embeds" {
			parentEmbedsCenter = true
		}
	}
	require.True(t, parentEmbedsCenter, "anchored embedded node keeps its parent-note embeds edge")
}

func TestOntologySummaryIgnoresMalformedFrontmatterInEmbeddedProjectionScan(t *testing.T) {
	t.Parallel()

	fixture := prepareOntologyFixtureVault(t)
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, "specs", "100-demo", "spec.md"), []byte(`---
summary: Demo spec summary
description: Two modes:
  partnership: ask focused questions
---
# Demo Spec

## Requirements

- Must support ontology browsing.

## Stories

### Validation story
status:: READY
^validation
`), 0o644))

	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var summary OntologySummaryResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/summary", &summary)
	require.True(t, summary.SchemaPresent)
	require.True(t, summary.QuerySchemaPresent)
	require.NotEmpty(t, summary.Types)

	var storyType OntologyTypeResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/types/Story", &storyType)
	require.Equal(t, 1, storyType.Count)
	require.Len(t, storyType.Notes, 1)
}

func TestOntologyAtlasEndpointWithSchema(t *testing.T) {
	t.Parallel()

	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var atlas OntologyAtlasResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/atlas", &atlas)
	require.True(t, atlas.SchemaPresent)
	require.NotEmpty(t, atlas.Types)

	byName := map[string]OntologyAtlasTypeEntry{}
	total := 0
	for _, entry := range atlas.Types {
		require.NotNil(t, entry.Type)
		byName[entry.Type.Name] = entry
		total += entry.Count
	}
	require.Equal(t, 8, total)
	for _, name := range []string{"Spec", "Plan", "Research", "Tasks", "Quickstart", "Criterion"} {
		entry, ok := byName[name]
		require.True(t, ok, "expected type %q in atlas", name)
		require.GreaterOrEqual(t, entry.Count, 1)
	}
	for i := 1; i < len(atlas.Types); i++ {
		prev, curr := atlas.Types[i-1], atlas.Types[i]
		if prev.Count == curr.Count {
			require.LessOrEqual(t, prev.Type.Name, curr.Type.Name)
		} else {
			require.GreaterOrEqual(t, prev.Count, curr.Count)
		}
	}

	sections := map[string]ontology.TypeDoc{}
	for _, section := range atlas.Sections {
		sections[section.Name] = section
	}
	storiesSection, ok := sections["StoriesSection"]
	require.True(t, ok, "expected StoriesSection in atlas sections")
	require.Equal(t, ontology.TypeRoleSection, storiesSection.Role)
	require.NotEmpty(t, storiesSection.Fields)
}

func TestOntologyAtlasEndpointIncludesEmbeddedTypes(t *testing.T) {
	t.Parallel()

	fixture := prepareInterfaceFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var atlas OntologyAtlasResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/atlas", &atlas)
	require.True(t, atlas.SchemaPresent)

	byName := map[string]OntologyAtlasTypeEntry{}
	for _, entry := range atlas.Types {
		require.NotNil(t, entry.Type)
		byName[entry.Type.Name] = entry
	}

	entry, ok := byName["SpecMetric"]
	require.True(t, ok, "expected embedded type SpecMetric in atlas")
	require.Equal(t, ontology.TypeRoleEmbeddedNode, entry.Type.Role)
	require.Equal(t, "EMBEDDED", entry.Type.Locator)
	require.Equal(t, 1, entry.Count)
	require.Equal(t, []string{"specs/prod.md#spec-metric-60"}, entry.StartingNotes)
	require.Len(t, atlas.Sections, 1)
}

func TestOntologySummaryReportsInterfacesWithImplementorRollup(t *testing.T) {
	t.Parallel()

	fixture := prepareInterfaceFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var summary OntologySummaryResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/summary", &summary)
	require.True(t, summary.SchemaPresent)
	require.Len(t, summary.Interfaces, 1)
	iface := summary.Interfaces[0]
	require.Equal(t, "SpecLike", iface.Name)
	require.ElementsMatch(t, []string{"ProcessSpec", "ProductSpec", "SpecMetric"}, iface.Implementors)
	require.Equal(t, 3, iface.Count)

	roleByName := map[string]string{}
	for _, ts := range summary.Types {
		roleByName[ts.Name] = ts.Role
	}
	require.Equal(t, "note", roleByName["ProcessSpec"])
	require.Equal(t, "note", roleByName["ProductSpec"])
	require.Equal(t, "embedded", roleByName["SpecMetric"])
	countByName := map[string]int{}
	for _, ts := range summary.Types {
		countByName[ts.Name] = ts.Count
	}
	require.Equal(t, 1, countByName["SpecMetric"])

	var typeResp OntologyTypeResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/types/SpecLike", &typeResp)
	require.Equal(t, 3, typeResp.Count)
	require.Len(t, typeResp.Notes, 3)
	resolved := map[string]string{}
	for _, note := range typeResp.Notes {
		resolved[note.Path] = note.ResolvedType
	}
	require.Equal(t, "ProcessSpec", resolved["specs/proc.md"])
	require.Equal(t, "ProductSpec", resolved["specs/prod.md"])
	require.Equal(t, "SpecMetric", resolved["specs/prod.md#spec-metric-60"])
}

func TestSearchNotesScopesInterfaceFiltersToNoteImplementors(t *testing.T) {
	t.Parallel()

	fixture := prepareInterfaceFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	handler := srv.Handler()
	search := func(target string) NoteSearchResponse {
		t.Helper()
		req := newApplicationRequest(http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		var response NoteSearchResponse
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&response))
		return response
	}

	interfaceSearch := search("/api/v1/search/notes?q=Process+spec&type=SpecLike")
	require.Equal(t, 1, interfaceSearch.Total)
	require.Len(t, interfaceSearch.Matches, 1)
	require.Equal(t, "specs/proc.md", interfaceSearch.Matches[0].Path)
	require.Equal(t, "ProcessSpec", interfaceSearch.Matches[0].ResolvedType)

	interfaceUnion := search("/api/v1/search/notes?q=spec&type=SpecLike")
	require.Equal(t, 2, interfaceUnion.Total)
	require.ElementsMatch(t, []string{"specs/proc.md", "specs/prod.md"}, []string{
		interfaceUnion.Matches[0].Path,
		interfaceUnion.Matches[1].Path,
	})

	concreteSearch := search("/api/v1/search/notes?q=Process+spec&type=ProcessSpec")
	require.Equal(t, 1, concreteSearch.Total)
	require.Equal(t, "specs/proc.md", concreteSearch.Matches[0].Path)

	unrelatedSearch := search("/api/v1/search/notes?q=unrelated&type=SpecLike")
	require.Zero(t, unrelatedSearch.Total)
}

func TestOntologyTypeEndpointReturnsEmbeddedNodeExamples(t *testing.T) {
	t.Parallel()

	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var typeResp OntologyTypeResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/types/Story", &typeResp)
	require.NotNil(t, typeResp.Type)
	require.Equal(t, "Story", typeResp.Type.Name)
	require.Equal(t, 1, typeResp.Count)
	require.Len(t, typeResp.Notes, 1)
	require.Equal(t, "specs/100-demo/spec.md#^validation", typeResp.Notes[0].Path)
	require.Equal(t, "Validation story", typeResp.Notes[0].Title)
	require.Equal(t, "Story", typeResp.Notes[0].ResolvedType)
}

func TestOntologyTypeDocsReportProfile(t *testing.T) {
	t.Parallel()

	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var typeResp struct {
		Type struct {
			Profile map[string]any `json:"profile"`
		} `json:"type"`
	}
	getJSON(t, httpSrv.URL+"/api/v1/ontology/types/Spec", &typeResp)
	require.Equal(t, map[string]any{
		"shape":          "reference",
		"summaryField":   "summary",
		"relationFields": []any{"plans", "research", "quickstarts"},
	}, typeResp.Type.Profile)

	var atlas OntologyAtlasResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/atlas", &atlas)
	for _, entry := range atlas.Types {
		require.NotNil(t, entry.Type.Profile, entry.Type.Name)
	}
}

func TestOntologyTypeEndpointPopulatesIdentityStatus(t *testing.T) {
	t.Parallel()

	fixture := prepareIdentityStatusFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var specResp OntologyTypeResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/types/Spec", &specResp)
	require.NotNil(t, specResp.Type)
	require.Equal(t, 3, specResp.Count)
	require.Len(t, specResp.Notes, 3)

	byPath := map[string]OntologyNoteListItem{}
	for _, note := range specResp.Notes {
		byPath[note.Path] = note
	}
	require.Equal(t, "proposed", byPath["specs/proposed.md"].IdentityStatus, "Spec with spec-status:proposed should surface identityStatus")
	require.Equal(t, "active", byPath["specs/active.md"].IdentityStatus, "Spec with spec-status:active should surface identityStatus")
	require.Empty(t, byPath["specs/empty.md"].IdentityStatus, "Spec without spec-status frontmatter should omit identityStatus")

	var personResp OntologyTypeResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/types/Person", &personResp)
	require.NotNil(t, personResp.Type)
	require.Equal(t, 1, personResp.Count)
	require.Len(t, personResp.Notes, 1)
	require.Empty(t, personResp.Notes[0].IdentityStatus, "Person has no identity-status field; identityStatus must stay empty")
}

func TestOntologyTypeEndpointPopulatesEmbeddedIdentityStatus(t *testing.T) {
	t.Parallel()

	fixture := prepareEmbeddedIdentityStatusFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var storyResp OntologyTypeResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/types/Story", &storyResp)
	require.NotNil(t, storyResp.Type)
	require.Equal(t, 2, storyResp.Count)
	require.Len(t, storyResp.Notes, 2)

	byPath := map[string]OntologyNoteListItem{}
	for _, note := range storyResp.Notes {
		byPath[note.Path] = note
	}
	require.Contains(t, byPath, "specs/product.md#^story-a")
	require.Contains(t, byPath, "specs/product.md#^story-b")
	require.Equal(t, "ready", byPath["specs/product.md#^story-a"].IdentityStatus)
	require.Equal(t, "satisfied", byPath["specs/product.md#^story-b"].IdentityStatus)
}

func TestOntologyTypeEndpointPostReflectsEditSession(t *testing.T) {
	t.Parallel()

	fixture := prepareIdentityStatusFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var created OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{
		Ops: []OntologyEditOp{{Kind: "setField", Path: "specs/proposed.md", Field: "specStatus", Value: "active"}},
	}, &created)
	require.True(t, created.HasUncommittedChanges)

	statusByPath := func(resp OntologyTypeResponse) map[string]string {
		out := map[string]string{}
		for _, note := range resp.Notes {
			out[note.Path] = note.IdentityStatus
		}
		return out
	}

	var staged OntologyTypeResponse
	postJSON(t, httpSrv.URL+"/api/v1/ontology/types/Spec", stagedReadRequest{
		EditSession: &EditSessionReadRequest{SessionID: created.SessionID},
	}, &staged)
	require.Equal(t, "active", statusByPath(staged)["specs/proposed.md"])

	var committed OntologyTypeResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/types/Spec", &committed)
	require.Equal(t, "proposed", statusByPath(committed)["specs/proposed.md"])
}

func TestNodePreviewPostReflectsEditSession(t *testing.T) {
	t.Parallel()

	fixture := prepareIdentityStatusFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var created OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{
		Ops: []OntologyEditOp{{Kind: "setField", Path: "specs/proposed.md", Field: "summary", Value: "Staged summary"}},
	}, &created)

	var staged NodePreview
	postJSON(t, httpSrv.URL+"/api/v1/nodes/preview?ref=specs%2Fproposed.md", stagedReadRequest{
		EditSession: &EditSessionReadRequest{SessionID: created.SessionID},
	}, &staged)
	require.Equal(t, "Staged summary", staged.Summary)

	var committed NodePreview
	getJSON(t, httpSrv.URL+"/api/v1/nodes/preview?ref=specs%2Fproposed.md", &committed)
	require.Equal(t, "Proposed spec", committed.Summary)
}

func TestStagedReadsReflectStagedSourceFragmentsAndTags(t *testing.T) {
	t.Parallel()

	fixture := prepareIdentityStatusFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	const notePath = "specs/proposed.md"
	original, err := os.ReadFile(filepath.Join(fixture.root, notePath))
	require.NoError(t, err)
	base, err := buildMarkdownDocumentSnapshotCompat(notePath, string(original), time.Time{})
	require.NoError(t, err)

	var created OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{
		Ops: []OntologyEditOp{
			{
				Kind:     "setSource",
				Path:     notePath,
				Markdown: string(original) + "\nTagged #staged-tag\n",
				Expected: &OntologyEditExpected{SourceHash: base.ContentFingerprint, SourceContent: string(original)},
			},
			{Kind: "addSectionScaffold", Path: notePath, Level: "H2", Value: "Staged heading"},
		},
	}, &created)
	session := stagedReadRequest{EditSession: &EditSessionReadRequest{SessionID: created.SessionID}}

	previewURL := httpSrv.URL + "/api/v1/nodes/preview?ref=specs%2Fproposed.md%23Staged%20heading"
	var staged NodePreview
	postJSON(t, previewURL, session, &staged)
	require.True(t, staged.FragmentResolved)
	require.Equal(t, &NodePreviewFragment{Kind: "heading", Text: "Staged heading"}, staged.Fragment)

	var committed NodePreview
	getJSON(t, previewURL, &committed)
	require.False(t, committed.FragmentResolved)

	var relative NodePreview
	postJSON(t, httpSrv.URL+"/api/v1/nodes/preview?ref=.%2Fproposed.md%23Staged%20heading&from=specs%2Factive.md", session, &relative)
	require.True(t, relative.FragmentResolved)
	require.Equal(t, &NodePreviewFragment{Kind: "heading", Text: "Staged heading"}, relative.Fragment)

	tagsByPath := func(resp OntologyTypeResponse) map[string][]string {
		out := map[string][]string{}
		for _, note := range resp.Notes {
			out[note.Path] = note.Tags
		}
		return out
	}
	var stagedType OntologyTypeResponse
	postJSON(t, httpSrv.URL+"/api/v1/ontology/types/Spec", session, &stagedType)
	require.Contains(t, tagsByPath(stagedType)[notePath], "staged-tag")

	var committedType OntologyTypeResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/types/Spec", &committedType)
	require.NotContains(t, tagsByPath(committedType)[notePath], "staged-tag")
}

func TestViewFieldCandidatesPostReadsThroughEditSession(t *testing.T) {
	t.Parallel()

	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var created OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{
		Ops: []OntologyEditOp{{Kind: "setField", Path: "specs/100-demo/spec.md", Field: "title", Value: "Staged candidate title"}},
	}, &created)

	url := httpSrv.URL + "/api/v1/views/generated.type.Plan.table/field-candidates?field=spec"
	var staged appviews.FieldCandidatesResponse
	postJSON(t, url, stagedReadRequest{EditSession: &EditSessionReadRequest{SessionID: created.SessionID}}, &staged)
	candidateFor := func(response appviews.FieldCandidatesResponse) appviews.EditCandidate {
		t.Helper()
		for _, candidate := range response.Candidates {
			if candidate.Path == "specs/100-demo/spec.md" {
				return candidate
			}
		}
		require.Failf(t, "missing candidate", "no candidate for specs/100-demo/spec.md in %+v", response.Candidates)
		return appviews.EditCandidate{}
	}
	stagedCandidate := candidateFor(staged)
	require.Equal(t, "Staged candidate title", stagedCandidate.Label)
	require.Equal(t, "[[specs/100-demo/spec|Staged candidate title]]", stagedCandidate.Value)

	var committed appviews.FieldCandidatesResponse
	getJSON(t, url, &committed)
	committedCandidate := candidateFor(committed)
	require.Equal(t, "Demo Spec", committedCandidate.Label)
	require.Equal(t, "[[specs/100-demo/spec|Demo Spec]]", committedCandidate.Value)

	var failed ErrorResponse
	postJSONWithStatus(t, url, stagedReadRequest{EditSession: &EditSessionReadRequest{SessionID: "missing"}}, http.StatusBadRequest, &failed)
	require.NotEmpty(t, failed.Code)
}

func prepareIdentityStatusFixtureVault(t *testing.T) fixtureVault {
	t.Helper()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "specs"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "people"), 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
enum SpecStatus {
  proposed
  active
  archived
}

type Spec
  @node(paths: ["specs/*.md"], label: "Spec", keyField: "summary") {
  summary: String!
  specStatus: SpecStatus @field(source: "spec-status")
}

type Person
  @node(paths: ["people/*.md"], label: "Person", keyField: "summary") {
  summary: String!
}
`), 0o644))

	write := func(path, body string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(body), 0o644))
	}
	write("specs/proposed.md", `---
summary: Proposed spec
spec-status: proposed
---
# Proposed spec
`)
	write("specs/active.md", `---
summary: Active spec
spec-status: active
---
# Active spec
`)
	write("specs/empty.md", `---
summary: Empty spec
---
# Empty spec
`)
	write("people/alice.md", `---
summary: Alice profile
---
# Alice
`)

	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = store.Close()
	})

	vaultDef := obsidian.VaultDefinition{
		Name:  "identity-status-fixture",
		Path:  root,
		Links: obsidian.LinkTypeBoth,
	}
	_, err = ontology.EnsureFreshRuntimeWithStore(context.Background(), testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)

	return fixtureVault{
		root:       root,
		vault:      &obsidian.Vault{Name: "identity-status-fixture"},
		vaultDef:   vaultDef,
		intelStore: store,
	}
}

func prepareEmbeddedIdentityStatusFixtureVault(t *testing.T) fixtureVault {
	t.Helper()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "specs"), 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
enum StoryStatus {
  draft
  ready
  satisfied
}

type ProductSpec @node(paths: ["specs/*.md"]) {
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}

type StoriesSection implements Section {
  stories: [Story!] @contains(level: H3)
}

type Story implements Section @node(locator: EMBEDDED, keyField: "summary") {
  summary: String @field
  storyStatus: StoryStatus @field
}
`), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(root, "specs", "product.md"), []byte(`# Product

## Stories

### Story A
^story-a
summary:: First story
story-status:: ready

### Story B
^story-b
summary:: Second story
story-status:: satisfied
`), 0o644))

	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = store.Close()
	})

	vaultDef := obsidian.VaultDefinition{
		Name:  "embedded-identity-status-fixture",
		Path:  root,
		Links: obsidian.LinkTypeBoth,
	}
	_, err = ontology.EnsureFreshRuntimeWithStore(context.Background(), testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)

	return fixtureVault{
		root:       root,
		vault:      &obsidian.Vault{Name: "embedded-identity-status-fixture"},
		vaultDef:   vaultDef,
		intelStore: store,
	}
}

func TestOntologyNodeEventsStreamPublishesWatchHubUpdates(t *testing.T) {
	t.Parallel()

	fixture := prepareOntologyFixtureVault(t)
	hub, err := watchhub.NewHub(fixture.root, watchhub.Options{
		DisableFSNotify: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = hub.Close()
	})
	runtime := &Runtime{}
	runtime.SetWatchHub(hub)
	srv := newFixtureServer(t, fixture, runtime)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	req, err := http.NewRequest(http.MethodGet, httpSrv.URL+"/api/v1/nodes/events?ref=specs/100-demo/spec.md%23%5Evalidation&kind=EMBEDDED", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	reader := bufio.NewReader(resp.Body)
	events := make(chan ontology.NodeEvent, 1)
	go func() {
		if event, ok := readSSEEvent(reader); ok {
			events <- event
		}
	}()

	specPath := filepath.Join(fixture.root, "specs/100-demo/spec.md")
	data, err := os.ReadFile(specPath)
	require.NoError(t, err)
	updated := strings.Replace(string(data), "status:: READY", "status:: DONE", 1)
	require.NoError(t, os.WriteFile(specPath, []byte(updated), 0o644))

	hub.EmitHintPaths([]string{"specs/100-demo/spec.md"})

	var event ontology.NodeEvent
	select {
	case event = <-events:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ontology node event")
	}
	require.Equal(t, "node.updated", event.Kind)
	require.Equal(t, "filesystem", event.Cause)
	require.Equal(t, "specs/100-demo/spec.md", event.Ref.NotePath)
	require.Equal(t, "^validation", event.Ref.Fragment)
	require.Equal(t, ontology.NodeKindEmbedded, event.Ref.Kind)
}

func TestOntologyNodeEventsReportsPublicStreamingUnsupported(t *testing.T) {
	t.Parallel()

	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	w := &nonFlushingResponseWriter{header: make(http.Header)}
	req := newApplicationRequest(http.MethodGet, "/api/v1/nodes/events?ref=specs/100-demo/spec.md", nil)

	srv.handleNodeEvents(w, req)

	require.Equal(t, http.StatusInternalServerError, w.status)
	var body ErrorResponse
	require.NoError(t, json.Unmarshal(w.body.Bytes(), &body))
	require.Equal(t, PublicErrorStreamingUnsupported, body.Code)
}

func TestPublicEventsStreamPublishesInvalidationEnvelope(t *testing.T) {
	t.Parallel()

	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	req, err := http.NewRequest(http.MethodGet, httpSrv.URL+"/api/v1/events", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "text/event-stream")

	reader := bufio.NewReader(resp.Body)
	connected, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(strings.TrimSpace(connected), ": connected"))

	srv.NotifyGlobalEvent(GlobalEventSchemaInvalidated, map[string]any{"reason": "test"})
	event, ok := readGlobalSSEEvent(reader)
	require.True(t, ok)
	require.Equal(t, GlobalEventSchemaInvalidated, event.Kind)
	require.NotEmpty(t, event.ID)
	require.NotNil(t, event.Data)
}

func TestOntologyNodeEventsStreamStalesOtherOpenPanesForRelationRefresh(t *testing.T) {
	t.Parallel()

	fixture := prepareOntologyFixtureVault(t)
	hub, err := watchhub.NewHub(fixture.root, watchhub.Options{
		DisableFSNotify: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = hub.Close()
	})
	runtime := &Runtime{}
	runtime.SetWatchHub(hub)
	srv := newFixtureServer(t, fixture, runtime)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	req, err := http.NewRequest(http.MethodGet, httpSrv.URL+"/api/v1/nodes/events?ref=specs/100-demo/plan.md", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	reader := bufio.NewReader(resp.Body)
	events := make(chan ontology.NodeEvent, 1)
	go func() {
		if event, ok := readSSEEvent(reader); ok {
			events <- event
		}
	}()

	specPath := filepath.Join(fixture.root, "specs/100-demo/spec.md")
	data, err := os.ReadFile(specPath)
	require.NoError(t, err)
	updated := strings.Replace(string(data), "status:: READY", "status:: DONE", 1)
	require.NoError(t, os.WriteFile(specPath, []byte(updated), 0o644))

	hub.EmitHintPaths([]string{"specs/100-demo/spec.md"})

	var event ontology.NodeEvent
	select {
	case event = <-events:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ontology stale event")
	}
	require.Equal(t, "node.stale", event.Kind)
	require.Equal(t, "filesystem", event.Cause)
	require.Equal(t, []string{"relations"}, event.Changed)
	require.Equal(t, "specs/100-demo/plan.md", event.Ref.NotePath)
	require.Equal(t, ontology.NodeKindNote, event.Ref.Kind)
}

func TestOntologyNodeEventsStreamAcceptsCommaInRefPath(t *testing.T) {
	t.Parallel()

	fixture := prepareOntologyFixtureVault(t)
	commaPath := filepath.Join(fixture.root, "specs/100-demo/Notes, 2026.md")
	require.NoError(t, os.WriteFile(commaPath, []byte(`---
type: Plan
summary: Comma note
---
# Notes, 2026

## Validation

Plan body.
`), 0o644))

	hub, err := watchhub.NewHub(fixture.root, watchhub.Options{
		DisableFSNotify: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = hub.Close()
	})
	runtime := &Runtime{}
	runtime.SetWatchHub(hub)
	srv := newFixtureServer(t, fixture, runtime)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	req, err := http.NewRequest(
		http.MethodGet,
		httpSrv.URL+"/api/v1/nodes/events?ref="+url.QueryEscape("specs/100-demo/Notes, 2026.md"),
		nil,
	)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestOntologySummaryDegradesWhenQuerySchemaBuildFails(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	// CodeRuntime is a valid ontology type that collides with a built-in
	// runtime GraphQL type, so the schema loads but cannot be executed.
	schemaPath := filepath.Join(fixture.root, ".rhizome", "ontology", "schema.graphql")
	schemaFile, err := os.OpenFile(schemaPath, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = schemaFile.WriteString("\ntype CodeRuntime @node(paths: [\"notes/code-runtime/*.md\"]) {\n  name: String!\n}\n")
	require.NoError(t, err)
	require.NoError(t, schemaFile.Close())
	_, err = ontology.EnsureFreshRuntimeWithStore(context.Background(), testNoteMetadataIndexer(t), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
	require.NoError(t, err)
	const schemaConflict = `ontology type "CodeRuntime" conflicts with built-in runtime GraphQL type`

	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var summary OntologySummaryResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/summary", &summary)
	require.True(t, summary.SchemaPresent)
	require.False(t, summary.QuerySchemaPresent)
	require.Contains(t, summary.QuerySchemaError, schemaConflict)
	require.Equal(t, 5, summary.TypedNotes)
	require.NotEmpty(t, summary.Types)

	var allNotes OntologyTypeResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/types/__all__", &allNotes)
	require.Equal(t, 5, allNotes.Count)
	require.Len(t, allNotes.Notes, 5)
	require.Contains(t, []string{"Spec", "Plan", "Research", "Tasks", "Quickstart"}, allNotes.Notes[0].ResolvedType)

	var querySchema OntologyQuerySchemaResponse
	getJSON(t, httpSrv.URL+"/api/v1/ontology/query-schema", &querySchema)
	require.True(t, querySchema.SchemaPresent)
	require.False(t, querySchema.QuerySchemaPresent)
	require.Contains(t, querySchema.Error, schemaConflict)
	require.Empty(t, querySchema.SDL)

	resp, err := http.Post(
		httpSrv.URL+"/api/v1/graphql",
		"application/json",
		bytes.NewReader([]byte(`{"query":"{ notes(find:\"demo\") { path } }"}`)),
	)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "ontology query schema is unavailable")

	publicSchemaResp, err := http.Get(httpSrv.URL + "/api/v1/graphql/schema")
	require.NoError(t, err)
	defer publicSchemaResp.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, publicSchemaResp.StatusCode)
	publicSchemaBody, err := io.ReadAll(publicSchemaResp.Body)
	require.NoError(t, err)
	require.Contains(t, string(publicSchemaBody), PublicErrorSchemaUnavailable)
}

func TestPublicEditSessions_MultiNodePreviewAndCommit(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var created OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{}, &created)
	require.NotEmpty(t, created.SessionID)
	require.Equal(t, OntologyEditSessionStatusClean, created.Status)
	require.False(t, created.HasUncommittedChanges)

	var staged OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/stage", OntologyEditSessionStageRequest{
		Ops: []OntologyEditOp{
			{Kind: "setField", Path: "specs/100-demo/spec.md", Field: "summary", Value: "Demo spec updated"},
			{Kind: "setField", Path: "specs/100-demo/spec.md#^validation", Field: "status", Value: "DONE"},
			{Kind: "setField", Path: "specs/100-demo/plan.md", Field: "summary", Value: "Demo plan updated"},
		},
	}, &staged)
	require.Equal(t, created.SessionID, staged.SessionID)
	require.Equal(t, OntologyEditSessionStatusDirty, staged.Status)
	require.True(t, staged.HasUncommittedChanges)
	require.ElementsMatch(t, []string{"specs/100-demo/plan.md", "specs/100-demo/spec.md"}, staged.TouchedPaths)
	require.Contains(t, staged.TouchedNodes, "specs/100-demo/spec.md#^validation")
	require.Len(t, staged.TouchedNodeRefs, 3)
	require.Contains(t, staged.TouchedNodeRefs, ontology.NodeRef{NotePath: "specs/100-demo/spec.md", Kind: ontology.NodeKindNote})
	require.Contains(t, staged.TouchedNodeRefs, ontology.NodeRef{NotePath: "specs/100-demo/plan.md", Kind: ontology.NodeKindNote})
	embeddedTouched := false
	for _, ref := range staged.TouchedNodeRefs {
		if ref.NotePath != "specs/100-demo/spec.md" || ref.Fragment != "^validation" {
			continue
		}
		require.Equal(t, ontology.NodeKindEmbedded, ref.Kind)
		require.NotEmpty(t, ref.NodeID)
		require.NotEmpty(t, ref.Structural)
		embeddedTouched = true
	}
	require.True(t, embeddedTouched)
	require.ElementsMatch(t, []string{"summary"}, staged.ChangedFieldsByNode["specs/100-demo/spec.md"])
	require.ElementsMatch(t, []string{"status"}, staged.ChangedFieldsByNode["specs/100-demo/spec.md#^validation"])
	require.ElementsMatch(t, []string{"summary"}, staged.ChangedFieldsByNode["specs/100-demo/plan.md"])
	require.ElementsMatch(t, []string{"summary"}, staged.ChangedFieldsByNodeRef[canonicalNodeRefKey(ontology.NodeRef{
		NotePath: "specs/100-demo/spec.md",
		Kind:     ontology.NodeKindNote,
	})])
	require.ElementsMatch(t, []string{"summary"}, staged.ChangedFieldsByNodeRef[canonicalNodeRefKey(ontology.NodeRef{
		NotePath: "specs/100-demo/plan.md",
		Kind:     ontology.NodeKindNote,
	})])
	foundEmbeddedFieldChange := false
	for key, fields := range staged.ChangedFieldsByNodeRef {
		if !strings.Contains(key, "specs/100-demo/spec.md|^validation|") {
			continue
		}
		require.ElementsMatch(t, []string{"status"}, fields)
		foundEmbeddedFieldChange = true
	}
	require.True(t, foundEmbeddedFieldChange)

	var preview OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/preview", map[string]any{}, &preview)
	require.Equal(t, OntologyEditSessionStatusDirty, preview.Status)
	require.NotNil(t, preview.Plan)
	require.Len(t, preview.Plan.Files, 2)
	require.Contains(t, preview.Plan.Files[0].Diff+preview.Plan.Files[1].Diff, "Demo spec updated")
	require.Contains(t, preview.Plan.Files[0].Diff+preview.Plan.Files[1].Diff, "Demo plan updated")
	require.Contains(t, preview.Plan.Files[0].Diff+preview.Plan.Files[1].Diff, "DONE")

	var committed OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/commit", map[string]any{}, &committed)
	require.Equal(t, OntologyEditSessionStatusClean, committed.Status)
	require.False(t, committed.HasUncommittedChanges)
	require.Empty(t, committed.Ops)
	require.NotNil(t, committed.Plan)
	require.NotEmpty(t, committed.Workspaces)

	specData, err := os.ReadFile(filepath.Join(fixture.root, "specs/100-demo/spec.md"))
	require.NoError(t, err)
	require.Contains(t, string(specData), "summary: Demo spec updated")
	require.Contains(t, string(specData), "status:: DONE")

	planData, err := os.ReadFile(filepath.Join(fixture.root, "specs/100-demo/plan.md"))
	require.NoError(t, err)
	require.Contains(t, string(planData), "summary: Demo plan updated")

	var fetched OntologyEditSessionResponse
	getJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID, &fetched)
	require.Equal(t, OntologyEditSessionStatusClean, fetched.Status)
	require.Empty(t, fetched.Ops)
	require.False(t, fetched.HasUncommittedChanges)
}

func TestPublicEditSessions_CoalescesSupersededFieldEditsAndPrunesNoOps(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var created OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{}, &created)
	require.Equal(t, OntologyEditSessionStatusClean, created.Status)

	var first OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/stage", OntologyEditSessionStageRequest{
		Ops: []OntologyEditOp{{
			Kind:  "setField",
			Path:  "specs/100-demo/plan.md",
			Field: "summary",
			Value: "Temporary plan summary",
		}},
	}, &first)
	require.Equal(t, OntologyEditSessionStatusDirty, first.Status)
	require.Len(t, first.Ops, 1)
	require.Equal(t, "Temporary plan summary", first.Ops[0].Value)

	var second OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/stage", OntologyEditSessionStageRequest{
		Ops: []OntologyEditOp{{
			Kind:  "setField",
			Path:  "specs/100-demo/plan.md",
			Field: "summary",
			Value: "Final plan summary",
		}},
	}, &second)
	require.Equal(t, OntologyEditSessionStatusDirty, second.Status)
	require.Len(t, second.Ops, 1)
	require.Equal(t, "Final plan summary", second.Ops[0].Value)
	require.Equal(t, []string{"specs/100-demo/plan.md"}, second.TouchedPaths)

	var reverted OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/stage", OntologyEditSessionStageRequest{
		Ops: []OntologyEditOp{{
			Kind:  "setField",
			Path:  "specs/100-demo/plan.md",
			Field: "summary",
			Value: "Demo plan summary",
		}},
	}, &reverted)
	require.Equal(t, OntologyEditSessionStatusClean, reverted.Status)
	require.False(t, reverted.HasUncommittedChanges)
	require.Empty(t, reverted.Ops)
	require.Empty(t, reverted.TouchedPaths)
}

func TestPublicEditSessions_PrunesEmbeddedTitleRevertedToDisk(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	schemaPath := filepath.Join(fixture.root, ".rhizome", "ontology", "schema.graphql")
	schemaData, err := os.ReadFile(schemaPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(schemaPath, append(schemaData, []byte(`

type ActionItem implements Section @node(locator: EMBEDDED) {
  title: String!
  done: Boolean! @field(sourceKind: CHECKBOX)
}

type Party @node(paths: ["docs/playground/*.md"]) {
  title: String!
  actionItems: [ActionItem!] @contains(shape: CHECKBOX_ITEM, marker: "#action-item")
}
`)...), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(fixture.root, "docs", "playground"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, "docs", "playground", "party.md"), []byte(`---
title: Party
---

# Party

## Action Items

- [ ] Bring the cooler back from the garage #action-item ^cooler
`), 0o644))

	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var created OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{}, &created)
	require.Equal(t, OntologyEditSessionStatusClean, created.Status)

	var changed OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/stage", OntologyEditSessionStageRequest{
		Ops: []OntologyEditOp{{
			Kind:  "setField",
			Path:  "docs/playground/party.md#^cooler",
			Field: "title",
			Value: "Bring the cooler back from the garage.",
		}},
	}, &changed)
	require.Equal(t, OntologyEditSessionStatusDirty, changed.Status)
	require.Len(t, changed.Ops, 1)

	var reverted OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/stage", OntologyEditSessionStageRequest{
		Ops: []OntologyEditOp{{
			Kind:  "setField",
			Path:  "docs/playground/party.md#^cooler",
			Field: "title",
			Value: "Bring the cooler back from the garage",
		}},
	}, &reverted)
	require.Equal(t, OntologyEditSessionStatusClean, reverted.Status)
	require.False(t, reverted.HasUncommittedChanges)
	require.Empty(t, reverted.Ops)
	require.Empty(t, reverted.TouchedPaths)
}

func TestOntologyEditSessions_DiffReturnsModifiedNotes(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var created OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{}, &created)
	require.NotEmpty(t, created.SessionID)

	var staged OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/stage", OntologyEditSessionStageRequest{
		Ops: []OntologyEditOp{
			{Kind: "setField", Path: "specs/100-demo/spec.md", Field: "summary", Value: "Summary via diff"},
			{Kind: "setLinkField", Path: "specs/100-demo/plan.md", Field: "spec", Values: []string{"specs/100-demo/spec"}},
			{Kind: "addEmbeddedNode", Path: "specs/100-demo/spec.md#Stories", Collection: "stories", Heading: "Story A", Body: "status:: TODO", BlockID: "story-a"},
			{Kind: "reorderCollection", Path: "specs/100-demo/spec.md#Stories", Collection: "stories", OrderedFragments: []string{"^story-a", "^validation"}},
			{Kind: "deleteNode", Path: "specs/100-demo/spec.md#^validation"},
		},
	}, &staged)
	require.True(t, staged.HasUncommittedChanges)

	var diffResp ModifiedNotesResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/diff", map[string]any{}, &diffResp)
	require.Equal(t, created.SessionID, diffResp.SessionID)
	require.Equal(t, 2, diffResp.Totals.Notes)
	require.Equal(t, 5, diffResp.Totals.Ops)
	require.Equal(t, 1, diffResp.Totals.SetField)
	require.Equal(t, 1, diffResp.Totals.SetLinkField)
	require.Equal(t, 1, diffResp.Totals.AddEmbedded)
	require.Equal(t, 1, diffResp.Totals.Reorder)
	require.Equal(t, 1, diffResp.Totals.Delete)

	entryByPath := map[string]ModifiedNoteEntry{}
	for _, entry := range diffResp.Notes {
		entryByPath[entry.Path] = entry
	}
	specEntry, ok := entryByPath["specs/100-demo/spec.md"]
	require.True(t, ok, "expected spec.md entry")
	require.Contains(t, specEntry.Diff, "Summary via diff")
	require.Len(t, specEntry.Ops, 4)
	kinds := map[string]ModifiedNoteOpView{}
	for _, op := range specEntry.Ops {
		kinds[op.Kind] = op
	}
	require.Equal(t, "Summary via diff", kinds["setField"].Value)
	require.NotEqual(t, kinds["setField"].Value, kinds["setField"].PreviousValue)
	require.Equal(t, "Story A", kinds["addEmbeddedNode"].Heading)
	require.Equal(t, []string{"^story-a", "^validation"}, kinds["reorderCollection"].OrderedFragments)
	require.NotEmpty(t, kinds["reorderCollection"].PreviousFragments)
	require.Equal(t, "deleteNode", kinds["deleteNode"].Kind)
	require.Equal(t, "Validation story", kinds["deleteNode"].NodeTitle, "an op inside a note names the node it changes")
	require.Empty(t, kinds["setField"].NodeTitle, "a note-level op is already named by its note entry")

	planEntry, ok := entryByPath["specs/100-demo/plan.md"]
	require.True(t, ok, "expected plan.md entry")
	require.Len(t, planEntry.Ops, 1)
	require.Equal(t, "setLinkField", planEntry.Ops[0].Kind)
	require.Equal(t, []string{"specs/100-demo/spec"}, planEntry.Ops[0].Values)
	require.NotEmpty(t, planEntry.Ops[0].PreviousValues)
}

// TestOntologyEditSessions_DiffPreservesBaseFingerprintAcrossDiskChange guards
// the contract that `/diff` reports against the baseline captured when ops were
// staged, not whatever happens to be on disk at query time. If the file drifts
// after staging, the diff must flag the note as rebased/stale rather than
// silently redrawing against the new contents.
func TestOntologyEditSessions_DiffPreservesBaseFingerprintAcrossDiskChange(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var created OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{
		Ops: []OntologyEditOp{
			{Kind: "setField", Path: "specs/100-demo/plan.md", Field: "summary", Value: "Baseline-anchored summary"},
		},
	}, &created)
	require.Equal(t, OntologyEditSessionStatusDirty, created.Status)

	planPath := filepath.Join(fixture.root, "specs/100-demo/plan.md")
	original, err := os.ReadFile(planPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(planPath, append(original, []byte("\nExternal tail after staging.\n")...), 0o644))

	var diffResp ModifiedNotesResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/diff", map[string]any{}, &diffResp)
	require.True(t, diffResp.Rebased, "diff should flag a rebase when disk drifts after staging")
	require.Contains(t, diffResp.StalePaths, "specs/100-demo/plan.md")
	require.Len(t, diffResp.Notes, 1)
	require.True(t, diffResp.Notes[0].Rebased)
}

func TestOntologyEditSessions_CollectionOpsAndConflict(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var created OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{
		Ops: []OntologyEditOp{
			{Kind: "addEmbeddedNode", Path: "specs/100-demo/spec.md#Stories", Collection: "stories", Heading: "Story B", Body: "status:: TODO", BlockID: "story-b"},
			{Kind: "addEmbeddedNode", Path: "specs/100-demo/spec.md#Stories", Collection: "stories", Heading: "Story C", Body: "status:: TODO", BlockID: "story-c"},
			{Kind: "reorderCollection", Path: "specs/100-demo/spec.md#Stories", Collection: "stories", OrderedFragments: []string{"^story-c", "^validation", "^story-b"}},
			{Kind: "deleteNode", Path: "specs/100-demo/spec.md#^validation"},
		},
	}, &created)
	require.Equal(t, OntologyEditSessionStatusDirty, created.Status)
	require.NotEmpty(t, created.CollectionChanges)
	require.NotNil(t, created.CollectionChanges[0].Ref)
	require.Equal(t, "specs/100-demo/spec.md", created.CollectionChanges[0].Ref.NotePath)
	require.Equal(t, ontology.NodeKindSection, created.CollectionChanges[0].Ref.Kind)

	var committed OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/commit", map[string]any{}, &committed)
	require.Equal(t, OntologyEditSessionStatusClean, committed.Status)
	require.NotEmpty(t, committed.Workspaces)

	data, err := os.ReadFile(filepath.Join(fixture.root, "specs/100-demo/spec.md"))
	require.NoError(t, err)
	text := string(data)
	require.NotContains(t, text, "^validation")
	require.Contains(t, text, "^story-b")
	require.Contains(t, text, "^story-c")
	require.True(t, strings.Index(text, "^story-c") < strings.Index(text, "^story-b"))

	writeText := `---
type: Spec
summary: Demo spec summary
---
# Demo Spec

## Requirements

- Must support ontology browsing.
- See [[plan]] for execution.
- ![[quickstart]]

## Stories

### Validation story
status:: READY
^validation

- Story tied to validation checks.
- Follow [[plan]] while validating.

### Story B
status:: TODO
^story-b
`
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, "specs/100-demo/spec.md"), []byte(writeText), 0o644))

	var conflictSession OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{
		Ops: []OntologyEditOp{
			{Kind: "reorderCollection", Path: "specs/100-demo/spec.md#Stories", Collection: "stories", OrderedFragments: []string{"^story-b", "^validation"}},
		},
	}, &conflictSession)
	require.Equal(t, OntologyEditSessionStatusDirty, conflictSession.Status)

	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, "specs/100-demo/spec.md"), []byte(writeText+`

### Story C
status:: TODO
^story-c
`), 0o644))

	var conflicted OntologyEditSessionResponse
	getJSONStatus(t, httpSrv.URL+"/api/v1/edit-sessions/"+conflictSession.SessionID, http.StatusOK, &conflicted)
	require.Equal(t, OntologyEditSessionStatusConflicted, conflicted.Status)
	require.NotEmpty(t, conflicted.Conflicts)
	require.Equal(t, ontology.ConflictKindCollectionDrift, conflicted.Conflicts[0].Kind)
}

func TestOntologyEditSessions_RebasesAfterExternalChange(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var created OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{
		Ops: []OntologyEditOp{
			{Kind: "setField", Path: "specs/100-demo/plan.md", Field: "summary", Value: "Rebased plan summary"},
		},
	}, &created)
	require.Equal(t, OntologyEditSessionStatusDirty, created.Status)

	path := filepath.Join(fixture.root, "specs/100-demo/plan.md")
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(original, []byte("\nExternal tail.\n")...), 0o644))

	var fetched OntologyEditSessionResponse
	getJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID, &fetched)
	require.Equal(t, OntologyEditSessionStatusRebased, fetched.Status)
	require.True(t, fetched.Rebased)
	require.Contains(t, fetched.StalePaths, "specs/100-demo/plan.md")

	var committed OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/commit", map[string]any{}, &committed)
	require.Equal(t, OntologyEditSessionStatusClean, committed.Status)

	updated, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(updated), "summary: Rebased plan summary")
	require.Contains(t, string(updated), "External tail.")
}

func TestOntologyEditSessions_RestoresFromClientSnapshot(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var created OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{
		Ops: []OntologyEditOp{
			{Kind: "setField", Path: "specs/100-demo/spec.md#^validation", Field: "status", Value: "DONE"},
			{Kind: "setField", Path: "specs/100-demo/plan.md", Field: "summary", Value: "Recovered plan summary"},
		},
	}, &created)
	require.Equal(t, OntologyEditSessionStatusDirty, created.Status)
	require.Len(t, created.Ops, 2)

	req, err := http.NewRequest(http.MethodDelete, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var preview OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/preview", OntologyEditSessionPreviewRequest{
		Snapshot: &OntologyEditSessionSnapshot{
			Version: 3, Revision: created.Revision, SessionID: created.SessionID,
			Ops: created.Ops, BaseFingerprints: created.BaseFingerprints, BaseDocuments: created.BaseDocuments,
		},
	}, &preview)
	require.Equal(t, created.SessionID, preview.SessionID)
	require.Equal(t, OntologyEditSessionStatusDirty, preview.Status)
	require.NotNil(t, preview.Plan)
	require.Contains(t, preview.Plan.Files[0].Diff+preview.Plan.Files[1].Diff, "Recovered plan summary")
	require.Contains(t, preview.Plan.Files[0].Diff+preview.Plan.Files[1].Diff, "DONE")

	var committed OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/commit", OntologyEditSessionCommitRequest{
		Snapshot: &OntologyEditSessionSnapshot{
			Version: 3, Revision: created.Revision, SessionID: created.SessionID,
			Ops: created.Ops, BaseFingerprints: created.BaseFingerprints, BaseDocuments: created.BaseDocuments,
		},
	}, &committed)
	require.Equal(t, OntologyEditSessionStatusClean, committed.Status)
	require.NotEmpty(t, committed.Workspaces)

	specData, err := os.ReadFile(filepath.Join(fixture.root, "specs/100-demo/spec.md"))
	require.NoError(t, err)
	require.Contains(t, string(specData), "status:: DONE")

	planData, err := os.ReadFile(filepath.Join(fixture.root, "specs/100-demo/plan.md"))
	require.NoError(t, err)
	require.Contains(t, string(planData), "summary: Recovered plan summary")
}

func TestOntologyEditSessions_RestoresSnapshotAfterExternalRebase(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var created OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{
		Ops: []OntologyEditOp{
			{Kind: "setField", Path: "specs/100-demo/plan.md", Field: "summary", Value: "Recovered rebased summary"},
		},
	}, &created)
	require.Equal(t, OntologyEditSessionStatusDirty, created.Status)
	require.Len(t, created.Ops, 1)

	req, err := http.NewRequest(http.MethodDelete, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	path := filepath.Join(fixture.root, "specs/100-demo/plan.md")
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(original, []byte("\nExternal tail.\n")...), 0o644))

	var preview OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/preview", OntologyEditSessionPreviewRequest{
		Snapshot: &OntologyEditSessionSnapshot{
			Version: 3, Revision: created.Revision, SessionID: created.SessionID,
			Ops: created.Ops, BaseFingerprints: created.BaseFingerprints, BaseDocuments: created.BaseDocuments,
		},
	}, &preview)
	require.Equal(t, created.SessionID, preview.SessionID)
	require.Equal(t, OntologyEditSessionStatusRebased, preview.Status)
	require.True(t, preview.Rebased)
	require.Contains(t, preview.StalePaths, "specs/100-demo/plan.md")
	require.NotNil(t, preview.Plan)

	var committed OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/commit", OntologyEditSessionCommitRequest{
		Snapshot: &OntologyEditSessionSnapshot{
			Version: 3, Revision: created.Revision, SessionID: created.SessionID,
			Ops: created.Ops, BaseFingerprints: created.BaseFingerprints, BaseDocuments: created.BaseDocuments,
		},
	}, &committed)
	require.Equal(t, OntologyEditSessionStatusClean, committed.Status)
	require.True(t, committed.Rebased)
	require.Contains(t, committed.StalePaths, "specs/100-demo/plan.md")
	require.NotEmpty(t, committed.Workspaces)

	updated, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(updated), "summary: Recovered rebased summary")
	require.Contains(t, string(updated), "External tail.")
}

func TestOntologyEditSessions_StageAfterRebasedPreviewPreservesOriginalBase(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var created OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{
		Ops: []OntologyEditOp{
			{Kind: "setField", Path: "specs/100-demo/plan.md", Field: "summary", Value: "Rebased summary"},
		},
	}, &created)
	require.Equal(t, OntologyEditSessionStatusDirty, created.Status)

	path := filepath.Join(fixture.root, "specs/100-demo/plan.md")
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(original, []byte("\nExternal tail.\n")...), 0o644))

	var rebased OntologyEditSessionResponse
	getJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID, &rebased)
	require.Equal(t, OntologyEditSessionStatusRebased, rebased.Status)
	require.True(t, rebased.Rebased)

	var staged OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/stage", OntologyEditSessionStageRequest{
		Ops: []OntologyEditOp{
			{Kind: "setField", Path: "specs/100-demo/plan.md", Field: "summary", Value: "Rebased summary v2"},
		},
	}, &staged)
	require.Equal(t, OntologyEditSessionStatusDirty, staged.Status)
	require.False(t, staged.Rebased)
	require.Contains(t, staged.BaseFingerprints, "specs/100-demo/plan.md")
	require.Equal(t, created.BaseFingerprints["specs/100-demo/plan.md"], staged.BaseFingerprints["specs/100-demo/plan.md"])

	var committed OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/commit", map[string]any{}, &committed)
	require.Equal(t, OntologyEditSessionStatusClean, committed.Status)

	updated, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(updated), "summary: Rebased summary v2")
	require.Contains(t, string(updated), "External tail.")
}

func TestOntologyRenderedNoteRejectsIgnoredPaths(t *testing.T) {
	t.Parallel()

	fixture := prepareOntologyFixtureVault(t)
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, ".rhizome", "ignore"), []byte("specs/100-demo/hidden.md\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, "specs", "100-demo", "hidden.md"), []byte("# hidden\n"), 0o644))

	srv, err := NewServer(context.Background(), Config{
		Vault:        fixture.vault,
		VaultDef:     fixture.vaultDef,
		VaultPath:    fixture.root,
		NoteMetadata: testNoteMetadataIndexer(t),
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = srv.Close()
	})

	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	resp, err := http.Get(httpSrv.URL + "/api/v1/files/rendered?path=specs/100-demo/hidden.md")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "path is ignored")
}

func TestServerServesSPAForNotesRoutes(t *testing.T) {
	t.Parallel()

	fixture := prepareWebFixtureVault(t)
	assets := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<html>notes ui</html>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log('ok')")},
	}

	srv, err := NewServer(context.Background(), Config{
		Vault:        fixture.vault,
		VaultDef:     fixture.vaultDef,
		VaultPath:    fixture.root,
		NoteMetadata: testNoteMetadataIndexer(t),
	}, fs.FS(assets))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = srv.Close()
	})

	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	resp, err := http.Get(httpSrv.URL + "/notes")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "notes ui")

	graphQLResp, err := http.Get(httpSrv.URL + "/graphql")
	require.NoError(t, err)
	defer graphQLResp.Body.Close()
	require.Equal(t, http.StatusOK, graphQLResp.StatusCode)
	graphQLBody, err := io.ReadAll(graphQLResp.Body)
	require.NoError(t, err)
	require.Contains(t, string(graphQLBody), "notes ui")

	assetResp, err := http.Get(httpSrv.URL + "/assets/app.js")
	require.NoError(t, err)
	defer assetResp.Body.Close()
	require.Equal(t, http.StatusOK, assetResp.StatusCode)
	assetBody, err := io.ReadAll(assetResp.Body)
	require.NoError(t, err)
	require.True(t, strings.Contains(string(assetBody), "console.log"))

	apiResp, err := http.Get(httpSrv.URL + "/api/status")
	require.NoError(t, err)
	defer apiResp.Body.Close()
	require.Equal(t, http.StatusNotFound, apiResp.StatusCode)
	require.Contains(t, apiResp.Header.Get("Content-Type"), "application/json")
	apiBody, err := io.ReadAll(apiResp.Body)
	require.NoError(t, err)
	require.NotContains(t, string(apiBody), "notes ui")
	require.Contains(t, string(apiBody), PublicErrorNotFound)
}

func TestServerGlobalGraphReturnsWithoutPersistedScores(t *testing.T) {
	t.Parallel()

	fixture := prepareWebFixtureVault(t)
	require.NoError(t, fixture.intelStore.ReplaceGraphDocScores(context.Background(), nil))
	require.NoError(t, fixture.intelStore.ReplaceAnchorScores(context.Background(), nil))

	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var graph GraphResponse
	getJSON(t, httpSrv.URL+"/api/v1/graphs/global", &graph)
	require.False(t, graph.NeedsIndex)
	require.NotEmpty(t, graph.Nodes)
	require.NotEmpty(t, graph.Edges)
}

func TestWorkspaceGroups_UsesOtherEndpointForInboundAmbientRelations(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "concepts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "concepts", "information-velocity.md"), []byte("# Information Velocity\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "concepts", "clarity-loop.md"), []byte("# Clarity Loop\n"), 0o644))
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceOntologySnapshot(context.Background(), semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{
				SrcPath:      "concepts/clarity-loop.md",
				RelationName: "connectedConcepts",
				DstPath:      "concepts/information-velocity.md",
				DstType:      "concept",
				Provenance:   "body_link",
				Structural:   false,
				UpdatedAt:    1,
			},
			{
				SrcPath:      "concepts/information-velocity.md",
				RelationName: "connectedConcepts",
				DstPath:      "concepts/clarity-loop.md",
				DstType:      "concept",
				Provenance:   "body_link",
				Structural:   false,
				UpdatedAt:    1,
			},
		},
	}))

	srv := &Server{
		cfg: Config{
			VaultDef:     obsidian.VaultDefinition{Name: "ambient", Path: root, Links: obsidian.LinkTypeBoth},
			VaultPath:    root,
			NoteMetadata: testNoteMetadataIndexer(t),
		},
		runtime: &Runtime{},
	}
	srv.runtime.SetIntelStore(store)

	groups, err := srv.workspaceGroups(context.Background(), nil, "concepts/information-velocity.md", RenderedFileResponse{
		Path: "concepts/information-velocity.md",
	}, ontology.InspectNote{})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, "ambient", groups[0].Key)
	require.Len(t, groups[0].Items, 1)
	require.Equal(t, "concepts/clarity-loop.md", groups[0].Items[0].Path)
}

func TestBuildGlobalGraphMode_NotesOnlySkipsModuleCollapse(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = store.Close()
	})

	notes := make([]semdb.NoteMetadataRow, 0, 260)
	edges := make([]semdb.GraphDocEdgeRow, 0, 259)
	for i := 0; i < 260; i++ {
		path := filepath.ToSlash(filepath.Join("notes", "bulk", strings.Join([]string{"note", strconv.Itoa(i)}, "-")+".md"))
		notes = append(notes, semdb.NoteMetadataRow{
			Path:        path,
			Title:       "Note " + strconv.Itoa(i),
			ContentHash: strconv.Itoa(i),
			Mtime:       int64(i + 1),
			Size:        1,
		})
		if i == 0 {
			continue
		}
		edges = append(edges, semdb.GraphDocEdgeRow{
			SrcPath: "notes/bulk/note-0.md",
			DstPath: path,
			Kind:    semdb.GraphDocEdgeKindWikilink,
		})
	}

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(context.Background(), semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{
			NotesHash:    "notes-hash",
			RawNotesHash: "raw-notes-hash",
			LoadedAt:     1,
			Ready:        true,
		},
		Notes:         notes,
		WikilinkEdges: edges,
	}))

	runtime := &Runtime{}
	runtime.SetIntelStore(store)
	srv := &Server{
		cfg: Config{
			VaultDef:     obsidian.VaultDefinition{Name: "notes-only", Path: root, Links: obsidian.LinkTypeBoth},
			VaultPath:    root,
			NoteMetadata: testNoteMetadataIndexer(t),
		},
		runtime: runtime,
	}

	graph, err := srv.buildGlobalGraphMode(context.Background(), 100, 2, true, false)
	require.NoError(t, err)
	require.Len(t, graph.Nodes, 260)
	require.NotEmpty(t, graph.Edges)
	for _, node := range graph.Nodes {
		require.Equal(t, "note", node.Kind)
		require.False(t, node.Collapsed)
		require.NotContains(t, node.ID, "module:")
	}
}

func TestServerGlobalGraphCacheInvalidatesWhenFingerprintChanges(t *testing.T) {
	t.Parallel()

	fixture := prepareWebFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)

	before, err := srv.buildGlobalGraph(context.Background(), 0, 0)
	require.NoError(t, err)
	again, err := srv.buildGlobalGraph(context.Background(), 0, 0)
	require.NoError(t, err)
	require.Len(t, before.Nodes, len(again.Nodes))
	require.Len(t, before.Edges, len(again.Edges))

	beforeFingerprint, err := fixture.intelStore.GraphWebFingerprint(context.Background())
	require.NoError(t, err)

	require.NoError(t, fixture.intelStore.UpsertFileMeta(context.Background(), codeanchor.FileMeta{
		Path: "src/new_feature.py",
		Lang: codeanchor.LangPy,
		Hash: "hash-new-feature",
	}))
	require.NoError(t, fixture.intelStore.ReplaceIntelCodeFile(context.Background(), "src/new_feature.py", []codeanchor.IntelAnchor{{
		AnchorID:    "new-feature",
		Lang:        codeanchor.LangPy,
		Kind:        "function",
		Path:        "src/new_feature.py",
		Symbol:      "new_feature",
		FQN:         "src.new_feature.new_feature",
		Fingerprint: "fp-new-feature",
	}}, nil, nil))

	afterFingerprint, err := fixture.intelStore.GraphWebFingerprint(context.Background())
	require.NoError(t, err)
	require.NotEqual(t, beforeFingerprint, afterFingerprint)

	after, err := srv.buildGlobalGraph(context.Background(), 0, 0)
	require.NoError(t, err)
	require.Greater(t, len(after.Nodes), len(before.Nodes))
}

func TestServerGlobalGraphCacheInvalidatesOnSecondHandleSameCountSwap(t *testing.T) {
	t.Parallel()

	fixture := prepareWebFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	ctx := context.Background()

	require.NoError(t, fixture.intelStore.ReplaceGraphDocEdgesForPath(ctx, "swap/a.md", semdb.GraphDocEdgeKindWikilink, []string{"swap/x.md"}))
	require.NoError(t, fixture.intelStore.ReplaceGraphDocEdgesForPath(ctx, "swap/b.md", semdb.GraphDocEdgeKindWikilink, []string{"swap/y.md"}))
	readGraph := func() GraphResponse {
		t.Helper()
		recorder := httptest.NewRecorder()
		request := newApplicationRequest(http.MethodGet, "/api/v1/graphs/global", nil)
		srv.Handler().ServeHTTP(recorder, request)
		require.Equal(t, http.StatusOK, recorder.Code)
		var graph GraphResponse
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &graph))
		return graph
	}
	before := readGraph()
	requireGraphEdge(t, before, "note:swap/a.md", "note:swap/x.md", semdb.GraphDocEdgeKindWikilink, "")
	requireGraphEdge(t, before, "note:swap/b.md", "note:swap/y.md", semdb.GraphDocEdgeKindWikilink, "")
	require.Empty(t, graphNodeByID(before.Nodes, "code:src/link-only.go"))

	second, err := semdb.Open(filepath.Join(fixture.root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })
	require.NoError(t, second.ReplaceGraphDocEdgesForPath(ctx, "swap/a.md", semdb.GraphDocEdgeKindWikilink, []string{"swap/y.md"}))
	require.NoError(t, second.ReplaceGraphDocEdgesForPath(ctx, "swap/b.md", semdb.GraphDocEdgeKindWikilink, []string{"swap/x.md"}))

	afterSwap := readGraph()
	requireGraphEdge(t, afterSwap, "note:swap/a.md", "note:swap/y.md", semdb.GraphDocEdgeKindWikilink, "")
	requireGraphEdge(t, afterSwap, "note:swap/b.md", "note:swap/x.md", semdb.GraphDocEdgeKindWikilink, "")
	requireNoGraphEdge(t, afterSwap, "note:swap/a.md", "note:swap/x.md", semdb.GraphDocEdgeKindWikilink)
	requireNoGraphEdge(t, afterSwap, "note:swap/b.md", "note:swap/y.md", semdb.GraphDocEdgeKindWikilink)
	require.Empty(t, graphNodeByID(afterSwap.Nodes, "code:src/link-only.go"))

	_, err = second.DB().ExecContext(ctx, `INSERT INTO doc_links(src_path, src_type, dst_path, dst_kind, updated_at) VALUES ('src/link-only.go', 'code', 'anchor-only', 'anchor', 1)`)
	require.NoError(t, err)
	afterCodeLink := readGraph()
	requireGraphNode(t, afterCodeLink, "code:src/link-only.go", "code")
}

func TestServerGraphMergesOntologyNodesEdgesAndUntypedFallback(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	require.NoError(t, store.ReplaceIntelDocSections(ctx, "docs/effort.md", []codeanchor.IntelDocSection{{SectionID: "effort", Path: "docs/effort.md", Title: "Effort", Level: 1, Fingerprint: "effort-fp", UpdatedAt: 1}}, nil, nil))
	require.NoError(t, store.ReplaceIntelDocSections(ctx, "docs/spec.md", []codeanchor.IntelDocSection{{SectionID: "spec", Path: "docs/spec.md", Title: "Spec", Level: 1, Fingerprint: "spec-fp", UpdatedAt: 1}}, nil, nil))
	require.NoError(t, store.ReplaceIntelDocSections(ctx, "docs/untyped.md", []codeanchor.IntelDocSection{{SectionID: "untyped", Path: "docs/untyped.md", Title: "Untyped", Level: 1, Fingerprint: "untyped-fp", UpdatedAt: 1}}, nil, nil))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "docs/untyped.md", semdb.GraphDocEdgeKindWikilink, []string{"docs/spec.md"}))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "docs/effort.md", semdb.GraphDocEdgeKindWikilink, []string{"docs/spec.md"}))
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		NoteTypes: []semdb.OntologyNoteTypeRow{
			{NotePath: "docs/effort.md", TypeName: "EffortNote", UpdatedAt: 1},
			{NotePath: "docs/spec.md", TypeName: "ProcessSpec", UpdatedAt: 1},
		},
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "docs/effort.md", RelationName: "frozenSpecs", DstPath: "docs/spec.md", DstType: "ProcessSpec", Provenance: "section_neighbor", Structural: true, UpdatedAt: 2},
			{SrcPath: "docs/effort.md", RelationName: "frozenStories", DstPath: "docs/spec.md", DstNodeID: "story-a", DstType: "UserStory", Provenance: "story_id", Structural: true, UpdatedAt: 2},
		},
	}))
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{"docs/effort.md", "docs/spec.md"}, []codeanchor.IntelOntologyNode{
		{NodeID: "effort-note", NotePath: "docs/effort.md", NodeRefJSON: `{"notePath":"docs/effort.md","kind":"NOTE","typeName":"EffortNote"}`, NodeKind: "NOTE", TypeName: "EffortNote", SourceLocator: "docs/effort.md", DisplayLabel: "Effort", UpdatedAt: 2},
		{NodeID: "spec-note", NotePath: "docs/spec.md", NodeRefJSON: `{"notePath":"docs/spec.md","kind":"NOTE","typeName":"ProcessSpec"}`, NodeKind: "NOTE", TypeName: "ProcessSpec", SourceLocator: "docs/spec.md", DisplayLabel: "Spec", UpdatedAt: 2},
		{NodeID: "story-a", NotePath: "docs/spec.md", NodeRefJSON: `{"notePath":"docs/spec.md","fragment":"^story-a","nodeId":"story-a","kind":"EMBEDDED","typeName":"UserStory"}`, NodeKind: "EMBEDDED", TypeName: "UserStory", SourceLocator: "docs/spec.md#^story-a", DisplayLabel: "Story A", ParentNodeID: "spec-note", UpdatedAt: 2},
	}))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/a.go", []codeanchor.IntelAnchor{{
		AnchorID:    "a-fn",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/a.go",
		Symbol:      "A",
		FQN:         "pkg.A",
		Fingerprint: "fp-a",
	}}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/b.go", []codeanchor.IntelAnchor{{
		AnchorID:    "b-fn",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/b.go",
		Symbol:      "B",
		FQN:         "pkg.B",
		Fingerprint: "fp-b",
	}}, []codeanchor.IntelEdge{{
		SrcID: "b-fn",
		DstID: "a-fn",
		Kind:  "calls",
	}}, nil))

	runtime := &Runtime{}
	runtime.SetIntelStore(store)
	fixture := fixtureVault{
		root:     root,
		vault:    &obsidian.Vault{Name: "graph-fixture"},
		vaultDef: obsidian.VaultDefinition{Name: "graph-fixture", Path: root, Links: obsidian.LinkTypeBoth},
	}
	srv := newFixtureServer(t, fixture, runtime)

	graph, err := srv.buildGlobalGraph(ctx, 100, 2)
	require.NoError(t, err)

	requireGraphNode(t, graph, "embedded:story-a", "embedded")
	requireGraphEdge(t, graph, "note:docs/spec.md", "embedded:story-a", "embeds", "")
	requireGraphEdge(t, graph, "note:docs/effort.md", "note:docs/spec.md", "ontology", "frozenSpecs")
	requireGraphEdge(t, graph, "note:docs/effort.md", "embedded:story-a", "ontology", "frozenStories")
	requireGraphUndirectedEdge(t, graph, "note:docs/untyped.md", "note:docs/spec.md", "wikilink")
	requireGraphEdge(t, graph, "code:pkg/b.go", "code:pkg/a.go", "calls", "")
	requireGraphNode(t, graph, "code:pkg/a.go", "code")
	requireGraphNode(t, graph, "code:pkg/b.go", "code")
	for _, node := range graph.Nodes {
		require.NotContains(t, node.ID, "a-fn")
		require.NotContains(t, node.ID, "b-fn")
		require.NotContains(t, node.ID, "pkg.A")
		require.NotContains(t, node.ID, "pkg.B")
	}
	requireNoGraphEdge(t, graph, "note:docs/effort.md", "note:docs/spec.md", "wikilink")
}

func TestServerGlobalGraphDoesNotClipCodeEdgesByDefaultLimit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		calleeID := fmt.Sprintf("early-callee-%d", i)
		callerID := fmt.Sprintf("early-caller-%d", i)
		calleePath := fmt.Sprintf("pkg/early/%02d_dep.go", i)
		callerPath := fmt.Sprintf("pkg/early/%02d_call.go", i)
		require.NoError(t, store.ReplaceIntelCodeFile(ctx, calleePath, []codeanchor.IntelAnchor{{
			AnchorID:    calleeID,
			Lang:        codeanchor.LangGo,
			Kind:        "function",
			Path:        calleePath,
			Symbol:      "Dep",
			FQN:         fmt.Sprintf("early%d.Dep", i),
			Fingerprint: "fp-" + calleeID,
		}}, nil, nil))
		require.NoError(t, store.ReplaceIntelCodeFile(ctx, callerPath, []codeanchor.IntelAnchor{{
			AnchorID:    callerID,
			Lang:        codeanchor.LangGo,
			Kind:        "function",
			Path:        callerPath,
			Symbol:      "Call",
			FQN:         fmt.Sprintf("early%d.Call", i),
			Fingerprint: "fp-" + callerID,
		}}, []codeanchor.IntelEdge{{
			SrcID: callerID,
			DstID: calleeID,
			Kind:  "calls",
		}}, nil))
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "zz/late_dep.go", []codeanchor.IntelAnchor{{
		AnchorID:    "late-callee",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "zz/late_dep.go",
		Symbol:      "LateDep",
		FQN:         "late.Dep",
		Fingerprint: "fp-late-callee",
	}}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "zz/late_call.go", []codeanchor.IntelAnchor{{
		AnchorID:    "late-caller",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "zz/late_call.go",
		Symbol:      "LateCall",
		FQN:         "late.Call",
		Fingerprint: "fp-late-caller",
	}}, []codeanchor.IntelEdge{{
		SrcID: "late-caller",
		DstID: "late-callee",
		Kind:  "calls",
	}}, nil))

	runtime := &Runtime{}
	runtime.SetIntelStore(store)
	fixture := fixtureVault{
		root:     root,
		vault:    &obsidian.Vault{Name: "graph-fixture"},
		vaultDef: obsidian.VaultDefinition{Name: "graph-fixture", Path: root, Links: obsidian.LinkTypeBoth},
	}
	srv := newFixtureServer(t, fixture, runtime)

	graph, err := srv.buildGlobalGraph(ctx, 1, 2)
	require.NoError(t, err)
	requireGraphEdge(t, graph, "code:zz/late_call.go", "code:zz/late_dep.go", "calls", "")
}

func requireGraphNode(t *testing.T, graph GraphResponse, id, kind string) {
	t.Helper()
	for _, node := range graph.Nodes {
		if node.ID == id {
			require.Equal(t, kind, node.Kind)
			return
		}
	}
	require.Failf(t, "missing graph node", "id=%s", id)
}

func graphNodeByID(nodes []GraphNode, id string) GraphNode {
	for _, node := range nodes {
		if node.ID == id {
			return node
		}
	}
	return GraphNode{}
}

func requireGraphEdge(t *testing.T, graph GraphResponse, source, target, kind, relation string) {
	t.Helper()
	for _, edge := range graph.Edges {
		if edge.Source == source && edge.Target == target && edge.Kind == kind && (relation == "" || edge.RelationName == relation) {
			return
		}
	}
	require.Failf(t, "missing graph edge", "source=%s target=%s kind=%s relation=%s", source, target, kind, relation)
}

func requireNoGraphEdge(t *testing.T, graph GraphResponse, source, target, kind string) {
	t.Helper()
	for _, edge := range graph.Edges {
		require.Falsef(t, edge.Source == source && edge.Target == target && edge.Kind == kind, "unexpected graph edge %s -> %s kind=%s", source, target, kind)
	}
}

func requireGraphUndirectedEdge(t *testing.T, graph GraphResponse, a, b, kind string) {
	t.Helper()
	for _, edge := range graph.Edges {
		if edge.Kind != kind {
			continue
		}
		if (edge.Source == a && edge.Target == b) || (edge.Source == b && edge.Target == a) {
			return
		}
	}
	require.Failf(t, "missing graph edge", "a=%s b=%s kind=%s", a, b, kind)
}

type fixtureVault struct {
	root       string
	vault      *obsidian.Vault
	vaultDef   obsidian.VaultDefinition
	intelStore *semdb.Store
}

type fixtureFile struct {
	path string
	mode fs.FileMode
	data []byte
}

var webFixtureSnapshot struct {
	once  sync.Once
	files []fixtureFile
	err   error
}

func prepareWebFixtureVault(t *testing.T) fixtureVault {
	t.Helper()

	webFixtureSnapshot.once.Do(func() {
		root := filepath.Join(t.TempDir(), "vault-base")
		copyTree(t, filepath.Join(repoRoot(t), "testdata", "integration", "python-app", "vault"), root)
		cfgDir, cfg, err := obsidian.FindLocalConfig(root)
		if err != nil {
			webFixtureSnapshot.err = err
			return
		}
		vaultDef := obsidian.LocalConfigToDefinition(cfgDir, cfg)
		if err := indexing.RunUnifiedCore(context.Background(), indexing.UnifiedOptions{
			VaultPath: root, VaultDef: vaultDef, NoteMetadata: testNoteMetadataIndexer(t),
		}); err != nil {
			webFixtureSnapshot.err = err
			return
		}
		webFixtureSnapshot.err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			webFixtureSnapshot.files = append(webFixtureSnapshot.files, fixtureFile{path: rel, mode: info.Mode(), data: data})
			return nil
		})
	})
	require.NoError(t, webFixtureSnapshot.err)

	root := filepath.Join(t.TempDir(), "vault")
	for _, file := range webFixtureSnapshot.files {
		path := filepath.Join(root, file.path)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, file.data, file.mode))
	}

	cfgDir, cfg, err := obsidian.FindLocalConfig(root)
	require.NoError(t, err)
	vaultDef := obsidian.LocalConfigToDefinition(cfgDir, cfg)
	vault := &obsidian.Vault{Name: root}

	store, cleanup, err := obsidian.OpenIntelStore(root, true)
	require.NoError(t, err)
	t.Cleanup(func() {
		if cleanup != nil {
			cleanup()
		}
	})

	return fixtureVault{
		root:       root,
		vault:      vault,
		vaultDef:   vaultDef,
		intelStore: store,
	}
}

func TestPublicGraphQLNodePreservesUnanchoredItemIdentityAcrossSourceEdits(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		rewrite    func(string) string
		wantAnchor string
	}{
		{
			name: "distinct item inserted above",
			rewrite: func(content string) string {
				return strings.Replace(content, "- Follow [[plan]] while validating.", "- A distinct new criterion.\n- Follow [[plan]] while validating.", 1)
			},
		},
		{
			name: "durable anchor added on demand",
			rewrite: func(content string) string {
				return strings.Replace(content, "- Follow [[plan]] while validating.", "- Follow [[plan]] while validating. ^validation-follow", 1)
			},
			wantAnchor: "^validation-follow",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareOntologyFixtureVault(t)
			rows, err := fixture.intelStore.AllOntologyNodes(context.Background())
			require.NoError(t, err)
			var original ontology.NodeRef
			for _, row := range rows {
				if row.TypeName != "Criterion" || row.DisplayLabel != "Follow [[plan]] while validating." {
					continue
				}
				require.NoError(t, json.Unmarshal([]byte(row.NodeRefJSON), &original))
				break
			}
			require.NotEmpty(t, original.Structural)
			require.Contains(t, original.NodeID, "#item-")

			path := filepath.Join(fixture.root, "specs", "100-demo", "spec.md")
			content, err := os.ReadFile(path)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, []byte(testCase.rewrite(string(content))), 0o644))
			_, err = ontology.EnsureFreshRuntimeWithStore(context.Background(), testNoteMetadataIndexer(t), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
			require.NoError(t, err)

			srv := newFixtureServer(t, fixture, nil)
			httpSrv := httptest.NewServer(srv.Handler())
			defer httpSrv.Close()
			var response map[string]any
			postJSON(t, httpSrv.URL+"/api/v1/graphql", map[string]any{
				"query": `query NodeByRef($ref: String!) { node(ref: $ref) { title nodeId ref { fragment structural nodeId } } }`,
				"variables": map[string]any{
					"ref": original.NotePath + "#struct:" + original.Structural,
				},
			}, &response)
			require.NotContains(t, response, "errors")
			data := response["data"].(map[string]any)
			node := data["node"].(map[string]any)
			require.Equal(t, "Follow [[plan]] while validating.", node["title"])
			require.NotEqual(t, original.NodeID, node["nodeId"])
			ref := node["ref"].(map[string]any)
			if testCase.wantAnchor != "" {
				require.Equal(t, testCase.wantAnchor, ref["fragment"])
			}
		})
	}
}

func TestPublicGraphQLNodeRemainsAvailableWhileTheCompleteIndexGateIsClosed(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	planPath := filepath.Join(fixture.root, "specs", "100-demo", "plan.md")
	planContent, err := os.ReadFile(planPath)
	require.NoError(t, err)
	planContent = bytes.Replace(planContent, []byte("spec: specs/100-demo/spec.md\n"), []byte("spec: specs/100-demo/spec.md\ntags: [startup]\n"), 1)
	require.NoError(t, os.WriteFile(planPath, planContent, 0o644))
	_, err = testNoteMetadataIndexer(t).EnsureIndexed(context.Background(), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
	require.NoError(t, err)
	runtime := &Runtime{IntelStore: fixture.intelStore}
	runtime.EnableIndexGate()
	srv := newFixtureServer(t, fixture, runtime)

	coldBody := bytes.NewBufferString(`{"query":"query { node(ref: \"specs/100-demo/plan.md\") { path title } }"}`)
	coldReq := newApplicationRequest(http.MethodPost, "/api/v1/graphql", coldBody)
	coldReq.Header.Set("Content-Type", "application/json")
	coldCtx, coldCancel := context.WithTimeout(coldReq.Context(), 10*time.Millisecond)
	defer coldCancel()
	coldRes := httptest.NewRecorder()
	srv.Handler().ServeHTTP(coldRes, coldReq.WithContext(coldCtx))
	require.Equal(t, http.StatusServiceUnavailable, coldRes.Code)
	require.Contains(t, coldRes.Body.String(), PublicErrorIndexInitializing)

	_, err = fixture.intelStore.ApplyOwnershipTransitions(context.Background(), []semdb.OwnershipTransition{{
		Path:   "specs/100-demo/unrelated.md",
		Target: semdb.OwnershipTargetUnowned,
	}})
	require.NoError(t, err)
	require.False(t, runtime.IndexReady())

	body := bytes.NewBufferString(`{"query":"query { node: note(path: \"specs/100-demo/plan.md\") { path title frontmatter tags } }"}`)
	req := newApplicationRequest(http.MethodPost, "/api/v1/graphql", body)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		srv.Handler().ServeHTTP(res, req)
		close(done)
	}()
	select {
	case <-done:
		t.Fatalf("exact note request returned before note metadata became ready: status=%d body=%s", res.Code, res.Body.String())
	case <-time.After(20 * time.Millisecond):
	}
	runtime.MarkNoteReadReady()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("exact note request did not resume after note metadata became ready")
	}

	require.Equal(t, http.StatusOK, res.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
	require.NotContains(t, response, "errors")
	data := response["data"].(map[string]any)
	node := data["node"].(map[string]any)
	require.Equal(t, "specs/100-demo/plan.md", node["path"])
	require.Equal(t, "Demo plan summary", node["frontmatter"].(map[string]any)["summary"])
	require.Equal(t, []any{"startup"}, node["tags"])

	srv.cfg.NotePathOwned = func(string) bool { return true }
	missingBody := bytes.NewBufferString(`{"query":"query { node(ref: \"specs/100-demo/not-yet-published.md\") { path title } }"}`)
	missingReq := newApplicationRequest(http.MethodPost, "/api/v1/graphql", missingBody)
	missingReq.Header.Set("Content-Type", "application/json")
	missingCtx, missingCancel := context.WithTimeout(missingReq.Context(), 10*time.Millisecond)
	defer missingCancel()
	missingRes := httptest.NewRecorder()
	srv.Handler().ServeHTTP(missingRes, missingReq.WithContext(missingCtx))
	require.Equal(t, http.StatusServiceUnavailable, missingRes.Code)
	require.Contains(t, missingRes.Body.String(), PublicErrorIndexInitializing)

	broadBody := bytes.NewBufferString(`{"query":"query { notes(type: \"Plan\", first: 1) { nodes { path } } }"}`)
	broadReq := newApplicationRequest(http.MethodPost, "/api/v1/graphql", broadBody)
	broadReq.Header.Set("Content-Type", "application/json")
	broadCtx, broadCancel := context.WithTimeout(broadReq.Context(), 10*time.Millisecond)
	defer broadCancel()
	broadRes := httptest.NewRecorder()
	srv.Handler().ServeHTTP(broadRes, broadReq.WithContext(broadCtx))
	require.Equal(t, http.StatusServiceUnavailable, broadRes.Code)
	require.Contains(t, broadRes.Body.String(), PublicErrorIndexInitializing)

	nestedBody := bytes.NewBufferString(`{"query":"query { node(ref: \"specs/100-demo/plan.md\") { workspace { version } } }"}`)
	nestedReq := newApplicationRequest(http.MethodPost, "/api/v1/graphql", nestedBody)
	nestedReq.Header.Set("Content-Type", "application/json")
	nestedCtx, nestedCancel := context.WithTimeout(nestedReq.Context(), 10*time.Millisecond)
	defer nestedCancel()
	nestedRes := httptest.NewRecorder()
	srv.Handler().ServeHTTP(nestedRes, nestedReq.WithContext(nestedCtx))
	require.Equal(t, http.StatusServiceUnavailable, nestedRes.Code)
	require.Contains(t, nestedRes.Body.String(), PublicErrorIndexInitializing)

	fragmentBody := bytes.NewBufferString(`{"query":"query { node(ref: \"specs/100-demo/plan.md#implementation\") { path title } }"}`)
	fragmentReq := newApplicationRequest(http.MethodPost, "/api/v1/graphql", fragmentBody)
	fragmentReq.Header.Set("Content-Type", "application/json")
	fragmentCtx, fragmentCancel := context.WithTimeout(fragmentReq.Context(), 10*time.Millisecond)
	defer fragmentCancel()
	fragmentRes := httptest.NewRecorder()
	srv.Handler().ServeHTTP(fragmentRes, fragmentReq.WithContext(fragmentCtx))
	require.Equal(t, http.StatusServiceUnavailable, fragmentRes.Code)
	require.Contains(t, fragmentRes.Body.String(), PublicErrorIndexInitializing)

	typeBody := bytes.NewBufferString(`{"query":"query { node(ref: \"specs/100-demo/plan.md\") { __typename path } }"}`)
	typeReq := newApplicationRequest(http.MethodPost, "/api/v1/graphql", typeBody)
	typeReq.Header.Set("Content-Type", "application/json")
	typeCtx, typeCancel := context.WithTimeout(typeReq.Context(), 10*time.Millisecond)
	defer typeCancel()
	typeRes := httptest.NewRecorder()
	srv.Handler().ServeHTTP(typeRes, typeReq.WithContext(typeCtx))
	require.Equal(t, http.StatusServiceUnavailable, typeRes.Code)
	require.Contains(t, typeRes.Body.String(), PublicErrorIndexInitializing)

	graphReq := newApplicationRequest(http.MethodGet, "/api/v1/ontology/types", nil)
	graphCtx, cancel := context.WithTimeout(graphReq.Context(), 10*time.Millisecond)
	defer cancel()
	graphRes := httptest.NewRecorder()
	srv.Handler().ServeHTTP(graphRes, graphReq.WithContext(graphCtx))
	require.Equal(t, http.StatusServiceUnavailable, graphRes.Code)

	// A one-shot complete-index gate cannot prove that a newly selected path
	// has reached the shared metadata projection. Once it opens, an absent
	// source is a final miss, while a real source still waits for publication.
	runtime.MarkIndexReady()
	missingReadyBody := bytes.NewBufferString(`{"query":"query { node(ref: \"specs/100-demo/not-yet-published.md\") { path title } }"}`)
	missingReadyReq := newApplicationRequest(http.MethodPost, "/api/v1/graphql", missingReadyBody)
	missingReadyReq.Header.Set("Content-Type", "application/json")
	missingReadyRes := httptest.NewRecorder()
	srv.Handler().ServeHTTP(missingReadyRes, missingReadyReq)
	require.Equal(t, http.StatusOK, missingReadyRes.Code)
	var missingReadyResponse map[string]any
	require.NoError(t, json.Unmarshal(missingReadyRes.Body.Bytes(), &missingReadyResponse))
	require.NotContains(t, missingReadyResponse, "errors")
	require.Nil(t, missingReadyResponse["data"].(map[string]any)["node"])

	latePath := "specs/100-demo/late.md"
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, filepath.FromSlash(latePath)), []byte("# Late\n"), 0o644))
	lateBody := bytes.NewBufferString(`{"query":"query { node(ref: \"specs/100-demo/late.md\") { path title } }"}`)
	lateReq := newApplicationRequest(http.MethodPost, "/api/v1/graphql", lateBody)
	lateReq.Header.Set("Content-Type", "application/json")
	lateRes := httptest.NewRecorder()
	lateDone := make(chan struct{})
	go func() {
		srv.Handler().ServeHTTP(lateRes, lateReq)
		close(lateDone)
	}()
	select {
	case <-lateDone:
		t.Fatal("exact note request returned before its metadata row was published")
	case <-time.After(20 * time.Millisecond):
	}
	_, err = testNoteMetadataIndexer(t).EnsureIndexed(context.Background(), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
	require.NoError(t, err)
	select {
	case <-lateDone:
	case <-time.After(time.Second):
		t.Fatal("exact note request did not resume after its metadata row was published")
	}
	require.Equal(t, http.StatusOK, lateRes.Code)
	var lateResponse map[string]any
	require.NoError(t, json.Unmarshal(lateRes.Body.Bytes(), &lateResponse))
	require.NotContains(t, lateResponse, "errors")
	lateData := lateResponse["data"].(map[string]any)
	require.Equal(t, latePath, lateData["node"].(map[string]any)["path"])
}

func TestPublicGraphQLExactMetadataReadFailureIsRetryable(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	runtime := &Runtime{IntelStore: fixture.intelStore}
	runtime.EnableIndexGate()
	runtime.MarkNoteReadReady()
	srv := newFixtureServer(t, fixture, runtime)
	require.NoError(t, fixture.intelStore.Close())

	body := bytes.NewBufferString(`{"query":"query { node(ref: \"specs/100-demo/plan.md\") { path title } }"}`)
	req := newApplicationRequest(http.MethodPost, "/api/v1/graphql", body)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)

	require.Equal(t, http.StatusServiceUnavailable, res.Code)
	require.Equal(t, "5", res.Header().Get("Retry-After"))
	require.Contains(t, res.Body.String(), PublicErrorIndexInitializing)
}

func TestPublicGraphQLListsIdenticalUnanchoredItems(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	path := filepath.Join(fixture.root, "specs", "100-demo", "spec.md")
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	content = []byte(strings.Replace(string(content), "- Follow [[plan]] while validating.", "- Follow [[plan]] while validating.\n- Follow [[plan]] while validating.", 1))
	require.NoError(t, os.WriteFile(path, content, 0o644))
	_, err = ontology.EnsureFreshRuntimeWithStore(context.Background(), testNoteMetadataIndexer(t), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
	require.NoError(t, err)

	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	var response map[string]any
	postJSON(t, httpSrv.URL+"/api/v1/graphql", map[string]any{
		"query": `{ spec(path: "specs/100-demo/spec.md") { stories { stories { criteria { title summary } } } } }`,
	}, &response)
	require.NotContains(t, response, "errors")
	data := response["data"].(map[string]any)
	specs := data["spec"].([]any)
	require.Len(t, specs, 1)
	spec := specs[0].(map[string]any)
	storiesSection := spec["stories"].(map[string]any)
	stories := storiesSection["stories"].([]any)
	require.Len(t, stories, 1)
	criteria := stories[0].(map[string]any)["criteria"].([]any)
	require.Len(t, criteria, 3)
	require.Equal(t, criteria[1].(map[string]any)["title"], criteria[2].(map[string]any)["title"])
}

func prepareOntologyFixtureVault(t *testing.T) fixtureVault {
	t.Helper()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "specs", "100-demo"), 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type Spec
  @node(paths: ["specs/*/spec.md"], label: "Spec", keyField: "summary")
  @display(singular: "Specification", plural: "Specifications", group: "Delivery", parent: "Artifact") {
  summary: String!
  plans: [Plan!] @link(inverse: "spec")
  research: [Research!] @link(inverse: "spec")
  quickstarts: [Quickstart!] @link(inverse: "spec")
  requirements: Section @contains(level: H2, heading: "Requirements", required: true)
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}

type Story
  implements Section
  @node(locator: EMBEDDED, label: "Story", keyField: "status") {
  status: String @field
  criteria: [Criterion!] @contains(shape: LIST_ITEM)
}

type Criterion
  implements Section
  @node(locator: EMBEDDED, label: "Criterion") {
  summary: String! @field(sourceKind: ITEM_SUMMARY)
}

type StoriesSection implements Section {
  stories: [Story!] @contains(level: H3)
}

type Plan
  @node(paths: ["specs/*/plan.md"], label: "Plan", keyField: "summary") {
  summary: String!
  spec: Spec! @link(inverse: "plans")
  tasks: [Tasks!] @link(inverse: "plan")
  implementationPhases: Section @contains(level: H2, heading: "Implementation Phases", required: true)
  validation: Section @contains(level: H2, heading: "Validation", required: true)
}

type Research
  @node(paths: ["specs/*/research.md"], label: "Research", keyField: "summary") {
  summary: String!
  spec: Spec! @link(inverse: "research")
}

type Tasks
  @node(paths: ["specs/*/tasks.md"], label: "Tasks", keyField: "summary") {
  summary: String!
  plan: Plan! @link(inverse: "tasks")
}

type Quickstart
  @node(paths: ["specs/*/quickstart.md"], label: "Quickstart", keyField: "summary") {
  summary: String!
  spec: Spec! @link(inverse: "quickstarts")
}
`), 0o644))

	write := func(path, body string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(body), 0o644))
	}
	write("specs/100-demo/spec.md", `---
summary: Demo spec summary
---
# Demo Spec

## Requirements

- Must support ontology browsing.
- See [[plan]] for execution.
- ![[quickstart]]

## Stories

### Validation story
status:: READY
^validation

- Story tied to validation checks.
- Follow [[plan]] while validating.
`)
	write("specs/100-demo/plan.md", `---
summary: Demo plan summary
spec: specs/100-demo/spec.md
---
# Implementation Plan

## Implementation Phases

1. Build the browser.
2. Add compare panes.

## Validation

- Run unit tests.
- Verify structural links back to [[spec]].
- Confirm this effort covers [[spec#^validation]].
`)
	write("specs/100-demo/tasks.md", `---
summary: Demo tasks summary
plan: specs/100-demo/plan.md
---
# Tasks

- [ ] Ship ontology workspace.
`)
	write("specs/100-demo/research.md", `---
summary: Demo research summary
spec: specs/100-demo/spec.md
---
# Research

- Semantic note search supports note-only mode.
`)
	write("specs/100-demo/quickstart.md", `---
summary: Demo quickstart summary
spec: specs/100-demo/spec.md
---
# Quickstart

Open the ontology tab and compare notes.
`)

	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = store.Close()
	})

	vaultDef := obsidian.VaultDefinition{
		Name:  "ontology-fixture",
		Path:  root,
		Links: obsidian.LinkTypeBoth,
	}
	_, err = ontology.EnsureFreshRuntimeWithStore(context.Background(), testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)

	return fixtureVault{
		root:       root,
		vault:      &obsidian.Vault{Name: "ontology-fixture"},
		vaultDef:   vaultDef,
		intelStore: store,
	}
}

// prepareInterfaceFixtureVault builds a minimal vault whose schema defines a
// SpecLike interface with two NOTE-role implementors and one embedded-node
// implementor. Exercises the node-centric rollup: embedded implementors should
// surface with real counts/starting refs and interface queries should union
// both note-root and embedded instances through one list.
func prepareInterfaceFixtureVault(t *testing.T) fixtureVault {
	t.Helper()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "specs"), 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
interface SpecLike {
  summary: String!
}

type ProcessSpec implements SpecLike
  @node(paths: ["specs/proc.md"], label: "Process spec") {
  summary: String!
}

type ProductSpec implements SpecLike
  @node(paths: ["specs/prod.md"], label: "Product spec") {
  summary: String!
  metrics: MetricsSection @contains(level: H2, heading: "Metrics")
}

type MetricsSection implements Section {
  metrics: [SpecMetric!] @contains(level: H3)
}

type SpecMetric implements SpecLike & Section @node(locator: EMBEDDED, label: "Spec metric") {
  summary: String! @field
}
`), 0o644))

	write := func(path, body string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(body), 0o644))
	}
	write("specs/proc.md", `---
summary: Process rollup
---
# Process spec
`)
	write("specs/prod.md", `---
summary: Product rollup
---
# Product spec

## Metrics

### Spec metric
summary:: Embedded metric
`)

	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = store.Close()
	})

	vaultDef := obsidian.VaultDefinition{
		Name:  "interface-fixture",
		Path:  root,
		Links: obsidian.LinkTypeBoth,
	}
	_, err = ontology.EnsureFreshRuntimeWithStore(context.Background(), testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)

	return fixtureVault{
		root:       root,
		vault:      &obsidian.Vault{Name: "interface-fixture"},
		vaultDef:   vaultDef,
		intelStore: store,
	}
}

func newFixtureServer(t *testing.T, fixture fixtureVault, runtime *Runtime) *Server {
	t.Helper()

	if runtime != nil {
		runtime.SetIgnoreMatcher(obsidian.LoadVaultIgnoreMatcher(fixture.root, fixture.vaultDef.Excludes))
	}

	srv, err := NewServer(context.Background(), Config{
		Vault:        fixture.vault,
		VaultDef:     fixture.vaultDef,
		VaultPath:    fixture.root,
		Runtime:      runtime,
		NoteMetadata: testNoteMetadataIndexer(t),
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = srv.Close()
	})
	return srv
}

func requireNodeWorkspace(t *testing.T, srv *Server, target string, includes nodeWorkspaceIncludes) NodeWorkspaceResponse {
	t.Helper()
	ref, err := nodeRefFromRequest(url.Values{"ref": []string{target}})
	require.NoError(t, err)
	workspace, err := srv.nodeWorkspace(context.Background(), ref, includes)
	require.NoError(t, err)
	return workspace
}

func getJSON(t *testing.T, url string, out any) {
	t.Helper()

	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
}

func postJSON(t *testing.T, url string, body any, out any) {
	t.Helper()

	data, err := json.Marshal(body)
	require.NoError(t, err)

	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	require.NoError(t, err)
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(responseBody))
	require.NoError(t, json.Unmarshal(responseBody, out))
}

func viewIDs(entries []appviews.CatalogEntry) []string {
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	return ids
}

func getJSONStatus(t *testing.T, url string, wantStatus int, out any) {
	t.Helper()

	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, wantStatus, resp.StatusCode)
	require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
}

type nonFlushingResponseWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (w *nonFlushingResponseWriter) Header() http.Header {
	return w.header
}

func (w *nonFlushingResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(data)
}

func (w *nonFlushingResponseWriter) WriteHeader(status int) {
	w.status = status
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()

	err := filepath.WalkDir(src, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
	require.NoError(t, err)
}

func readSSEEvent(reader *bufio.Reader) (ontology.NodeEvent, bool) {
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return ontology.NodeEvent{}, false
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event ontology.NodeEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			return ontology.NodeEvent{}, false
		}
		return event, true
	}
}

func readGlobalSSEEvent(reader *bufio.Reader) (GlobalEvent, bool) {
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return GlobalEvent{}, false
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event GlobalEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			return GlobalEvent{}, false
		}
		return event, true
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)
	return root
}

func TestValidateEndpoint(t *testing.T) {
	t.Parallel()

	// Build a fixture vault with a broken link to exercise the broken_links check.
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))

	write := func(path, body string) {
		t.Helper()
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(body), 0o644))
	}

	// Schema with a simple type.
	write(".rhizome/ontology/schema.graphql", `
type Doc @node(paths: ["docs/*.md"], label: "Doc") {
  summary: String!
}
`)
	// A doc note with a broken link.
	write("docs/hello.md", `---
summary: Hello
---
# Hello

See [[Does Not Exist]] for details.
`)
	// A doc note with no issues.
	write("docs/clean.md", `---
summary: Clean doc
---
# Clean
`)

	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	vaultDef := obsidian.VaultDefinition{
		Name:  "validate-fixture",
		Path:  root,
		Links: obsidian.LinkTypeBoth,
	}
	// Publish one generation through the canonical coordinator path: live
	// projection refresh, RunLive, then sanitized snapshot publication.
	require.NoError(t, validationproduct.NewRefreshCoordinator(validationproduct.RefreshCoordinatorOptions{
		Store: store, VaultDef: vaultDef, NoteMetadata: testNoteMetadataIndexer(t),
	}).Refresh(context.Background()))

	fixture := fixtureVault{
		root:       root,
		vault:      &obsidian.Vault{Name: "validate-fixture"},
		vaultDef:   vaultDef,
		intelStore: store,
	}
	rt := &Runtime{IntelStore: store, ownsIntel: false}
	srv := newFixtureServer(t, fixture, rt)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	// GET /api/v2/validate should return issues.
	var result map[string]interface{}
	getJSON(t, httpSrv.URL+"/api/v2/validate", &result)

	require.Equal(t, "ok", result["status"])
	require.Equal(t, "current_issues", result["health"])
	payload := result["snapshot"].(map[string]interface{})
	issueCount := int(payload["issueCount"].(float64))
	require.Greater(t, issueCount, 0, "expected at least one issue (broken link)")

	checks := payload["checks"].([]interface{})
	require.NotEmpty(t, checks, "expected check results")

	// Find the broken_links check.
	var brokenLinksCheck map[string]interface{}
	for _, check := range checks {
		c := check.(map[string]interface{})
		if c["check"] == "broken_links" {
			brokenLinksCheck = c
			break
		}
	}
	require.NotNil(t, brokenLinksCheck, "expected broken_links check in results")
	require.Greater(t, int(brokenLinksCheck["issueCount"].(float64)), 0)

	// The published snapshot ran exactly the default suite, which excludes code anchors.
	var envelope ValidationEnvelope
	getJSON(t, httpSrv.URL+"/api/v2/validate", &envelope)
	require.NotNil(t, envelope.Snapshot)
	require.Equal(t, []string{
		validate.CheckOntology,
		validate.CheckIdentifiers,
		validate.CheckBrokenLinks,
	}, validate.DefaultChecks)
	require.Equal(t, validate.DefaultChecks, envelope.Snapshot.SelectedChecks)
	byName := make(map[string]semdb.ValidationCheckSnapshot, len(envelope.Snapshot.Checks))
	for _, check := range envelope.Snapshot.Checks {
		byName[check.Check] = check
	}
	for _, name := range validate.DefaultChecks {
		require.Containsf(t, byName, name, "missing check result for %s", name)
	}
	require.NotContains(t, byName, validate.CheckCodeAnchors)

	page, err := store.GetValidationDiagnosticsPage(context.Background(), semdb.ValidationDiagnosticPageRequest{
		Generation: int64(payload["generation"].(float64)), Limit: 200,
	})
	require.NoError(t, err)
	require.NotEmpty(t, page.Diagnostics)
	var brokenLink *semdb.ValidationDiagnostic
	for index := range page.Diagnostics {
		if page.Diagnostics[index].Code == "broken_note_link" {
			brokenLink = &page.Diagnostics[index]
			break
		}
	}
	require.NotNil(t, brokenLink)
	require.Contains(t, brokenLink.Target, "Does Not Exist")
}
