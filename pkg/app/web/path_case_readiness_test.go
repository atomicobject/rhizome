package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExactQueryReadinessAcceptsFilesystemCaseAlias(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	original, err := os.Stat(filepath.Join(fixture.root, "specs"))
	require.NoError(t, err)
	alias, err := os.Stat(filepath.Join(fixture.root, "SPECS"))
	if err != nil || !os.SameFile(original, alias) {
		t.Skip("requires a filesystem case alias")
	}
	runtime := &Runtime{IntelStore: fixture.intelStore}
	runtime.EnableIndexGate()
	runtime.MarkNoteReadReady()
	runtime.MarkIndexReady()
	srv := newFixtureServer(t, fixture, runtime)
	for _, selection := range []string{"path title", "__typename path"} {
		body, err := json.Marshal(map[string]string{"query": `{ note(path: "SPECS/100-demo/plan.md") { ` + selection + ` } }`})
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		req := newApplicationRequest(http.MethodPost, "/api/v1/graphql", bytes.NewReader(body)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		srv.Handler().ServeHTTP(res, req)
		cancel()
		require.Equal(t, http.StatusOK, res.Code, res.Body.String())
		var result struct {
			Data   struct{ Note struct{ Path string } }
			Errors []any
		}
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &result))
		require.Empty(t, result.Errors)
		require.Equal(t, "specs/100-demo/plan.md", result.Data.Note.Path)
	}
}
