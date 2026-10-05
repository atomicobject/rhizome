package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func getOntologyTypeJSON(t *testing.T, srv *Server, target string) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.handleOntologyTypeByName(rec, httptest.NewRequest(http.MethodGet, target, nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

func TestOntologyTypeNotesNoneReturnsSchemaWithoutNotes(t *testing.T) {
	t.Parallel()
	fixture := prepareInterfaceFixtureVault(t)
	srv := newFixtureServer(t, fixture, &Runtime{IntelStore: fixture.intelStore})

	full := getOntologyTypeJSON(t, srv, "/api/v1/ontology/types/ProductSpec")
	require.NotEmpty(t, full["notes"])

	schemaOnly := getOntologyTypeJSON(t, srv, "/api/v1/ontology/types/ProductSpec?notes=none")
	require.NotContains(t, schemaOnly, "notes")
	require.NotContains(t, schemaOnly, "authoringGuide")
	require.Equal(t, full["count"], schemaOnly["count"])
	require.Equal(t, full["issueCount"], schemaOnly["issueCount"])

	typeDoc := schemaOnly["type"].(map[string]any)
	require.Equal(t, "ProductSpec", typeDoc["name"])
	require.Equal(t, "summary", typeDoc["summaryField"])
	for _, raw := range typeDoc["fields"].([]any) {
		field := raw.(map[string]any)
		if field["name"] == "summary" {
			require.Equal(t, map[string]any{"role": "SUMMARY", "importance": "NORMAL"}, field["display"])
		}
	}
}
