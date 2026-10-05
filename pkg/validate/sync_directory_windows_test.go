//go:build windows

package validate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSyncDirectoryAcceptsWindowsDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "journal")
	require.NoError(t, os.Mkdir(dir, 0o700))
	require.NoError(t, syncDirectory(dir))
	require.Error(t, syncDirectory(filepath.Join(dir, "missing")))
}
