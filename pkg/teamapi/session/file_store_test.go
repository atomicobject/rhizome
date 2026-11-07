package session

import (
	"io"
	"testing"

	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/stretchr/testify/require"
)

func TestFileStoreReplacementPreservesReaderSnapshot(t *testing.T) {
	store := fileStore{dir: t.TempDir()}
	const origin = "https://team.example.invalid"
	require.NoError(t, store.Save(origin, []byte("synthetic old session")))
	reader, err := fileio.OpenRead(store.path(origin))
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })

	// A refresh publisher must work while another process is still reading.
	require.NoError(t, store.Save(origin, []byte("synthetic rotated session")))
	old, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, "synthetic old session", string(old))
	current, err := store.Load(origin)
	require.NoError(t, err)
	require.Equal(t, "synthetic rotated session", string(current))

	require.NoError(t, store.Delete(origin))
	_, err = store.Load(origin)
	require.ErrorIs(t, err, ErrNotFound)
}
