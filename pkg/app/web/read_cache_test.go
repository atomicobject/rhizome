package web

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// waitForJoins waits until n callers besides the first have joined key's
// flight, so a test releases the run only once every caller is waiting on it.
func waitForJoins(t *testing.T, cache *readCache, hash string, joins uint64) {
	t.Helper()
	require.Eventually(t, func() bool {
		return cache.Metrics().Queries[hash].Joins == joins
	}, 5*time.Second, time.Millisecond)
}

func waitForMiss(t *testing.T, cache *readCache, hash string) {
	t.Helper()
	require.Eventually(t, func() bool {
		return cache.Metrics().Queries[hash].Misses == 1
	}, 5*time.Second, time.Millisecond)
}

func TestReadCacheExecutesIdenticalConcurrentReadsOnce(t *testing.T) {
	cache := newReadCache(defaultReadCacheBytes, 0)
	defer cache.Close()
	release := make(chan struct{})
	var runs atomic.Int32
	run := func(context.Context) ([]byte, bool, error) {
		runs.Add(1)
		<-release
		return []byte("body"), true, nil
	}

	const callers = 8
	var wg sync.WaitGroup
	bodies := make([][]byte, callers)
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, err := cache.do(context.Background(), "g1|q", "q", run)
			require.NoError(t, err)
			bodies[i] = body
		}()
	}
	waitForJoins(t, cache, "q", callers-1)
	close(release)
	wg.Wait()
	require.EqualValues(t, 1, runs.Load())
	for _, body := range bodies {
		require.Equal(t, "body", string(body))
	}

	body, err := cache.do(context.Background(), "g1|q", "q", run)
	require.NoError(t, err)
	require.Equal(t, "body", string(body))
	require.EqualValues(t, 1, runs.Load(), "a stored body is a hit")
	require.EqualValues(t, 1, cache.Metrics().Queries["q"].Hits)
}

func TestReadCacheCancelledCallerNeverFailsTheOthers(t *testing.T) {
	cache := newReadCache(defaultReadCacheBytes, 0)
	defer cache.Close()
	release := make(chan struct{})
	runCtxErr := make(chan error, 1)
	run := func(ctx context.Context) ([]byte, bool, error) {
		<-release
		runCtxErr <- ctx.Err()
		return []byte("body"), true, nil
	}

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstErr := make(chan error, 1)
	go func() {
		_, err := cache.do(firstCtx, "g1|q", "q", run)
		firstErr <- err
	}()
	waitForMiss(t, cache, "q")
	secondBody := make(chan []byte, 1)
	go func() {
		body, err := cache.do(context.Background(), "g1|q", "q", run)
		require.NoError(t, err)
		secondBody <- body
	}()
	waitForJoins(t, cache, "q", 1)
	cancelFirst()
	require.ErrorIs(t, <-firstErr, context.Canceled)
	close(release)
	require.Equal(t, "body", string(<-secondBody))
	require.NoError(t, <-runCtxErr, "the shared execution is detached from the cancelled caller")
}

func TestReadCacheJoinersExecuteOnTheirOwnWhenTheSharedRunFails(t *testing.T) {
	cache := newReadCache(defaultReadCacheBytes, 0)
	defer cache.Close()
	release := make(chan struct{})
	var runs atomic.Int32
	failing := errors.New("index initializing")
	run := func(context.Context) ([]byte, bool, error) {
		if runs.Add(1) == 1 {
			<-release
			return nil, false, failing
		}
		return []byte("own"), true, nil
	}

	firstErr := make(chan error, 1)
	go func() {
		_, err := cache.do(context.Background(), "g1|q", "q", run)
		firstErr <- err
	}()
	waitForMiss(t, cache, "q")
	joinerBody := make(chan []byte, 1)
	go func() {
		body, err := cache.do(context.Background(), "g1|q", "q", run)
		require.NoError(t, err)
		joinerBody <- body
	}()
	waitForJoins(t, cache, "q", 1)
	close(release)
	require.ErrorIs(t, <-firstErr, failing)
	require.Equal(t, "own", string(<-joinerBody))
}

func TestReadCacheDoesNotStoreAnUnstorableBody(t *testing.T) {
	cache := newReadCache(defaultReadCacheBytes, 0)
	defer cache.Close()
	var runs atomic.Int32
	run := func(context.Context) ([]byte, bool, error) {
		runs.Add(1)
		return []byte("changed during execution"), false, nil
	}
	for range 2 {
		_, err := cache.do(context.Background(), "g1|q", "q", run)
		require.NoError(t, err)
	}
	require.EqualValues(t, 2, runs.Load())
	require.Zero(t, cache.Metrics().Entries)
}

func TestReadCacheEvictsLeastRecentlyUsedPastItsByteBound(t *testing.T) {
	body := make([]byte, 1000)
	cache := newReadCache(2*entrySize("g|a", body), 0)
	defer cache.Close()
	var runs atomic.Int32
	run := func(context.Context) ([]byte, bool, error) {
		runs.Add(1)
		return body, true, nil
	}
	read := func(key string) {
		_, err := cache.do(context.Background(), key, "q", run)
		require.NoError(t, err)
	}
	read("g|a")
	read("g|b")
	read("g|a") // a is now more recent than b
	read("g|c") // evicts b
	require.EqualValues(t, 3, runs.Load())
	read("g|a")
	require.EqualValues(t, 3, runs.Load(), "a survived")
	read("g|b")
	require.EqualValues(t, 4, runs.Load(), "b was evicted")
	require.LessOrEqual(t, cache.Metrics().Bytes, 2*entrySize("g|a", body))

	huge := newReadCache(10, 0)
	defer huge.Close()
	_, err := huge.do(context.Background(), "g|a", "q", run)
	require.NoError(t, err)
	require.Zero(t, huge.Metrics().Entries, "a body larger than the bound is never stored")
}

func TestReadCacheCloseCancelsAndDrainsSharedExecutions(t *testing.T) {
	cache := newReadCache(defaultReadCacheBytes, 0)
	started := make(chan struct{})
	finished := atomic.Bool{}
	run := func(ctx context.Context) ([]byte, bool, error) {
		close(started)
		<-ctx.Done()
		finished.Store(true)
		return nil, false, ctx.Err()
	}
	go func() { _, _ = cache.do(context.Background(), "g|q", "q", run) }()
	<-started
	cache.Close()
	require.True(t, finished.Load(), "Close returns only after the execution exits")
}

func TestReadCacheVerificationReportsAStaleHit(t *testing.T) {
	cache := newReadCache(defaultReadCacheBytes, 1)
	var recorded []ReadCacheMetrics
	cache.record = func(metrics ReadCacheMetrics) { recorded = append(recorded, metrics) }
	var answer atomic.Value
	answer.Store("first")
	run := func(context.Context) ([]byte, bool, error) {
		return []byte(answer.Load().(string)), true, nil
	}
	_, err := cache.do(context.Background(), "g|q", "q", run)
	require.NoError(t, err)
	// Same generation, different data: a dependency the generation missed.
	answer.Store("second")
	body, err := cache.do(context.Background(), "g|q", "q", run)
	require.NoError(t, err)
	require.Equal(t, "first", string(body))
	cache.Close()
	require.Len(t, recorded, 1)
	require.EqualValues(t, 1, recorded[0].Queries["q"].Mismatches)
}
