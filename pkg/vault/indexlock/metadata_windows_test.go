package indexlock

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/stretchr/testify/require"
)

func TestPriorityCleanupWithOpenMetadataReader(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "index.lock")
	cleanup, err := RequestPriority(lockPath)
	require.NoError(t, err)
	t.Cleanup(cleanup.Close)
	requestPath := onlyPriorityRequestPath(t, lockPath)
	reader, err := fileio.OpenRead(requestPath)
	require.NoError(t, err)
	defer reader.Close()

	// Exercise the production owner check and removal while a poller holds a handle.
	cleanup.Close()
	require.NoError(t, reader.Close())
	_, err = os.Stat(requestPath)
	require.True(t, os.IsNotExist(err), "priority request must clear after cleanup: %v", err)
}

func TestLockReleaseWithOpenMetadataReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.lock")
	release, acquired, err := TryAcquire(path)
	require.NoError(t, err)
	require.True(t, acquired)
	reader, err := fileio.OpenRead(path)
	require.NoError(t, err)
	defer reader.Close()
	require.NoError(t, release())
	require.NoError(t, reader.Close())
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err), "lock must clear after cleanup: %v", err)
}

func TestMetadataCleanupAtLongPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), strings.Repeat("nested", 20), strings.Repeat("vault", 24))
	path := filepath.Join(dir, "index.lock")
	require.Greater(t, len(path), 260)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	release, acquired, err := TryAcquire(path)
	require.NoError(t, err)
	require.True(t, acquired)
	data, ok := ReadLockData(path)
	require.True(t, ok)
	require.Equal(t, os.Getpid(), data.PID)
	require.NoError(t, release())
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err))
	cleanup, err := RequestPriority(path)
	require.NoError(t, err)
	cleanup.Close()
	require.False(t, CheckPriority(path))
}
