package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/stretchr/testify/require"
)

const validClientBatch = `{
  "session": "0123456789abcdef0123456789abcdef",
  "build": "DTQf8ri4",
  "surface": "desktop",
  "route": "notes",
  "window_start": 1761400000000,
  "window_end": 1761400010000,
  "regions": {"sidebar": {"mount": 1, "update": 4, "nested_update": 0, "actual_total_ms": 12.5,
    "actual_max_ms": 6.25, "base_total_ms": 20, "base_max_ms": 8, "commits_after_refresh": 3}},
  "frames": {"over_50ms": 2, "over_200ms": 1, "stalled_ms": 310.5, "longest_ms": 240},
  "refresh": {"cycles": 1, "refetched": 5, "unchanged": 4, "churned": 1},
  "stability": {"reconnects": 1, "errors": {"TypeError": 2}, "rejections": {}}
}`

func clientDiagnosticsServer(t *testing.T, recorder *diagnostics.Recorder) *Server {
	t.Helper()
	s := &Server{diagnostics: recorder, mux: http.NewServeMux()}
	s.registerRoutes()
	return s
}

func postClientBatch(s *Server, body, origin string) int {
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/diagnostics/client", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	return response.Code
}

func clientEvents(t *testing.T, root string, recorder *diagnostics.Recorder) []diagnostics.Event {
	t.Helper()
	require.NoError(t, recorder.Close())
	result, err := diagnostics.ReadEvents(root, diagnostics.Filter{})
	require.NoError(t, err)
	var events []diagnostics.Event
	for _, event := range result.Events {
		if event.Name == "web.client" {
			events = append(events, event)
		}
	}
	return events
}

func TestClientDiagnosticsRecordsOneEventPerBatch(t *testing.T) {
	root := t.TempDir()
	recorder, err := diagnostics.Open(root, diagnostics.Options{})
	require.NoError(t, err)
	s := clientDiagnosticsServer(t, recorder)

	require.Equal(t, http.StatusNoContent, postClientBatch(s, validClientBatch, "http://127.0.0.1"))

	events := clientEvents(t, root, recorder)
	require.Len(t, events, 1)
	event := events[0]
	require.Equal(t, "web", event.Subsystem)
	require.Equal(t, "desktop", event.Attributes["surface"])
	require.Equal(t, "notes", event.Attributes["route"])
	require.Equal(t, "DTQf8ri4", event.Attributes["build"])
	require.Equal(t, "0123456789abcdef0123456789abcdef", event.Attributes["session"])
	require.Equal(t, "2025-10-25T13:46:40Z", event.Attributes["window_start"])
	require.Equal(t, float64(10_000), event.Attributes["window_ms"])
	var metrics struct {
		Regions map[string]clientRegion `json:"regions"`
		Frames  clientFrames            `json:"frames"`
		Refresh clientRefresh           `json:"refresh"`
		Stab    clientStability         `json:"stability"`
	}
	require.NoError(t, json.Unmarshal(event.Metrics, &metrics))
	require.Equal(t, uint64(4), metrics.Regions["sidebar"].Update)
	require.Equal(t, uint64(3), metrics.Regions["sidebar"].CommitsAfterRefresh)
	require.Equal(t, 240.0, metrics.Frames.LongestMs)
	require.Equal(t, uint64(4), metrics.Refresh.Unchanged)
	require.Equal(t, uint64(1), metrics.Refresh.Churned)
	require.Equal(t, uint64(2), metrics.Stab.Errors["TypeError"])
	require.False(t, event.Truncated)
}

func TestClientDiagnosticsRejectsInvalidBatchesWithoutRecording(t *testing.T) {
	root := t.TempDir()
	recorder, err := diagnostics.Open(root, diagnostics.Options{})
	require.NoError(t, err)
	s := clientDiagnosticsServer(t, recorder)
	replace := func(old, replacement string) string {
		require.Contains(t, validClientBatch, old)
		return strings.Replace(validClientBatch, old, replacement, 1)
	}

	cases := []struct {
		name, body, origin string
		status             int
	}{
		{"oversized", replace(`"stability"`, `"padding": "`+strings.Repeat("x", maxClientDiagnosticsBodyBytes)+`", "stability"`), "", http.StatusBadRequest},
		{"unknown field", replace(`"route": "notes",`, `"route": "notes", "title": "PRIVATE_TITLE",`), "", http.StatusBadRequest},
		{"unknown nested field", replace(`"cycles": 1,`, `"cycles": 1, "query": "PRIVATE_QUERY",`), "", http.StatusBadRequest},
		{"session pattern", replace(`0123456789abcdef0123456789abcdef`, `../PRIVATE_PATH`), "", http.StatusBadRequest},
		{"region pattern", replace(`"sidebar"`, `"notes/PRIVATE.md"`), "", http.StatusBadRequest},
		{"route kind", replace(`"route": "notes"`, `"route": "/notes/PRIVATE.md"`), "", http.StatusBadRequest},
		{"error name", replace(`"TypeError"`, `"PRIVATE message: failed"`), "", http.StatusBadRequest},
		{"negative count", replace(`"reconnects": 1`, `"reconnects": -1`), "", http.StatusBadRequest},
		{"trailing value", validClientBatch + `{}`, "", http.StatusBadRequest},
		{"cross origin", validClientBatch, "http://evil.example", http.StatusForbidden},
	}
	for _, tc := range cases {
		require.Equal(t, tc.status, postClientBatch(s, tc.body, tc.origin), tc.name)
	}

	require.Empty(t, clientEvents(t, root, recorder))
}

func TestClientDiagnosticsAcceptsAndDiscardsWithoutPersistence(t *testing.T) {
	root := t.TempDir()
	recorder, err := diagnostics.Open(root, diagnostics.Options{Disabled: true})
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, postClientBatch(clientDiagnosticsServer(t, recorder), validClientBatch, ""))
	require.Empty(t, clientEvents(t, root, recorder))

	require.Equal(t, http.StatusNoContent, postClientBatch(clientDiagnosticsServer(t, nil), validClientBatch, ""))
}
