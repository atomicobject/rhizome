package watchhub

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWatchHubMovedDirectoryEmitsNestedEdits(t *testing.T) {
	fixture := t.TempDir()
	root := filepath.Join(fixture, "vault")
	source := filepath.Join(fixture, "incoming")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(source, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(source, "nested", "note.md"), []byte("before"), 0o644))

	hub, err := NewHub(root, Options{ForceFSNotify: true, Debounce: time.Millisecond})
	require.NoError(t, err)
	hub.Start(context.Background())
	t.Cleanup(func() { require.NoError(t, hub.Close()) })
	require.NoError(t, hub.AddRoot(root, RootOptions{Kind: RootNotes}))
	waitForBackendReady(t, hub, 5*time.Second)

	events := make(chan WatchEvent, 64)
	stale := make(chan StaleEvent, 64)
	hub.Subscribe("moved-tree", Filter{IncludeFiles: true, IncludeDirs: true}, func(_ context.Context, batch []WatchEvent) {
		for _, event := range batch {
			events <- event
		}
	}, func(_ context.Context, event StaleEvent) { stale <- event })

	destination := filepath.Join(root, "incoming")
	require.NoError(t, os.Rename(source, destination))
	select {
	case installed := <-stale:
		require.Equal(t, StaleDirCreated, installed.Reason)
		require.Equal(t, SourceFSNotify, installed.Source)
		require.Equal(t, normalizePath(destination), normalizePath(installed.Path))
	case <-time.After(5 * time.Second):
		t.Fatal("moved-tree installation did not request resync")
	}
	waitForRelPath(t, events, stale, "incoming", 5*time.Second)
	backend := waitForBackendReady(t, hub, 5*time.Second)
	assertWatchlistContains(t, backend.WatchList(), root)
	assertWatchlistContains(t, backend.WatchList(), destination)
	assertWatchlistContains(t, backend.WatchList(), filepath.Join(destination, "nested"))

	require.NoError(t, os.WriteFile(filepath.Join(destination, "direct.md"), []byte("control"), 0o644))
	waitForRelPath(t, events, stale, "incoming/direct.md", 5*time.Second)
	t.Log("direct-file positive control received")
	require.NoError(t, os.WriteFile(filepath.Join(destination, "nested", "note.md"), []byte("after"), 0o644))
	waitForRelPath(t, events, stale, "incoming/nested/note.md", 5*time.Second)
}
