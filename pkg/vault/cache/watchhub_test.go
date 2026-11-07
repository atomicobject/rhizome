package cache

import (
	"context"
	"os"
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

func TestHandleCacheEvent_RecreateAfterPendingRemoveStaysRecreated(t *testing.T) {
	svc, err := NewService(t.TempDir(), Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })

	handleCacheEvent(svc, watchhub.WatchEvent{RelPath: "note.md", Op: watchhub.OpRemove})
	handleCacheEvent(svc, watchhub.WatchEvent{RelPath: "note.md", Op: watchhub.OpRemove | watchhub.OpCreate})

	require.Equal(t, DirtyRecreated, svc.DirtySnapshot()["note.md"])
}

func TestRefresh_OutOfOrderRequeueKeepsRecreatedNote(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "note.md"), []byte("#kept"), 0o644))
	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	ctx := context.Background()
	require.NoError(t, svc.EnsureReady(ctx))

	// Two cancelled refreshes requeue out of order: the older removal lands on
	// top of the later recreation, though the note is back on disk.
	svc.requeueDirty(map[string]DirtyKind{"note.md": DirtyRemoved})
	svc.requeueDirty(map[string]DirtyKind{"note.md": DirtyCreated})
	require.Equal(t, DirtyRemoved, svc.DirtySnapshot()["note.md"])

	require.NoError(t, svc.Refresh(ctx))
	_, ok := svc.Entry("note.md")
	require.True(t, ok, "a removal must not delete a note that exists on disk")
}

func TestRefresh_RemovalOfOldCaseSpellingStaysRemoved(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "Note.md"), []byte("#old"), 0o644))
	if _, err := os.Stat(filepath.Join(tmp, "note.md")); err != nil {
		t.Skip("needs a case-insensitive filesystem")
	}
	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	ctx := context.Background()
	require.NoError(t, svc.EnsureReady(ctx))

	require.NoError(t, os.Remove(filepath.Join(tmp, "Note.md")))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "note.md"), []byte("#new"), 0o644))
	svc.requeueDirty(map[string]DirtyKind{"Note.md": DirtyRemoved, "note.md": DirtyCreated})

	require.NoError(t, svc.Refresh(ctx))
	_, oldKept := svc.Entry("Note.md")
	_, newCached := svc.Entry("note.md")
	require.False(t, oldKept, "the deleted spelling must not come back")
	require.True(t, newCached)
}
