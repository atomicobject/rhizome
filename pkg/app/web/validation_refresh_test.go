package web

import (
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidationRefreshEndpoint(t *testing.T) {
	requester := &recordingValidationRefreshRequester{}
	store, err := semdb.Open(filepath.Join(t.TempDir(), "validation.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	runtime := &Runtime{IntelStore: store}
	runtime.SetValidationRefreshRequester(requester)
	server := &Server{runtime: runtime}
	rec := httptest.NewRecorder()
	server.handlePublicValidationRefresh(rec, httptest.NewRequest(http.MethodPost, "/api/v2/validate/refresh", nil))
	require.Equal(t, http.StatusAccepted, rec.Code)
	require.JSONEq(t, `{"accepted":true}`, rec.Body.String())
	require.Equal(t, int32(1), requester.calls.Load())

	rec = httptest.NewRecorder()
	server.handlePublicValidationRefresh(rec, httptest.NewRequest(http.MethodGet, "/api/v2/validate/refresh", nil))
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	require.Equal(t, int32(1), requester.calls.Load())

	rec = httptest.NewRecorder()
	(&Server{}).handlePublicValidationRefresh(rec, httptest.NewRequest(http.MethodPost, "/api/v2/validate/refresh", nil))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	runtime.IntelStore = nil
	rec = httptest.NewRecorder()
	server.handlePublicValidationRefresh(rec, httptest.NewRequest(http.MethodPost, "/api/v2/validate/refresh", nil))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Equal(t, int32(1), requester.calls.Load(), "initializing store must not admit a refresh it cannot run")
}

func TestValidationEnvelopeReportsQueuedRefreshIndependentlyOfGeneration(t *testing.T) {
	requester := &recordingValidationRefreshRequester{}
	store, err := semdb.Open(filepath.Join(t.TempDir(), "validation.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	runtime := &Runtime{IntelStore: store}
	runtime.SetValidationRefreshRequester(requester)
	server := &Server{runtime: runtime}
	requester.RequestValidationRefresh()
	generation, err := store.SetValidationRunning(t.Context())
	require.NoError(t, err)
	_, err = store.PublishValidationSnapshot(t.Context(), webValidationSnapshot(generation, 0, 0))
	require.NoError(t, err)
	envelope := server.readCachedValidationEnvelope(t.Context())
	require.Equal(t, generation, envelope.PublishedGeneration)
	require.True(t, envelope.RefreshPending, "an older publication cannot hide queued runtime work")
	requester.pending.Store(false)
	envelope = server.readCachedValidationEnvelope(t.Context())
	require.Equal(t, generation, envelope.PublishedGeneration)
	require.False(t, envelope.RefreshPending)
}
