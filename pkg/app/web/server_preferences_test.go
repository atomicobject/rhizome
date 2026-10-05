package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appviews "github.com/atomicobject/rhizome/pkg/app/views"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
	"github.com/stretchr/testify/require"
)

func TestNewServerPreservesUnavailablePersonalDatabaseAndServesNotesAndViews(t *testing.T) {
	t.Parallel()
	fixture := prepareOntologyFixtureVault(t)
	databasePath := filepath.Join(fixture.root, ".rhizome", "user-state.sqlite")
	original := []byte("synthetic fixture: not a SQLite database")
	require.NoError(t, os.WriteFile(databasePath, original, 0o600))
	srv := newFixtureServer(t, fixture, nil)
	require.Nil(t, srv.userState)
	require.Error(t, srv.userStateErr)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	var note FileViewResponse
	getJSON(t, httpSrv.URL+"/api/v1/files/view?path=specs/100-demo/spec.md", &note)
	require.Equal(t, "note", note.Kind)
	require.NotEmpty(t, note.Content)
	var catalog appviews.Catalog
	getJSON(t, httpSrv.URL+"/api/v1/views", &catalog)
	require.NotEmpty(t, catalog.Views)
	scope := `{"viewId":"tasks","context":{"kind":"standalone"}}`
	for _, operation := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/view-preferences?scope=" + url.QueryEscape(scope), ""},
		{http.MethodPatch, "/api/v1/view-preferences", `{"scope":` + scope + `,"expectedRevision":0,"set":{"density":"compact"}}`},
		{http.MethodPost, "/api/v1/view-preferences/reset", `{"scope":` + scope + `,"expectedRevision":0}`},
		{http.MethodPost, "/api/v1/view-preferences/import", `{"scope":` + scope + `,"migrationId":"old","values":{}}`},
	} {
		request := newApplicationRequest(operation.method, operation.path, strings.NewReader(operation.body))
		recorder := httptest.NewRecorder()
		srv.Handler().ServeHTTP(recorder, request)
		require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
		var failure ErrorResponse
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &failure))
		require.Equal(t, "USER_STATE_UNAVAILABLE", failure.Code)
	}
	after, err := os.ReadFile(databasePath)
	require.NoError(t, err)
	require.Equal(t, original, after)
}

func TestUnavailablePersonalPreferenceDiagnosticsAreSanitizedAndActionable(t *testing.T) {
	t.Parallel()
	for _, item := range []struct {
		name       string
		diagnostic error
		message    string
	}{
		{"future", &migration.ErrFutureSchema{Domain: migration.DomainUserState, Current: 3, Supported: 2}, "need a newer Rhizome version"},
		{"drift", &migration.ErrSchemaDrift{Domain: migration.DomainUserState, Err: errors.New("synthetic-sensitive-diagnostic")}, "database schema is invalid"},
		{"other", errors.New("synthetic-sensitive-diagnostic"), "personal preferences are unavailable"},
	} {
		t.Run(item.name, func(t *testing.T) {
			srv := &Server{userStateErr: item.diagnostic}
			recorder := httptest.NewRecorder()
			srv.handleViewPreferences(recorder, newApplicationRequest(http.MethodGet, "/api/v1/view-preferences", nil))
			require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
			require.Contains(t, recorder.Body.String(), item.message)
			require.NotContains(t, recorder.Body.String(), "synthetic-sensitive-diagnostic")
			require.Same(t, item.diagnostic, srv.userStateErr)
		})
	}
}
