package provideraccess

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRuntimeHealthSnapshotNeverWaitsForSessionStore(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	var startOnce, releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	f := Factory{Load: func() (Managed, bool, error) {
		startOnce.Do(func() { close(started) })
		<-release
		return nil, false, nil
	}}
	read := f.CachedRuntimeReports(ctx, testEnv(t, nil))
	returned := make(chan []RuntimeReport, 1)
	go func() { returned <- read() }()
	var first []RuntimeReport
	select {
	case first = <-returned:
		require.Equal(t, "checking", first[0].Next.Reason)
	case <-time.After(time.Second):
		t.Fatal("health snapshot waited on the session store")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background source refresh did not start")
	}
	go func() { returned <- read() }()
	select {
	case pending := <-returned:
		require.Equal(t, "checking", pending[0].Next.Reason)
	case <-time.After(time.Second):
		t.Fatal("a second health snapshot waited on the in-flight refresh")
	}
	releaseOnce.Do(func() { close(release) })
	require.Eventually(t, func() bool { return read()[0].Next.Reason == "not_configured" }, time.Second, time.Millisecond)
	// Already returned snapshots remain immutable after a refresh publishes.
	require.Equal(t, "checking", first[0].Next.Reason)
}
