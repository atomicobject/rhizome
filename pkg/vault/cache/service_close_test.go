package cache

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCloseWaitsForBackgroundRecrawl(t *testing.T) {
	var discoveries atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	svc, err := NewService(t.TempDir(), Options{DiscoverFiles: func() ([]string, error) {
		if discoveries.Add(1) == 2 {
			close(started)
			<-release
		}
		return nil, nil
	}})
	require.NoError(t, err)
	t.Cleanup(func() { unblock(); _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))
	svc.MarkStale()
	require.NoError(t, svc.Refresh(context.Background()))
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("recrawl did not start")
	}
	closed := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() { _ = svc.Close(); closed <- struct{}{} }()
	}
	<-svc.lifetime.Done()
	remaining := 2
	select {
	case <-closed:
		remaining--
		t.Error("Close returned while background discovery was blocked")
	case <-time.After(50 * time.Millisecond):
	}
	unblock()
	for i := 0; i < remaining; i++ {
		select {
		case <-closed:
		case <-time.After(5 * time.Second):
			t.Fatal("Close did not finish after discovery returned")
		}
	}
	require.Equal(t, uint64(1), svc.Metrics().ResyncCount)
	svc.mu.RLock()
	running := svc.recrawling
	svc.mu.RUnlock()
	require.False(t, running, "Close must drain recrawl state updates")
	require.NoError(t, svc.Close())
}

func TestClosePreventsBackgroundRecrawlAdmission(t *testing.T) {
	svc, err := NewService(t.TempDir(), Options{})
	require.NoError(t, err)
	require.NoError(t, svc.EnsureReady(context.Background()))
	require.NoError(t, svc.Close())
	svc.MarkStale()
	require.NoError(t, svc.Refresh(context.Background()))
	svc.mu.RLock()
	stale, running := svc.stale, svc.recrawling
	svc.mu.RUnlock()
	require.True(t, stale, "closed cache must not consume a new recrawl request")
	require.False(t, running)
	require.Equal(t, uint64(0), svc.Metrics().ResyncCount)
}

func TestCloseRacesBackgroundRecrawlAdmission(t *testing.T) {
	for i := 0; i < 100; i++ {
		svc, err := NewService(t.TempDir(), Options{})
		require.NoError(t, err)
		require.NoError(t, svc.EnsureReady(context.Background()))
		svc.MarkStale()
		start := make(chan struct{})
		refreshed := make(chan error, 1)
		go func() {
			<-start
			refreshed <- svc.Refresh(context.Background())
		}()
		close(start)
		require.NoError(t, svc.Close())
		require.NoError(t, <-refreshed)
		svc.mu.RLock()
		running := svc.recrawling
		svc.mu.RUnlock()
		require.False(t, running, "racing refresh must not outlive Close")
	}
}
