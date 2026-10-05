package fileio

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadFilePreservesBytesAndMissingError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	_, err := ReadFile(path)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.True(t, os.IsNotExist(err))
	var pathErr *os.PathError
	require.ErrorAs(t, err, &pathErr)
	require.Equal(t, "open", pathErr.Op)
	require.Equal(t, path, pathErr.Path)

	want := []byte("# unchanged source\nnotes: {}\n")
	require.NoError(t, os.WriteFile(path, want, 0o600))
	got, err := ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, want, got)
}
