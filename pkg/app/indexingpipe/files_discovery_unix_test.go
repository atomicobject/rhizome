//go:build !windows

package indexingpipe

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/stretchr/testify/require"
)

func TestFileDiscovery_ReturnsDirectoryErrorAfterWorkerDrain(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.md"), "readable")
	locked := filepath.Join(root, "z-locked")
	require.NoError(t, os.Mkdir(locked, 0o755))
	require.NoError(t, os.Chmod(locked, 0))
	t.Cleanup(func() { require.NoError(t, os.Chmod(locked, 0o755)) })
	_, err := os.ReadDir(locked)
	if err == nil {
		t.Skip("permission restriction is ineffective for this process")
	}
	require.ErrorIs(t, err, os.ErrPermission)
	classify := func(path string, _ os.DirEntry, modTime int64) (FileCandidate, bool, error) {
		return FileCandidate{AbsPath: path, RelPath: "a.md", ModTime: modTime, Kind: FileKindNote}, true, nil
	}
	opts := ProcessOptions{Root: root, WorkerCount: 1, QueueCapacity: 1}
	t.Run("count", func(t *testing.T) {
		count, err := CountFiles(context.Background(), opts, classify)
		require.ErrorIs(t, err, os.ErrPermission)
		require.Zero(t, count, "an incomplete count is not a progress total")
	})
	started := make(chan struct{})
	release := make(chan struct{})
	completed := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- ProcessFiles(context.Background(), opts, classify, nil, nil, func(FilePayload) error {
			close(started)
			<-release
			close(completed)
			return nil
		})
	}()
	<-started
	close(release)
	require.ErrorIs(t, <-result, os.ErrPermission)
	select {
	case <-completed:
	default:
		t.Fatal("accepted worker must finish before discovery returns")
	}
}

func TestFileDiscovery_PrunesUnreadableExcludedDirectories(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "visible.md"), "readable")
	for _, name := range []string{".hidden", "vendor", "ignored"} {
		dir := filepath.Join(root, name)
		require.NoError(t, os.Mkdir(dir, 0o755))
		require.NoError(t, os.Chmod(dir, 0))
		t.Cleanup(func() { require.NoError(t, os.Chmod(dir, 0o755)) })
		_, err := os.ReadDir(dir)
		if err == nil {
			t.Skip("permission restriction is ineffective for this process")
		}
		require.ErrorIs(t, err, os.ErrPermission)
	}
	opts := ProcessOptions{Root: root, Matcher: ignore.NewMatcher([]string{"ignored/**"}), WorkerCount: 1}
	classify := func(path string, _ os.DirEntry, modTime int64) (FileCandidate, bool, error) {
		return FileCandidate{AbsPath: path, RelPath: "visible.md", ModTime: modTime, Kind: FileKindNote}, true, nil
	}
	var processed []string
	require.NoError(t, ProcessFiles(context.Background(), opts, classify, nil, nil, func(payload FilePayload) error {
		processed = append(processed, payload.Candidate.RelPath)
		return nil
	}))
	require.Equal(t, []string{"visible.md"}, processed)
	count, err := CountFiles(context.Background(), opts, classify)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}
