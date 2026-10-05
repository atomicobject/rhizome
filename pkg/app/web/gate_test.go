package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestRuntimeIndexGate_DefaultOpen guarantees that runtimes constructed
// without EnableIndexGate (the test-friendly default — tests, headless
// embeddings, anything that doesn't go through `rzm serve`'s gate setup)
// pass through WaitIndexReady immediately. This is what keeps the existing
// integration test suite green without each test calling MarkIndexReady.
func TestRuntimeIndexGate_DefaultOpen(t *testing.T) {
	rt := &Runtime{}
	require.True(t, rt.IndexReady())
	require.True(t, rt.NoteReadReady())
	require.NoError(t, rt.WaitIndexReady(context.Background(), 10*time.Millisecond))
	require.NoError(t, rt.WaitNoteReadReady(context.Background(), 10*time.Millisecond))
}

func TestRuntimeIndexGate_GatedBlocksThenOpens(t *testing.T) {
	rt := &Runtime{}
	rt.EnableIndexGate()
	require.False(t, rt.IndexReady())

	// Tight timeout demonstrates the gate actually blocks.
	err := rt.WaitIndexReady(context.Background(), 5*time.Millisecond)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	rt.MarkIndexReady()
	require.True(t, rt.IndexReady())
	require.NoError(t, rt.WaitIndexReady(context.Background(), 5*time.Millisecond))
	require.True(t, rt.NoteReadReady())
	require.NoError(t, rt.WaitNoteReadReady(context.Background(), 5*time.Millisecond))
}

func TestRuntimeIndexGate_NoteReadsCanOpenBeforeTheCompleteIndex(t *testing.T) {
	rt := &Runtime{}
	rt.EnableIndexGate()
	require.False(t, rt.NoteReadReady())
	require.False(t, rt.IndexReady())

	rt.MarkNoteReadReady()

	require.True(t, rt.NoteReadReady())
	require.NoError(t, rt.WaitNoteReadReady(context.Background(), 5*time.Millisecond))
	require.False(t, rt.IndexReady())
	require.ErrorIs(t, rt.WaitIndexReady(context.Background(), 5*time.Millisecond), context.DeadlineExceeded)
}

func TestRuntimeIndexGate_ContextCancellationReleasesWaiter(t *testing.T) {
	rt := &Runtime{}
	rt.EnableIndexGate()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- rt.WaitIndexReady(ctx, time.Minute)
	}()

	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

func TestRuntimeIndexGate_ConcurrentMarksAreSafe(t *testing.T) {
	rt := &Runtime{}
	rt.EnableIndexGate()

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				rt.MarkIndexReady()
				return
			}
			rt.MarkIndexReady()
		}(i)
	}
	wg.Wait()

	require.True(t, rt.IndexReady())
}

// TestRequireIndexReady_Returns503AndRetryAfter exercises the middleware end
// to end via httptest: a closed gate must return 503 + `Retry-After: 5` with
// the structured `INDEX_INITIALIZING` error code so frontend banners and
// custom-app retry loops can distinguish first-launch waits from real
// outages. After MarkIndexReady fires the same handler must serve 200 with
// the wrapped payload.
func TestRequireIndexReady_Returns503AndRetryAfterThenOpens(t *testing.T) {
	rt := &Runtime{}
	rt.EnableIndexGate()

	srv := &Server{runtime: rt}

	var calls atomic.Int32
	handler := srv.requireIndexReady(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	// Use a context with a tight deadline to force the closed gate to time
	// out without waiting the production indexGateTimeout. The middleware
	// returns ctx.Err() to writePublicError, which maps to 503.
	gatedReq := httptest.NewRequest(http.MethodGet, "/api/v1/ontology/types", nil)
	gatedCtx, gatedCancel := context.WithTimeout(gatedReq.Context(), 10*time.Millisecond)
	defer gatedCancel()
	gatedRes := httptest.NewRecorder()
	handler(gatedRes, gatedReq.WithContext(gatedCtx))

	require.Equal(t, http.StatusServiceUnavailable, gatedRes.Code)
	require.Equal(t, "5", gatedRes.Header().Get("Retry-After"))
	require.Contains(t, gatedRes.Body.String(), PublicErrorIndexInitializing)
	require.Equal(t, int32(0), calls.Load(), "underlying handler must not run while gated")

	rt.MarkIndexReady()

	openReq := httptest.NewRequest(http.MethodGet, "/api/v1/ontology/types", nil)
	openRes := httptest.NewRecorder()
	handler(openRes, openReq)

	require.Equal(t, http.StatusOK, openRes.Code)
	require.Equal(t, int32(1), calls.Load())
}

// TestRequireIndexReady_PassthroughOnUngatedRuntime confirms the middleware
// does not impose latency or errors on the integration-test fixtures — those
// servers never call EnableIndexGate, so requireIndexReady must hand straight
// off to the wrapped handler.
func TestRequireIndexReady_PassthroughOnUngatedRuntime(t *testing.T) {
	srv := &Server{runtime: &Runtime{}}

	var ran atomic.Bool
	handler := srv.requireIndexReady(func(w http.ResponseWriter, r *http.Request) {
		ran.Store(true)
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ontology/types", nil)
	res := httptest.NewRecorder()
	handler(res, req)

	require.True(t, ran.Load())
	require.Equal(t, http.StatusOK, res.Code)
	require.Empty(t, res.Header().Get("Retry-After"))
}

// TestStatusReportsIndexState ties the runtime state to the status payload
// so frontend `IndexReadyBanner` and capability consumers see the same
// signal the middleware is gating on.
func TestStatusReportsIndexState(t *testing.T) {
	srv := &Server{runtime: &Runtime{}}
	srv.runtime.EnableIndexGate()

	require.Equal(t, "initializing", srv.status(context.Background()).IndexState)

	srv.runtime.MarkIndexReady()
	require.Equal(t, "ready", srv.status(context.Background()).IndexState)
}
