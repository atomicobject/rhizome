package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/stretchr/testify/require"
)

// A client polling runtime.json must never make the owner's publication fail.
func TestManifestReplaceSucceedsWithOpenReader(t *testing.T) {
	for _, tc := range []struct{ name, vault string }{
		{"short path", t.TempDir()},
		{"long path", filepath.Join(t.TempDir(), strings.Repeat("nested", 20), strings.Repeat("vault", 24))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest := testManifest(t, tc.vault, os.Getpid())
			require.NoError(t, WriteManifest(tc.vault, manifest))
			reader, err := fileio.OpenRead(ManifestPath(tc.vault))
			require.NoError(t, err)
			t.Cleanup(func() { _ = reader.Close() })

			updated := manifest
			updated.Ready = !manifest.Ready
			updated.HTTPPort = manifest.HTTPPort + 1
			require.NoError(t, WriteManifest(tc.vault, updated), "an open reader must not block replacement")

			got, err := ReadManifest(tc.vault)
			require.NoError(t, err)
			require.Equal(t, updated.Ready, got.Ready)
			require.Equal(t, updated.HTTPPort, got.HTTPPort)
			require.NoError(t, reader.Close())
		})
	}
}

// A registry listing (rzm stop --all, PruneStale) must not block serve's
// startup or heartbeat registry write.
func TestRegistryWriteReplacesUnderOpenSharedReader(t *testing.T) {
	registry := &Registry{dir: t.TempDir()}
	manifest := testManifest(t, t.TempDir(), os.Getpid())
	require.NoError(t, registry.Write(manifest))
	reader, err := fileio.OpenRead(registry.Path(manifest.InstanceID))
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })

	updated := manifest
	updated.HTTPPort = manifest.HTTPPort + 1
	require.NoError(t, registry.Write(updated), "an open registry reader must not block replacement")

	listed, err := registry.List()
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, updated.HTTPPort, listed[0].HTTPPort)
	require.NoError(t, reader.Close())
}
