package indexcore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestResolveCodeRootsAcceptsAbsoluteVaultAndSymlinkAliases(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "vault")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0o755))
	alias := filepath.Join(parent, "alias")
	require.NoError(t, os.Symlink(root, alias))
	canonical := paths.ResolveSymlinks(root).String()
	roots, err := ResolveCodeRoots(canonical, []string{".", root, alias, filepath.Join(alias, "pkg")})
	require.NoError(t, err)
	require.Equal(t, []paths.AbsPath{paths.AbsPath(canonical), paths.AbsPath(filepath.ToSlash(filepath.Join(canonical, "pkg")))}, roots)
	_, err = ResolveCodeRoots(canonical, []string{parent})
	require.ErrorIs(t, err, paths.ErrOutsideVault)
}
