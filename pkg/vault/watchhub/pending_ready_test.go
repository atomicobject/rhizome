package watchhub

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPendingDirectoryReadinessSurvivesQueueHandoff(t *testing.T) {
	root := normalizePath(t.TempDir())
	incoming := filepath.Join(root, "incoming")
	nested := filepath.Join(incoming, "nested")
	hub, err := NewHub(root, Options{DisableFSNotify: true, PendingAddBuffer: 1})
	require.NoError(t, err)
	backend := newDirectoryInstallBackend()
	hub.setBackend(backend)
	t.Cleanup(func() { require.NoError(t, hub.Close()) })
	require.NoError(t, hub.AddRoot(root, RootOptions{Kind: RootNotes}))
	require.NoError(t, os.MkdirAll(nested, 0o755))
	stale := make(chan StaleEvent, 2)
	hub.Subscribe("handoff", Filter{}, nil, func(_ context.Context, event StaleEvent) { stale <- event })
	hub.handleBackendEvent(BackendEvent{Path: incoming, Op: OpCreate, IsDir: true, IsDirKnown: true})
	rejected := filepath.Join(root, "rejected")
	require.NoError(t, os.MkdirAll(rejected, 0o755))
	hub.handleBackendEvent(BackendEvent{Path: rejected, Op: OpCreate, IsDir: true, IsDirKnown: true})
	require.Len(t, stale, 1)
	require.Equal(t, StaleOverflow, (<-stale).Reason)

	path := <-hub.pendingAdd
	require.Error(t, hub.WaitForReady(context.Background(), 10*time.Millisecond))
	require.ElementsMatch(t, []string{root}, backend.WatchList())
	hub.installPendingDirectory(path)
	hub.finishPendingAdd()
	require.NoError(t, hub.WaitForReady(context.Background(), time.Second))
	require.ElementsMatch(t, []string{root, incoming, nested}, backend.WatchList())
}
