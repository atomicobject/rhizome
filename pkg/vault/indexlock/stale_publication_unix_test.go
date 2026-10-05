//go:build !windows

package indexlock

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/stretchr/testify/require"
)

func TestStaleTakeoverPreparationFailurePreservesMetadata(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	path, old := writeStalePublicationFixture(t, t.TempDir())
	// The guarded stale recheck can still read the old owner and open its
	// persistent guard while the parent forbids replacement preparation.
	require.NoError(t, os.WriteFile(path+".guard.lock", nil, 0o600))
	dir := filepath.Dir(path)
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { require.NoError(t, os.Chmod(dir, 0o700)) })
	release, acquired, err := TryAcquire(path)
	if release != nil {
		t.Cleanup(func() { require.NoError(t, release()) })
	}
	require.Error(t, err)
	require.False(t, acquired)
	var pathErr *os.PathError
	require.ErrorAs(t, err, &pathErr)
	require.Equal(t, "open", pathErr.Op, "failure must occur while preparing the replacement")
	raw, err := fileio.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, old, raw)
	requireNoPublicationTemps(t, path)

	require.NoError(t, os.Chmod(dir, 0o700))
	release, acquired, err = TryAcquire(path)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { require.NoError(t, release()) })
}
