package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	appviews "github.com/atomicobject/rhizome/pkg/app/views"
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
