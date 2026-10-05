package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	appviews "github.com/atomicobject/rhizome/pkg/app/views"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

func TestConfiguredViewsGenerationChangesWhenCatalogContentChanges(t *testing.T) {
	catalog := appviews.Catalog{Views: []appviews.CatalogEntry{{
		ID:   "specs",
		Name: "Specs",
		Defaults: viewconfig.DefaultsSpec{
			Sort: []viewconfig.SortSpec{{Field: "title", Direction: "asc"}},
		},
	}}}

	first := configuredViewsGeneration(catalog)
	catalog.Views[0].Defaults.Sort[0].Direction = "desc"
	second := configuredViewsGeneration(catalog)

	require.NotEmpty(t, first)
	require.NotEqual(t, first, second)
}

func TestWritePublicViewExecutionErrorMapsUnknownErrorsToInternal(t *testing.T) {
	rec := httptest.NewRecorder()

	writePublicViewExecutionError(rec, errors.New("source backend failed"))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	var resp ErrorResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Equal(t, PublicErrorInternal, resp.Code)
}

func TestWritePublicViewReadSetupErrorPreservesInternalServiceFailures(t *testing.T) {
	rec := httptest.NewRecorder()

	writePublicViewReadSetupError(rec, errors.New("view service failed"))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	var resp ErrorResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Equal(t, PublicErrorInternal, resp.Code)
}

func TestPublicViewSaveCreatesThenConflictsAndRefuses(t *testing.T) {
	t.Parallel()
	fixture := prepareOntologyFixtureVault(t)
	httpSrv := httptest.NewServer(newFixtureServer(t, fixture, nil).Handler())
	defer httpSrv.Close()
	generatedID := viewconfig.GeneratedTypeID("Plan")
	var loaded appviews.ExecuteResponse
	postJSON(t, httpSrv.URL+"/api/v1/views/"+generatedID+"/execute", appviews.ExecuteRequest{}, &loaded)

	var saved appviews.SaveResponse
	postJSON(t, httpSrv.URL+"/api/v1/views/"+generatedID+"/save", appviews.SaveRequest{
		DefinitionFingerprint: loaded.DefinitionFingerprint,
		State:                 appviews.SaveState{Variant: "table", Sort: []viewconfig.SortSpec{{Field: "title", Direction: "desc"}}},
	}, &saved)
	require.Equal(t, appviews.SaveResponse{ID: "plan", Path: ".rhizome/views/plan.yaml", Created: true}, saved)
	var catalog appviews.Catalog
	getJSON(t, httpSrv.URL+"/api/v1/views", &catalog)
	require.Contains(t, viewIDs(catalog.Views), "plan")

	var conflict ErrorResponse
	postJSONWithStatus(t, httpSrv.URL+"/api/v1/views/plan/save", appviews.SaveRequest{DefinitionFingerprint: loaded.DefinitionFingerprint, State: appviews.SaveState{Variant: "table"}}, http.StatusConflict, &conflict)
	require.Equal(t, PublicErrorConflict, conflict.Code)
	var invalid ErrorResponse
	postJSONWithStatus(t, httpSrv.URL+"/api/v1/views/plan/save", appviews.SaveRequest{State: appviews.SaveState{Filters: []viewconfig.FilterSpec{{Field: "title"}}}}, http.StatusBadRequest, &invalid)
	var missing ErrorResponse
	postJSONWithStatus(t, httpSrv.URL+"/api/v1/views/nope/save", appviews.SaveRequest{}, http.StatusNotFound, &missing)

	request, err := http.NewRequest(http.MethodPost, httpSrv.URL+"/api/v1/views/plan/save", nil)
	require.NoError(t, err)
	request.Header.Set("Origin", "https://elsewhere.example")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusForbidden, response.StatusCode, "cross-origin browser writes are refused")
	getResp, err := http.Get(httpSrv.URL + "/api/v1/views/plan/save")
	require.NoError(t, err)
	require.NoError(t, getResp.Body.Close())
	require.Equal(t, http.StatusMethodNotAllowed, getResp.StatusCode)
}
