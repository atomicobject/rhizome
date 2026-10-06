package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/userstate"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

func preferencesTestServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	store, err := userstate.Open(context.Background(), root)
	require.NoError(t, err)
	s := &Server{cfg: Config{VaultPath: root}, userState: store, mux: http.NewServeMux(), globalEvents: newGlobalEventBroker(), cleanup: []func() error{store.Close}}
	s.registerRoutes()
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	return s
}

func preferencesRequest(t *testing.T, s *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	if raw, ok := body.(string); ok {
		data = []byte(raw)
	} else if body != nil {
		var err error
		data, err = json.Marshal(body)
		require.NoError(t, err)
	}
	r := newApplicationRequest(method, path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestViewPreferencesHTTPConflictResetAndDedicatedEvents(t *testing.T) {
	t.Parallel()
	s := preferencesTestServer(t)
	scope := userstate.Scope{ViewID: "$selection", Context: userstate.Context{Kind: viewconfig.MountKindType, Type: "MissingFromSchema"}}
	encoded, err := json.Marshal(scope)
	require.NoError(t, err)
	path := "/api/v1/view-preferences?scope=" + url.QueryEscape(string(encoded))
	w := preferencesRequest(t, s, http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var initial userstate.Snapshot
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &initial))
	require.Zero(t, initial.Revision)
	require.Empty(t, initial.Values)
	ch, unsubscribe := s.globalEvents.Subscribe()
	defer unsubscribe()
	w = preferencesRequest(t, s, http.MethodPatch, "/api/v1/view-preferences", map[string]any{"scope": scope, "expectedRevision": 0, "set": map[string]any{"selectedView": nil}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var patched userstate.Snapshot
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &patched))
	require.JSONEq(t, `null`, string(patched.Values["selectedView"]))
	event := <-ch
	require.Equal(t, "view_preferences.changed", event.Kind)
	data, err := json.Marshal(event.Data)
	require.NoError(t, err)
	require.JSONEq(t, fmt.Sprintf(`{"scope":%s,"revision":1,"vaultKey":%q}`, string(encoded), s.cfg.VaultPath), string(data))
	w = preferencesRequest(t, s, http.MethodPost, "/api/v1/view-preferences/reset", map[string]any{"scope": scope, "expectedRevision": 0})
	require.Equal(t, http.StatusConflict, w.Code)
	var conflict struct {
		Code    string `json:"code"`
		Details struct {
			Current userstate.Snapshot `json:"current"`
		} `json:"details"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &conflict))
	require.Equal(t, "CONFLICT", conflict.Code)
	require.Equal(t, patched, conflict.Details.Current)
	select {
	case event := <-ch:
		t.Fatalf("failed write published event: %+v", event)
	default:
	}
	w = preferencesRequest(t, s, http.MethodPost, "/api/v1/view-preferences/reset", map[string]any{"scope": scope, "expectedRevision": 1})
	require.Equal(t, http.StatusOK, w.Code)
	var reset userstate.Snapshot
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &reset))
	require.Empty(t, reset.Values)
	require.True(t, reset.MigrationClosed)
	require.EqualValues(t, 2, reset.Revision)
	require.Equal(t, "view_preferences.changed", (<-ch).Kind)
	w = preferencesRequest(t, s, http.MethodPost, "/api/v1/view-preferences/import", map[string]any{"scope": scope, "migrationId": "legacy-source", "values": map[string]any{"selectedView": "old-view"}})
	require.Equal(t, http.StatusOK, w.Code)
	var imported viewPreferencesImportResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &imported))
	require.False(t, imported.Imported)
	require.Equal(t, reset, imported.Snapshot)
}

func TestViewPreferencesHTTPRejectsInvalidInputAndCrossOriginMutations(t *testing.T) {
	t.Parallel()
	s := preferencesTestServer(t)
	validScope := `{"viewId":"tasks","context":{"kind":"standalone"}}`
	for _, body := range []string{
		`{"scope":` + validScope + `,"set":{"a":true}}`,
		`{"scope":` + validScope + `,"expectedRevision":null}`,
		`{"scope":` + validScope + `,"expectedRevision":0,"extra":true}`,
		`{"scope":` + validScope + `,"expectedRevision":0} {}`,
		`{"scope":` + validScope + `,"expectedRevision":0,"set":{"a":true},"unset":["a"]}`,
		`{"scope":{"viewId":"tasks","context":{"kind":"node","type":"*","ref":{"notePath":"note.md","kind":"NOTE"}}},"expectedRevision":0}`,
		`{"scope":` + validScope + `,"expectedRevision":0,"set":{"a":"` + strings.Repeat("x", userstate.MaxValueBytes) + `"}}`,
		`{"scope":` + validScope + `,"expectedRevision":0,"set":{"a":"` + strings.Repeat("x", maxViewPreferencesBodyBytes) + `"}}`,
	} {
		w := preferencesRequest(t, s, http.MethodPatch, "/api/v1/view-preferences", body)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	}
	for _, query := range []string{"", "?scope={}", "?scope=" + url.QueryEscape(validScope) + "&scope=" + url.QueryEscape(validScope)} {
		w := preferencesRequest(t, s, http.MethodGet, "/api/v1/view-preferences"+query, nil)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	}
	w := preferencesRequest(t, s, http.MethodPost, "/api/v1/view-preferences/import", `{"scope":`+validScope+`,"migrationId":"old"}`)
	require.Equal(t, http.StatusBadRequest, w.Code)
	w = preferencesRequest(t, s, http.MethodPut, "/api/v1/view-preferences", nil)
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
	r := newApplicationRequest(http.MethodPatch, "/api/v1/view-preferences", strings.NewReader(`{"scope":`+validScope+`,"expectedRevision":0,"set":{"a":true}}`))
	r.Header.Set("Origin", "https://other.example")
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	require.Equal(t, http.StatusForbidden, w.Code)
	current, err := s.userState.Read(context.Background(), userstate.Scope{ViewID: "tasks", Context: userstate.Context{Kind: viewconfig.MountKindStandalone}})
	require.NoError(t, err)
	require.Zero(t, current.Revision)
	require.Empty(t, current.Values)
}

func TestViewPreferencesCustomInvocationCarriesVaultAndLocalSlot(t *testing.T) {
	t.Parallel()
	s := preferencesTestServer(t)
	def := viewconfig.ViewDefinition{ID: "custom", Mount: viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone}}
	r := httptest.NewRequest(http.MethodGet, "/views/custom?preferenceSlot=detail", nil)
	invocation, err := s.customViewInvocationForRequest(r, def)
	require.NoError(t, err)
	require.Equal(t, s.cfg.VaultPath, invocation.VaultKey)
	require.Equal(t, "detail", invocation.PreferenceSlot)
	r = httptest.NewRequest(http.MethodGet, "/views/custom?preferenceSlot="+strings.Repeat("s", 257), nil)
	_, err = s.customViewInvocationForRequest(r, def)
	require.Error(t, err)
}

func TestViewPreferencesHostResetIncludesSlotsAndPublishesFamilyEvent(t *testing.T) {
	t.Parallel()
	s := preferencesTestServer(t)
	host := userstate.Scope{ViewID: "custom", Context: userstate.Context{Kind: viewconfig.MountKindStandalone}, Slot: "detail"}
	widget := host
	widget.WidgetSlot = "table"
	_, err := s.userState.Patch(context.Background(), widget, 0, map[string]json.RawMessage{"columns": json.RawMessage(`[]`)}, nil)
	require.NoError(t, err)
	ch, unsubscribe := s.globalEvents.Subscribe()
	defer unsubscribe()
	w := preferencesRequest(t, s, http.MethodPost, "/api/v1/view-preferences/reset", map[string]any{"scope": host, "expectedRevision": 0, "includeSlots": true})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	data, err := json.Marshal((<-ch).Data)
	require.NoError(t, err)
	var event struct {
		IncludeSlots bool            `json:"includeSlots"`
		Scope        userstate.Scope `json:"scope"`
	}
	require.NoError(t, json.Unmarshal(data, &event))
	require.True(t, event.IncludeSlots)
	require.Equal(t, host, event.Scope)
	read, err := s.userState.Read(context.Background(), widget)
	require.NoError(t, err)
	require.Empty(t, read.Values)
	require.EqualValues(t, 2, read.Revision)
}

func TestWorkspaceViewPreferencesPersistAndRejectSubjects(t *testing.T) {
	s := preferencesTestServer(t)
	scope := userstate.Scope{ViewID: "workspace.overview", Context: userstate.Context{Kind: viewconfig.MountKindWorkspace}}
	w := preferencesRequest(t, s, http.MethodPatch, "/api/v1/view-preferences", map[string]any{"scope": scope, "expectedRevision": 0, "set": map[string]any{"matrix": true}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	encoded, err := json.Marshal(scope)
	require.NoError(t, err)
	w = preferencesRequest(t, s, http.MethodGet, "/api/v1/view-preferences?scope="+url.QueryEscape(string(encoded)), nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"matrix":true`)
	scope.Context.Group = "Delivery"
	w = preferencesRequest(t, s, http.MethodPatch, "/api/v1/view-preferences", map[string]any{"scope": scope, "expectedRevision": 0, "set": map[string]any{"matrix": true}})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}
