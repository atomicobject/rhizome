package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// Web reads take note bytes from the vault cache, which otherwise refreshes
// only on watch events. A batch can publish a rewrite whose event has not
// arrived yet (here a resync; in production also a late FSEvents delivery),
// and node.changed must not announce a state the cache cannot serve.
func TestLiveOwnership_PublishedRewriteRefreshesCacheBeforeItsWatchEvent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(path, []byte("# first\n"), 0o644))
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
	defer store.Close()
	defer cacheService.Close()
	cacheService.MarkDirty("note.md", cache.DirtyModified)
	runOwnershipBatch(t, w)

	rewrite := "# second\n\nlonger body\n"
	require.NoError(t, os.WriteFile(path, []byte(rewrite), 0o644))
	later := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, later, later))
	w.retainWatchEvents(nil, true)
	runOwnershipBatch(t, w)

	rows, err := store.CurrentNoteMetadataRowsByPaths(context.Background(), []string{"note.md"})
	require.NoError(t, err)
	require.Equal(t, int64(len(rewrite)), rows["note.md"].Size, "the resync must publish the rewrite")
	entry, ok := cacheService.Entry("note.md")
	require.True(t, ok)
	require.Equal(t, rewrite, entry.Content, "reads must serve the bytes the batch published")
}
