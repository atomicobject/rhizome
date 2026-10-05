package indexlock

import (
	"os"
	"testing"

	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/stretchr/testify/require"
)

func TestStaleTakeoverWithNoncooperatingReader(t *testing.T) {
	path, old := writeStalePublicationFixture(t, t.TempDir())
	// Ordinary Windows readers do not share deletion. The publisher must
	// report that failure rather than deleting or hiding the old metadata.
	reader, err := os.Open(path)
	require.NoError(t, err)
	defer reader.Close()
	release, acquired, err := TryAcquire(path)
	if release != nil {
		t.Cleanup(func() { require.NoError(t, release()) })
	}
	require.Error(t, err)
	require.False(t, acquired)
	raw, readErr := fileio.ReadFile(path)
	require.NoError(t, readErr)
	require.Equal(t, old, raw)
	requireNoPublicationTemps(t, path)

	require.NoError(t, reader.Close())
	release, acquired, err = TryAcquire(path)
	require.NoError(t, err)
	require.True(t, acquired, "closing the blocking reader must permit retry")
	t.Cleanup(func() { require.NoError(t, release()) })
}
