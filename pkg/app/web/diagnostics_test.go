package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/stretchr/testify/require"
)

func TestDiagnosticRequestKeepsRouteAndAuthorizedCorrelationWithoutPayload(t *testing.T) {
	root := t.TempDir()
	var console bytes.Buffer
	recorder, err := diagnostics.Open(root, diagnostics.Options{Stderr: &console})
	require.NoError(t, err)
	parent := diagnostics.NewOperation("command", "index")
	s := &Server{diagnostics: recorder, mux: http.NewServeMux(), cfg: Config{RuntimeControl: &RuntimeControl{Token: "synthetic-control-token"}}}
	s.mux.HandleFunc("/api/control/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte("PRIVATE_RESPONSE"))
	})
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/control/PRIVATE_PATH?query=PRIVATE_QUERY", strings.NewReader("PRIVATE_BODY"))
	appruntime.SetBearerToken(request, "synthetic-control-token")
	appruntime.SetDiagnosticIdentity(request, diagnostics.WithOperation(context.Background(), parent))
	observed := httptest.NewRecorder()
	s.observeRequest(s.mux).ServeHTTP(observed, request)
	require.Equal(t, http.StatusConflict, observed.Code)
	request.Header.Set(appruntime.DiagnosticParentHeader, "../../PRIVATE_ID")
	request.Header.Set("Authorization", "Bearer incorrect")
	s.observeRequest(s.mux).ServeHTTP(httptest.NewRecorder(), request)
	require.NoError(t, recorder.Close())
	events, err := diagnostics.ReadEvents(root, diagnostics.Filter{})
	require.NoError(t, err)
	require.Len(t, events.Events, 4)
	var correlated, uncorrelated bool
	for _, event := range events.Events {
		if event.Name != "http.request.finished" {
			continue
		}
		require.Equal(t, "error", event.Attributes["status"])
		require.Equal(t, "INFO", event.Level)
		require.Equal(t, "/api/control/", event.Attributes["route"])
		require.Equal(t, float64(http.StatusConflict), event.Attributes["http_status"])
		if event.ParentOperationID == parent.ID {
			correlated = true
			require.Equal(t, parent.TraceID, event.TraceID)
		} else {
			uncorrelated = true
			require.Empty(t, event.ParentOperationID)
		}
		data, err := json.Marshal(event)
		require.NoError(t, err)
		require.NotContains(t, string(data), "PRIVATE")
		require.NotContains(t, string(data), "synthetic-control-token")
	}
	reports, err := diagnostics.ReadReports(root, diagnostics.Filter{})
	require.NoError(t, err)
	require.Empty(t, reports.Reports)
	require.True(t, correlated)
	require.True(t, uncorrelated)
	require.Empty(t, console.String(), "ordinary HTTP client errors must not flood the terminal")
}

func TestDiagnosticResponsePreservesStreamingFlush(t *testing.T) {
	response := httptest.NewRecorder()
	wrapped := &diagnosticResponse{ResponseWriter: response}
	_, err := wrapped.Write([]byte("event: ready\n\n"))
	require.NoError(t, err)
	diagnosticFlusher{wrapped}.Flush()
	require.True(t, response.Flushed)
	require.Equal(t, http.StatusOK, wrapped.status)
}

func TestDiagnosticHTTPPanicNeverRecordsSuccess(t *testing.T) {
	root := t.TempDir()
	recorder, err := diagnostics.Open(root, diagnostics.Options{})
	require.NoError(t, err)
	s := &Server{diagnostics: recorder, mux: http.NewServeMux()}
	handler := s.observeRequest(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("PRIVATE_PANIC") }))
	require.Panics(t, func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://127.0.0.1/", nil))
	})
	require.NoError(t, recorder.Close())
	events, err := diagnostics.ReadEvents(root, diagnostics.Filter{})
	require.NoError(t, err)
	require.Len(t, events.Events, 2)
	require.Equal(t, "error", events.Events[1].Attributes["status"])
	require.Equal(t, "handler_panicked", events.Events[1].Attributes["reason_code"])
	require.Equal(t, float64(http.StatusInternalServerError), events.Events[1].Attributes["http_status"])
	require.NotContains(t, events.Events[1].Message, "PRIVATE")
}

func TestDiagnosticHTTPServerFailureIsRetainedWithoutTerminalFlood(t *testing.T) {
	root := t.TempDir()
	var console bytes.Buffer
	recorder, err := diagnostics.Open(root, diagnostics.Options{Stderr: &console})
	require.NoError(t, err)
	s := &Server{diagnostics: recorder, mux: http.NewServeMux()}
	handler := s.observeRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "request failed", http.StatusServiceUnavailable)
	}))
	for range 3 {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/", nil))
		require.Equal(t, http.StatusServiceUnavailable, response.Code)
	}
	require.NoError(t, recorder.Close())
	require.Empty(t, console.String())
	events, err := diagnostics.ReadEvents(root, diagnostics.Filter{})
	require.NoError(t, err)
	require.Len(t, events.Events, 6)
	for _, event := range events.Events {
		if event.Name == "http.request.finished" {
			require.Equal(t, "ERROR", event.Level)
			require.Equal(t, "error", event.Attributes["status"])
			require.Equal(t, float64(http.StatusServiceUnavailable), event.Attributes["http_status"])
		}
	}
}

type diagnosticUnflushable struct{ header http.Header }

func (w diagnosticUnflushable) Header() http.Header          { return w.header }
func (diagnosticUnflushable) Write(data []byte) (int, error) { return len(data), nil }
func (diagnosticUnflushable) WriteHeader(int)                {}

func TestDiagnosticRequestDoesNotInventStreamingSupport(t *testing.T) {
	root := t.TempDir()
	recorder, err := diagnostics.Open(root, diagnostics.Options{})
	require.NoError(t, err)
	defer recorder.Close()
	s := &Server{diagnostics: recorder, mux: http.NewServeMux()}
	handler := s.observeRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, ok := w.(http.Flusher); require.False(t, ok) }))
	handler.ServeHTTP(diagnosticUnflushable{header: make(http.Header)}, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/", nil))
}
