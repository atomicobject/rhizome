package cache

import (
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
	"github.com/stretchr/testify/require"
)

func TestHandleCacheEvent_RemoveWriteDirectoryMarksRemoved(t *testing.T) {
	tmp := t.TempDir()

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })

	// Simulate an existing dirty marker from a child remove.
	svc.MarkDirty("subdir", DirtyModified)

	handleCacheEvent(svc, watchhub.WatchEvent{
		RelPath: "subdir",
		Op:      watchhub.OpRemove | watchhub.OpWrite,
		IsDir:   true,
	})

	dirty := svc.DirtySnapshot()
	require.Equal(t, DirtyRemoved, dirty[string(filepath.ToSlash("subdir"))])
}

func TestHandleCacheEvent_RemoveCreateMarksRecreated(t *testing.T) {
	tmp := t.TempDir()

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })

	handleCacheEvent(svc, watchhub.WatchEvent{
		RelPath: "note.md",
		Op:      watchhub.OpRemove | watchhub.OpCreate,
		IsDir:   false,
	})

	dirty := svc.DirtySnapshot()
	require.Equal(t, DirtyRecreated, dirty["note.md"])
}
