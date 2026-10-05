package namespacegit_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWindowsInventoryRefreshesCachedDirectoryMetadata(t *testing.T) {
	root := canonicalTemp(t)
	dir := filepath.Join(root, "directory")
	require.NoError(t, os.Mkdir(dir, 0o700))
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	old, err := entries[0].Info()
	require.NoError(t, err)
	stamp := old.ModTime().Add(-time.Hour)
	require.NoError(t, os.Chtimes(dir, stamp, stamp))
	cached, err := entries[0].Info()
	require.NoError(t, err)
	fresh, err := os.Lstat(dir)
	require.NoError(t, err)
	require.Equal(t, old.ModTime(), cached.ModTime(), "Windows DirEntry.Info retains enumeration metadata")
	require.NotEqual(t, cached.ModTime(), fresh.ModTime(), "positive control must distinguish stale enumeration from fresh state")
	require.Equal(t, fresh.ModTime(), inventory(t, root)["directory"].Time)
}
