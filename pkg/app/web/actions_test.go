package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/app/actions"
)

func newActionsTestServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "modules", "demo", "actions")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	write := func(name, body string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}
	write("echo.yaml", "description: Echo the input\ninputs:\n  reason: {kind: string, required: true, enum: [wrong]}\nrun:\n  command: [cat]\n")
	write("refuse.yaml", "description: Always refuse\nrun:\n  command: [sh, -c, \"echo busy >&2; exit 2\"]\n")
	s := &Server{
		cfg: Config{VaultPath: root, Actions: &actions.Runner{RepoRoot: root, Trust: func(context.Context) string { return "" }}},
		mux: http.NewServeMux(),
	}
	s.registerRoutes()
	return s
}

func serveAction(s *Server, method, target, body string, header map[string]string) *httptest.ResponseRecorder {
	req := newApplicationRequest(method, target, strings.NewReader(body))
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestActionsRoutes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh and cat")
	}
	s := newActionsTestServer(t)

	rec := serveAction(s, http.MethodGet, "/api/v1/actions", "", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `[
		{"id":"demo/echo","module":"demo","name":"echo","description":"Echo the input","inputs":{"reason":{"kind":"string","required":true,"enum":["wrong"]}}},
		{"id":"demo/refuse","module":"demo","name":"refuse","description":"Always refuse","inputs":{}}
	]`, rec.Body.String())

	for _, tc := range []struct {
		name, target, body string
		code               int
		envelope           string
	}{
		{"ok", "/api/v1/actions/demo/echo", `{"reason":"wrong"}`, http.StatusOK, `{"status":"ok","result":{"reason":"wrong"}}`},
		{"refused is still 200", "/api/v1/actions/demo/refuse", ``, http.StatusOK, `{"status":"refused","reason":"busy"}`},
		{"invalid input", "/api/v1/actions/demo/echo", `{"reason":"other"}`, http.StatusBadRequest, ""},
		{"body not an object", "/api/v1/actions/demo/echo", `["wrong"]`, http.StatusBadRequest, ""},
		{"unknown action", "/api/v1/actions/demo/missing", `{}`, http.StatusNotFound, ""},
		{"unknown module", "/api/v1/actions/nope/echo", `{}`, http.StatusNotFound, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveAction(s, http.MethodPost, tc.target, tc.body, map[string]string{"Content-Type": "application/json"})
			require.Equal(t, tc.code, rec.Code, rec.Body.String())
			if tc.envelope != "" {
				require.JSONEq(t, tc.envelope, rec.Body.String())
			} else {
				var payload map[string]any
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
				require.NotContains(t, payload, "status")
			}
		})
	}

	rec = serveAction(s, http.MethodGet, "/api/v1/actions/demo/echo", "", nil)
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	rec = serveAction(s, http.MethodPost, "/api/v1/actions/demo/echo", `{"reason":"wrong"}`, map[string]string{"Origin": "https://evil.example"})
	require.Equal(t, http.StatusForbidden, rec.Code)
}
