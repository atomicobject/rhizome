package watchhub

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDebouncedBatchesAreDeliveredInOrderWithoutOverlappingCallbacks(t *testing.T) {
	t.Parallel()
	hub, err := NewHub(t.TempDir(), Options{DisableFSNotify: true, Debounce: time.Millisecond})
	require.NoError(t, err)
	hub.Start(context.Background())
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() { unblock(); require.NoError(t, hub.Close()) })
	hub.Subscribe("ordered", Filter{IncludeFiles: true}, func(_ context.Context, events []WatchEvent) {
		for _, event := range events {
			switch event.RelPath {
			case "first.md":
				close(firstStarted)
				<-release
			case "second.md":
				close(secondStarted)
			}
		}
	}, nil)
	hub.enqueue(WatchEvent{RelPath: "first.md", Op: OpWrite})
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first callback did not start")
	}
	hub.enqueue(WatchEvent{RelPath: "second.md", Op: OpWrite})
	select {
	case <-secondStarted:
		t.Fatal("second batch overlapped the first callback")
	case <-time.After(25 * time.Millisecond):
	}
	unblock()
	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("second batch did not drain after the first callback")
	}
}
