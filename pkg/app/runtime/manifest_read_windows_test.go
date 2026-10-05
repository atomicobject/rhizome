package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestManifestOwnerCleanupWithOpenReader(t *testing.T) {
	for _, tc := range []struct {
		name          string
		open          func(string) (*os.File, error)
		blocksCleanup bool
	}{
		{"ordinary reader blocks deletion", os.Open, true},
		{"manifest reader allows deletion", fileio.OpenRead, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vault := t.TempDir()
			manifest := testManifest(t, vault, os.Getpid())
			require.NoError(t, WriteManifest(vault, manifest))
			reader, err := tc.open(ManifestPath(vault))
			require.NoError(t, err)
			t.Cleanup(func() { _ = reader.Close() })

			// Hold the reader open to reproduce the polling/shutdown overlap without timing.
			err = RemoveManifestIfOwner(vault, manifest.PID)
			if tc.blocksCleanup {
				require.ErrorIs(t, err, windows.ERROR_SHARING_VIOLATION)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, reader.Close())
			if tc.blocksCleanup {
				require.NoError(t, RemoveManifestIfOwner(vault, manifest.PID))
			}
			_, err = ReadManifest(vault)
			require.True(t, os.IsNotExist(err), "the owner's manifest must be gone after its readers close")
			require.NoError(t, RemoveManifestIfOwner(vault, manifest.PID), "cleanup stays idempotent")
		})
	}
}

func TestManifestLifecycleAtLongPath(t *testing.T) {
	vault := filepath.Join(t.TempDir(), strings.Repeat("nested", 20), strings.Repeat("vault", 24))
	require.Greater(t, len(ManifestPath(vault)), 260)
	manifest := testManifest(t, vault, os.Getpid())
	require.NoError(t, WriteManifest(vault, manifest))
	got, err := ReadManifest(vault)
	require.NoError(t, err)
	require.Equal(t, manifest.RunID, got.RunID)
	require.NoError(t, RemoveManifestIfOwner(vault, manifest.PID))
	_, err = ReadManifest(vault)
	require.True(t, os.IsNotExist(err))
}

func TestManifestRewritePreservesPublishedManifestWithOpenReader(t *testing.T) {
	vault := t.TempDir()
	manifest := testManifest(t, vault, os.Getpid())
	require.NoError(t, WriteManifest(vault, manifest))
	reader, err := fileio.OpenRead(ManifestPath(vault))
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })

	updated := manifest
	updated.Ready = !manifest.Ready
	writeErr := WriteManifest(vault, updated)
	require.NoError(t, reader.Close())
	got, err := ReadManifest(vault)
	require.NoError(t, err, "a failed replacement must retain the published manifest")
	require.Equal(t, manifest.RunID, got.RunID)
	if writeErr != nil {
		require.Equal(t, manifest.Ready, got.Ready)
	} else {
		require.Equal(t, updated.Ready, got.Ready)
	}
	require.NoError(t, WriteManifest(vault, updated), "replacement succeeds after the reader closes")
}
