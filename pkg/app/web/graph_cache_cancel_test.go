package web

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/stretchr/testify/require"
)

const graphCacheCancelScope = "global|limit=2000|depth=2"

// joinSignalContext reports the first Done call. getOrBuild calls Done only in
// its wait select, after the caller has started or joined the in-flight build,
// so the signal proves the caller is waiting on the shared flight.
type joinSignalContext struct {
	context.Context
	once   sync.Once
	joined chan struct{}
}

func (c *joinSignalContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.joined) })
	return c.Context.Done()
}

type graphCacheWaiter struct {
	cancel context.CancelFunc
	joined chan struct{}
	done   chan struct{}
	resp   GraphResponse
	err    error
}

func (w *graphCacheWaiter) settled() bool {
	select {
	case <-w.done:
		return true
	default:
		return false
	}
}

// graphCacheHarness drives callers into one getOrBuild key, so the
// leader/follower split is exercised at the cache owner.
type graphCacheHarness struct {
	t     *testing.T
	cache *graphResponseCache
	wg    sync.WaitGroup
	build func(context.Context) (GraphResponse, error)
}

func newGraphCacheHarness(t *testing.T, build func(context.Context) (GraphResponse, error)) *graphCacheHarness {
	t.Helper()
	cache := newGraphResponseCache(defaultScopedGraphCacheEntries)
	t.Cleanup(cache.Close)
	return &graphCacheHarness{t: t, cache: cache, build: build}
}

func (h *graphCacheHarness) get(ctx context.Context) (GraphResponse, error) {
	return h.cache.getOrBuild(ctx, graphCacheCancelScope, true, h.build)
}

func (h *graphCacheHarness) start(cancellable bool) *graphCacheWaiter {
	h.t.Helper()
	ctx := context.Background()
	w := &graphCacheWaiter{joined: make(chan struct{}), done: make(chan struct{})}
	if cancellable {
		ctx, w.cancel = context.WithCancel(ctx)
	}
	joinCtx := &joinSignalContext{Context: ctx, joined: w.joined}
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		defer close(w.done)
		w.resp, w.err = h.get(joinCtx)
	}()
	return w
}

// waitJoined blocks until every waiter is waiting on the shared flight, so a
// later cancel or release cannot race a caller that has not joined yet.
func (h *graphCacheHarness) waitJoined(waiters ...*graphCacheWaiter) {
	h.t.Helper()
	for _, w := range waiters {
		select {
		case <-w.joined:
		case <-time.After(5 * time.Second):
			h.t.Fatal("caller never joined the in-flight build")
		}
	}
}

func (h *graphCacheHarness) requireSettledPromptly(waiters ...*graphCacheWaiter) {
	h.t.Helper()
	for _, w := range waiters {
		select {
		case <-w.done:
		case <-time.After(5 * time.Second):
			h.t.Fatal("waiter should return on its own cancellation while the build is still running")
		}
	}
}

func (h *graphCacheHarness) inflightLen() int {
	h.cache.mu.Lock()
	defer h.cache.mu.Unlock()
	return len(h.cache.inflight)
}

func newGraphCacheServer(t *testing.T) *Server {
	t.Helper()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "graph-cache.db"))
	require.NoError(t, err)
	cache := newGraphResponseCache(defaultScopedGraphCacheEntries)
	t.Cleanup(func() {
		cache.Close()
		_ = store.Close()
	})
	return &Server{runtime: &Runtime{IntelStore: store}, graphCache: cache}
}

func graphCacheBuiltResponse() GraphResponse {
	return GraphResponse{Nodes: []GraphNode{{ID: "note:a", Kind: "note", Label: "a"}}}
}

// blockingGraphBuild reports entry on ready, then finishes on release or returns
// its own context's error, whichever comes first.
func blockingGraphBuild(calls *atomic.Int32, ready chan struct{}, release chan struct{}) func(context.Context) (GraphResponse, error) {
	var started sync.Once
	return func(ctx context.Context) (GraphResponse, error) {
		calls.Add(1)
		started.Do(func() { close(ready) })
		select {
		case <-release:
			return graphCacheBuiltResponse(), nil
		case <-ctx.Done():
			return GraphResponse{}, ctx.Err()
		}
	}
}

func TestCachedGraphResponse_WaiterCancellationIsIndependent(t *testing.T) {
	t.Run("follower cancelled", func(t *testing.T) {
		var calls atomic.Int32
		ready := make(chan struct{})
		release := make(chan struct{})

		h := newGraphCacheHarness(t, blockingGraphBuild(&calls, ready, release))

		leader := h.start(false)
		<-ready
		followers := []*graphCacheWaiter{h.start(true), h.start(false), h.start(false)}
		h.waitJoined(append([]*graphCacheWaiter{leader}, followers...)...)
		require.EqualValues(t, 1, calls.Load(), "joined callers must share one build")

		followers[0].cancel()
		h.requireSettledPromptly(followers[0])
		require.ErrorIs(t, followers[0].err, context.Canceled)
		require.False(t, leader.settled(), "leader must still be building")

		close(release)
		h.wg.Wait()

		require.NoError(t, leader.err)
		require.Equal(t, graphCacheBuiltResponse(), leader.resp)
		for _, w := range followers[1:] {
			require.NoError(t, w.err)
			require.Equal(t, graphCacheBuiltResponse(), w.resp)
		}
		require.EqualValues(t, 1, calls.Load(), "joined callers must share one build")

		resp, err := h.get(context.Background())
		require.NoError(t, err)
		require.Equal(t, graphCacheBuiltResponse(), resp)
		require.EqualValues(t, 1, calls.Load(), "subsequent call must be a cache hit")
	})

	t.Run("initiator cancelled", func(t *testing.T) {
		var calls atomic.Int32
		ready := make(chan struct{})
		release := make(chan struct{})

		h := newGraphCacheHarness(t, blockingGraphBuild(&calls, ready, release))

		leader := h.start(true)
		<-ready
		followers := []*graphCacheWaiter{h.start(false), h.start(false), h.start(false)}
		h.waitJoined(append([]*graphCacheWaiter{leader}, followers...)...)
		require.EqualValues(t, 1, calls.Load(), "joined callers must share one build")

		leader.cancel()
		h.requireSettledPromptly(leader)
		require.ErrorIs(t, leader.err, context.Canceled)

		close(release)
		h.wg.Wait()

		for _, w := range followers {
			require.NoError(t, w.err)
			require.Equal(t, graphCacheBuiltResponse(), w.resp)
		}
		require.EqualValues(t, 1, calls.Load(), "the initiator's build must survive its own cancellation")

		_, err := h.get(context.Background())
		require.NoError(t, err)
		require.EqualValues(t, 1, calls.Load(), "subsequent call must be a cache hit")
	})

	t.Run("everyone cancelled", func(t *testing.T) {
		var calls atomic.Int32
		ready := make(chan struct{})
		release := make(chan struct{})
		built := make(chan struct{})
		var builtOnce sync.Once

		shared := blockingGraphBuild(&calls, ready, release)
		h := newGraphCacheHarness(t, func(ctx context.Context) (GraphResponse, error) {
			resp, err := shared(ctx)
			if err == nil {
				builtOnce.Do(func() { close(built) })
			}
			return resp, err
		})

		leader := h.start(true)
		<-ready
		waiters := []*graphCacheWaiter{leader, h.start(true), h.start(true), h.start(true)}
		h.waitJoined(waiters...)

		for _, w := range waiters {
			w.cancel()
		}
		h.requireSettledPromptly(waiters...)
		h.wg.Wait()
		for _, w := range waiters {
			require.ErrorIs(t, w.err, context.Canceled)
		}

		close(release)
		select {
		case <-built:
		case <-time.After(5 * time.Second):
			t.Fatal("abandoned build never completed")
		}

		resp, err := h.get(context.Background())
		require.NoError(t, err)
		require.Equal(t, graphCacheBuiltResponse(), resp)
		require.EqualValues(t, 1, calls.Load(), "abandoned build still populates the cache")
	})

	t.Run("build error not cached", func(t *testing.T) {
		buildErr := errors.New("graph build failed")
		var calls atomic.Int32
		var started sync.Once
		ready := make(chan struct{})
		release := make(chan struct{})

		h := newGraphCacheHarness(t, func(ctx context.Context) (GraphResponse, error) {
			calls.Add(1)
			started.Do(func() { close(ready) })
			<-release
			return GraphResponse{}, buildErr
		})

		leader := h.start(false)
		<-ready
		followers := []*graphCacheWaiter{h.start(false), h.start(false)}
		h.waitJoined(append([]*graphCacheWaiter{leader}, followers...)...)

		close(release)
		h.wg.Wait()

		for _, w := range append([]*graphCacheWaiter{leader}, followers...) {
			require.ErrorIs(t, w.err, buildErr)
		}
		require.EqualValues(t, 1, calls.Load(), "joined callers must share one failed build")
		require.Zero(t, h.inflightLen())

		_, err := h.get(context.Background())
		require.ErrorIs(t, err, buildErr)
		require.EqualValues(t, 2, calls.Load(), "failed builds must not be cached")
	})

	t.Run("close during build", func(t *testing.T) {
		var calls atomic.Int32
		ready := make(chan struct{})
		release := make(chan struct{})
		defer close(release)

		h := newGraphCacheHarness(t, blockingGraphBuild(&calls, ready, release))

		leader := h.start(false)
		<-ready
		followers := []*graphCacheWaiter{h.start(false), h.start(false)}
		h.waitJoined(append([]*graphCacheWaiter{leader}, followers...)...)

		closed := make(chan struct{})
		go func() {
			h.cache.Close()
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.After(5 * time.Second):
			t.Fatal("Close did not cancel the in-flight build")
		}
		h.wg.Wait()

		for _, w := range append([]*graphCacheWaiter{leader}, followers...) {
			require.ErrorIs(t, w.err, context.Canceled)
		}
		require.Zero(t, h.inflightLen())

		_, err := h.get(context.Background())
		require.ErrorIs(t, err, context.Canceled)
		require.EqualValues(t, 1, calls.Load(), "closed cache must not start new builds")
	})
}

func TestGetOrBuild_BuildContextIsNotTheCallerContext(t *testing.T) {
	type ctxKey struct{}

	cache := newGraphResponseCache(defaultScopedGraphCacheEntries)
	defer cache.Close()

	callerCtx := context.WithValue(context.Background(), ctxKey{}, "request-scoped")
	var buildValue any
	_, err := cache.getOrBuild(callerCtx, "global|key", true, func(ctx context.Context) (GraphResponse, error) {
		buildValue = ctx.Value(ctxKey{})
		return graphCacheBuiltResponse(), nil
	})
	require.NoError(t, err)
	require.Nil(t, buildValue, "the shared build must not inherit the initiating request context")
}

func TestGraphResponseCacheSpawnDrainsOnClose(t *testing.T) {
	cache := newGraphResponseCache(4)
	started := make(chan struct{})
	finished := make(chan struct{})
	if !cache.spawn(func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		close(finished)
	}) {
		t.Fatal("spawn rejected before close")
	}
	<-started
	cache.Close()
	select {
	case <-finished:
	default:
		t.Fatal("Close returned before the spawned work finished")
	}
	if cache.spawn(func(context.Context) { t.Error("ran after close") }) {
		t.Fatal("spawn accepted after close")
	}
}

func TestCachedGraphResponse_CancelledCallerDoesNotBuild(t *testing.T) {
	var calls atomic.Int32
	srv := newGraphCacheServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := srv.cachedGraphResponse(ctx, graphCacheCancelScope, true, func(context.Context) (GraphResponse, error) {
		calls.Add(1)
		return graphCacheBuiltResponse(), nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, int32(0), calls.Load(), "cancelled caller ran its own uncached build")
}

// Without a graph cache the wrapper still serves an uncached build.
func TestCachedGraphResponse_FallsBackToUncachedBuildWithoutCache(t *testing.T) {
	var calls atomic.Int32
	srv := newGraphCacheServer(t)
	srv.graphCache = nil

	resp, err := srv.cachedGraphResponse(context.Background(), graphCacheCancelScope, true, func(context.Context) (GraphResponse, error) {
		calls.Add(1)
		return graphCacheBuiltResponse(), nil
	})
	require.NoError(t, err)
	require.Equal(t, graphCacheBuiltResponse(), resp)
	require.Equal(t, int32(1), calls.Load())
}
