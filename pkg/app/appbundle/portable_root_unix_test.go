//go:build !windows

package appbundle

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPortableEntryUsesOSAccountRatherThanRepositoryHome(t *testing.T) {
	home, err := nativeAccountHome()
	require.NoError(t, err)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USER", "synthetic-repository-user")
	entry, err := EntryPath()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".local", "bin", "rzm"), entry)
}
