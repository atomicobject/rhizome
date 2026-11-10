//go:build darwin && !fsnotify_fallback

package watchhub

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsevents"
	"github.com/stretchr/testify/require"
)

// FSEvents watches recursively, so a nested file arrives as one event on the
// root's stream. Feed the loop a batch directly: delivery timing belongs to
// fseventsd, which no test can bound under load.
func TestFSEventsBackend_TranslatesNestedFileEvent(t *testing.T) {
	backend, err := newFSEventsBackend(8, func(string) bool { return false })
	require.NoError(t, err)
	b := backend.(*fseventsBackend)
	b.ctx, b.cancel = context.WithCancel(t.Context())
	t.Cleanup(func() { _ = b.Close() })
	go b.loop()

	batches := make(chan []fsevents.Event, 1)
	b.publishStream(&fsevents.EventStream{Events: batches})
	target := filepath.Join(t.TempDir(), "a", "b", "note.md")
	batches <- []fsevents.Event{{Path: target, Flags: fsevents.ItemCreated | fsevents.ItemModified | fsevents.ItemIsFile}}

	select {
	case ev := <-b.Events():
		require.Equal(t, BackendEvent{Path: target, Op: OpCreate | OpWrite, IsDir: false, IsDirKnown: true}, ev)
	case <-time.After(5 * time.Second):
		t.Fatal("nested FSEvents file event was not translated")
	}
}
