package watchhub

import (
	"context"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/require"
)

func TestCloseDiscardsPendingDelivery(t *testing.T) {
	hub, err := NewHub(t.TempDir(), Options{DisableFSNotify: true, Debounce: 30 * time.Millisecond})
	require.NoError(t, err)
	hub.Start(context.Background())
	delivered := make(chan struct{}, 8)
	hub.Subscribe("shutdown", Filter{IncludeFiles: true}, func(context.Context, []WatchEvent) { delivered <- struct{}{} }, func(context.Context, StaleEvent) { delivered <- struct{}{} })
	hub.EmitHintPaths([]string{"pending.md"})
	require.NoError(t, hub.Close())
	select {
	case <-delivered:
		t.Fatal("subscriber called after Close")
	case <-time.After(100 * time.Millisecond):
	}
	hub.EmitHintPaths([]string{"late.md"})
	hub.EmitHintResync()
	select {
	case <-delivered:
		t.Fatal("new delivery accepted after Close")
	case <-time.After(100 * time.Millisecond):
	}
	require.NoError(t, hub.Close())
}

func TestCloseWaitsForSubscriber(t *testing.T) {
	for _, stale := range []bool{false, true} {
		name := "events"
		if stale {
			name = "stale"
		}
		t.Run(name, func(t *testing.T) {
			hub, err := NewHub(t.TempDir(), Options{DisableFSNotify: true, Debounce: time.Millisecond})
			require.NoError(t, err)
			hub.Start(context.Background())
			entered := make(chan struct{})
			release := make(chan struct{})
			finished := make(chan struct{})
			callback := func(ctx context.Context) {
				close(entered)
				<-ctx.Done()
				<-release
				close(finished)
			}
			hub.Subscribe("blocking", Filter{IncludeFiles: true}, func(ctx context.Context, _ []WatchEvent) { callback(ctx) }, func(ctx context.Context, _ StaleEvent) { callback(ctx) })
			if stale {
				go hub.EmitHintResync()
			} else {
				hub.EmitHintPaths([]string{"note.md"})
			}
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("callback did not start")
			}
			closed := make(chan error, 2)
			for range 2 {
				go func() { closed <- hub.Close() }()
			}
			select {
			case <-closed:
				t.Error("Close returned while subscriber was active")
			case <-time.After(20 * time.Millisecond):
			}
			close(release)
			for range 2 {
				select {
				case err := <-closed:
					require.NoError(t, err)
				case <-time.After(time.Second):
					t.Fatal("Close did not finish")
				}
			}
			select {
			case <-finished:
			default:
				t.Fatal("subscriber had not finished")
			}
		})
	}
}

func TestCloseBeforeStart(t *testing.T) {
	hub, err := NewHub(t.TempDir(), Options{ForceFSNotify: true})
	require.NoError(t, err)
	require.NoError(t, hub.Close())
	hub.Start(context.Background())
	require.NoError(t, hub.Close())
}

func TestStartAndCloseConcurrently(t *testing.T) {
	hub, err := NewHub(t.TempDir(), Options{ForceFSNotify: true})
	require.NoError(t, err)
	start := make(chan struct{})
	done := make(chan error, 3)
	for range 2 {
		go func() {
			<-start
			hub.Start(context.Background())
			done <- nil
		}()
	}
	go func() { <-start; done <- hub.Close() }()
	close(start)
	for range 3 {
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("concurrent start/close did not finish")
		}
	}
	require.NoError(t, hub.Close())
}

// Windows dispatches Add replies and watch events on the same native reader.
// Cancelling the event pump before admitted Add calls finish strands that reader.
func TestFSNotifyDrainsNativeEventsAfterHubCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	backend := &fsnotifyBackend{
		watcher: &fsnotify.Watcher{Events: make(chan fsnotify.Event), Errors: make(chan error)},
		events:  make(chan BackendEvent, 1), errors: make(chan error, 1),
	}
	require.NoError(t, backend.Start(ctx))
	defer func() { backend.cancel(); <-backend.loopDone }()
	select {
	case <-backend.loopDone:
		t.Fatal("native event pump stopped before backend close")
	case <-time.After(20 * time.Millisecond):
	}
	select {
	case backend.watcher.Events <- fsnotify.Event{Name: "note.md", Op: fsnotify.Write}:
	case <-time.After(time.Second):
		t.Fatal("native reader blocked after hub cancellation")
	}
	select {
	case event := <-backend.Events():
		require.Equal(t, "note.md", event.Path)
	case <-time.After(time.Second):
		t.Fatal("event was not drained")
	}
}

func TestCloseWaitsForWatchInstallation(t *testing.T) {
	root := t.TempDir()
	hub, err := NewHub(root, Options{DisableFSNotify: true})
	require.NoError(t, err)
	backend := newBlockingBackend(false)
	hub.setBackend(backend)
	hub.Start(context.Background())
	added := make(chan error, 1)
	go func() { added <- hub.AddRoot(root, RootOptions{Kind: RootNotes}) }()
	select {
	case <-backend.started:
	case <-time.After(time.Second):
		close(backend.release)
		t.Fatal("watch installation did not start")
	}
	closed := make(chan error, 1)
	go func() { closed <- hub.Close() }()
	<-hub.context().Done()
	select {
	case <-closed:
		t.Error("Close returned before watch installation finished")
	case <-time.After(20 * time.Millisecond):
	}
	close(backend.release)
	select {
	case err := <-added:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("watch installation did not finish")
	}
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after watch installation")
	}
}
