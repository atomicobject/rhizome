package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	appviews "github.com/atomicobject/rhizome/pkg/app/views"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/identity"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func executeView(t *testing.T, srv *Server, viewID string, payload any) (string, appviews.ExecuteResponse) {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	req := newApplicationRequest(http.MethodPost, "/api/v1/views/"+viewID+"/execute", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var resp appviews.ExecuteResponse
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &resp))
	generation := res.Header().Get(GenerationHeader)
	require.NotEmpty(t, generation)
	return generation, resp
}

func TestViewExecutionIsCachedPerGenerationAndRereadAfterADefinitionChange(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	writeView := func(sort string) {
		dir := filepath.Join(fixture.root, ".rhizome", "views")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "plans.yaml"), []byte("apiVersion: rhizome.view.v1\nid: plans\nname: Plans\nsource:\n  kind: ontology_type\n  type: Plan\nmount:\n  kind: standalone\ndefaults:\n  sort:\n    - field: title\n      direction: "+sort+"\nvariants:\n  table:\n    columns:\n      - field: title\n"), 0o644))
	}
	writeView("asc")
	hash := readQueryHash("view", "plans")

	first, firstResp := executeView(t, srv, "plans", appviews.ExecuteRequest{})
	second, _ := executeView(t, srv, "plans", appviews.ExecuteRequest{})
	require.Equal(t, first, second)
	require.EqualValues(t, 1, srv.reads.Metrics().Queries[hash].Executions)
	require.EqualValues(t, 1, srv.reads.Metrics().Queries[hash].Hits)

	writeView("desc")
	third, thirdResp := executeView(t, srv, "plans", appviews.ExecuteRequest{})
	require.NotEqual(t, first, third, "a view definition change moves the generation")
	require.NotEqual(t, firstResp.DefinitionFingerprint, thirdResp.DefinitionFingerprint)
	require.EqualValues(t, 2, srv.reads.Metrics().Queries[hash].Executions)

	// A different request is a different entry.
	executeView(t, srv, "plans", appviews.ExecuteRequest{Search: "demo"})
	require.EqualValues(t, 3, srv.reads.Metrics().Queries[hash].Executions)
}

func TestViewExecutionWithAnEditSessionBypassesTheCache(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	payload := map[string]any{"editSession": map[string]any{"id": "session-1"}}
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	for range 2 {
		req := newApplicationRequest(http.MethodPost, "/api/v1/views/plans/execute", bytes.NewReader(body))
		res := httptest.NewRecorder()
		srv.Handler().ServeHTTP(res, req)
		require.NotEmpty(t, res.Header().Get(GenerationHeader))
	}
	stats := srv.reads.Metrics()
	require.EqualValues(t, 2, stats.Queries[readQueryHash("view", "plans")].Bypasses)
	require.Zero(t, stats.Entries)
}

func TestViewCatalogCarriesTheGenerationHeader(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, newApplicationRequest(http.MethodGet, "/api/v1/views", nil))
	require.Equal(t, http.StatusOK, res.Code)
	require.Equal(t, srv.IndexGeneration(t.Context()), res.Header().Get(GenerationHeader))
}

// A view whose statistics count stale rows depends on the clock, which no
// generation component covers, so it is executed every time (SPEC-0136 R4).
func TestViewExecutionThatReadsTheClockIsNotStored(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	schemaPath := filepath.Join(fixture.root, ".rhizome", "ontology", "schema.graphql")
	schema, err := os.ReadFile(schemaPath)
	require.NoError(t, err)
	withLifecycle := strings.Replace(string(schema), "type Plan\n  @node(paths: [\"specs/*/plan.md\"], label: \"Plan\", keyField: \"summary\") {\n", "type Plan\n  @node(paths: [\"specs/*/plan.md\"], label: \"Plan\", keyField: \"summary\") {\n  status: PlanStatus @field @display(importance: KEY)\n", 1)
	require.NotEqual(t, string(schema), withLifecycle)
	withLifecycle += "\nenum PlanStatus {\n  drafting @view(stage: \"open\")\n  building @view(stage: \"active\")\n}\n"
	require.NoError(t, os.WriteFile(schemaPath, []byte(withLifecycle), 0o644))
	srv := newFixtureServer(t, fixture, nil)
	dir := filepath.Join(fixture.root, ".rhizome", "views")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plans.yaml"), []byte("apiVersion: rhizome.view.v1\nid: plans\nname: Plans\nsource:\n  kind: ontology_type\n  type: Plan\nmount:\n  kind: standalone\nvariants:\n  table:\n    columns:\n      - field: title\n"), 0o644))

	first, resp := executeView(t, srv, "plans", appviews.ExecuteRequest{})
	require.NotNil(t, resp.Stats, "a type view reports statistics")
	second, _ := executeView(t, srv, "plans", appviews.ExecuteRequest{})
	require.Equal(t, first, second)
	stats := srv.reads.Metrics()
	require.EqualValues(t, 2, stats.Queries[readQueryHash("view", "plans")].Executions)
	require.Zero(t, stats.Queries[readQueryHash("view", "plans")].Hits)
	require.Zero(t, stats.Entries)
}

// A preset that binds the current user answers for whoever is configured, which
// no generation component covers, so it is executed every time (SPEC-0136 R4).
func TestViewExecutionBoundToTheCurrentUserIsNotStored(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	schemaPath := filepath.Join(fixture.root, ".rhizome", "ontology", "schema.graphql")
	schema, err := os.ReadFile(schemaPath)
	require.NoError(t, err)
	withOwner := strings.Replace(string(schema), "  spec: Spec! @link(inverse: \"plans\")\n", "  spec: Spec! @link(inverse: \"plans\")\n  owner: Person @link\n", 1)
	require.NotEqual(t, string(schema), withOwner)
	withOwner += "\ntype Person @node(paths: [\"people/*.md\"], label: \"Person\") {\n  name: String\n}\n"
	require.NoError(t, os.WriteFile(schemaPath, []byte(withOwner), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(fixture.root, "people"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, "people", "ada.md"), []byte("---\nname: Ada\n---\n# Ada\n"), 0o644))
	planPath := filepath.Join(fixture.root, "specs", "100-demo", "plan.md")
	plan, err := os.ReadFile(planPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(planPath, []byte(strings.Replace(string(plan), "spec: specs/100-demo/spec.md\n", "spec: specs/100-demo/spec.md\nowner: \"[[people/ada]]\"\n", 1)), 0o644))
	_, err = ontologytest.Publish(vaultconfig.WithEnv(t.Context(), fixture.vault.Env), testkit.NoteMetadataIndexer(t), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
	require.NoError(t, err)
	srv := newFixtureServer(t, fixture, nil)
	dir := filepath.Join(fixture.root, ".rhizome", "views")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plans.yaml"), []byte("apiVersion: rhizome.view.v1\nid: plans\nname: Plans\nsource:\n  kind: ontology_type\n  type: Plan\nmount:\n  kind: standalone\nfilterPresets:\n  - id: mine\n    label: Mine\n    filters:\n      - field: owner\n        op: eq\n        valueFrom: ontology.currentUser.resolvedRef\nvariants:\n  table:\n    columns:\n      - field: title\n"), 0o644))
	mine := appviews.ExecuteRequest{FilterPreset: "mine"}

	before, unset := executeView(t, srv, "plans", mine)
	require.Empty(t, unset.Rows, "no current user matches no plan")
	_, err = identity.WriteCurrentUser(fixture.root, "people/ada.md")
	require.NoError(t, err)
	after, set := executeView(t, srv, "plans", mine)
	require.Equal(t, before, after, "configuring the user moves no generation component")
	require.Len(t, set.Rows, 1, "the configured user's plan shows at the same generation")
	require.Zero(t, srv.reads.Metrics().Entries)
}

func TestCachedViewFollowsOwnershipWithoutIndexWrites(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	dir := filepath.Join(fixture.root, ".rhizome", "views")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plans.yaml"), []byte("apiVersion: rhizome.view.v1\nid: plans\nname: Plans\nsource:\n  kind: ontology_type\n  type: Plan\nmount:\n  kind: standalone\nvariants:\n  table:\n    columns:\n      - field: title\n"), 0o644))
	srv := newFixtureServer(t, fixture, nil)
	before, response := executeView(t, srv, "plans", appviews.ExecuteRequest{})
	require.NotEmpty(t, response.Rows)
	for _, row := range response.Rows {
		require.Empty(t, row.ReadOnlyReason)
	}
	writes, _ := srv.writesGeneration(t.Context())
	path := filepath.Join(fixture.root, ".rhizome", "source-ownership.yml")
	require.NoError(t, os.WriteFile(path, []byte("sources:\n  - owner: Synthetic tool\n    reason: Managed externally.\n    paths: [\"**\"]\n"), 0o644))
	owned, response := executeView(t, srv, "plans", appviews.ExecuteRequest{})
	require.NotEqual(t, before, owned)
	for _, row := range response.Rows {
		require.Contains(t, row.ReadOnlyReason, "Managed externally.")
	}
	executeView(t, srv, "plans", appviews.ExecuteRequest{})
	require.NoError(t, os.Remove(path))
	_, response = executeView(t, srv, "plans", appviews.ExecuteRequest{})
	for _, row := range response.Rows {
		require.Empty(t, row.ReadOnlyReason)
	}
	afterWrites, _ := srv.writesGeneration(t.Context())
	require.Equal(t, writes, afterWrites)
	require.GreaterOrEqual(t, srv.reads.Metrics().Queries[readQueryHash("view", "plans")].Hits, uint64(1))
}
