//go:build unix

package sourceownership

import (
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestOwnershipProtectsNewFilesThroughDirectoryAliases(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "owned"), 0755))
	require.NoError(t, os.Symlink(filepath.Join(root, "owned"), filepath.Join(root, "alias")))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome/source-ownership.yml"), []byte("sources:\n  - owner: external\n    reason: Use external authoring.\n    paths: [owned/**]\n"), 0644))
	require.ErrorContains(t, Check(root, []string{"alias/new.md"}), "Use external authoring.")
	require.NoFileExists(t, filepath.Join(root, "owned/new.md"))
}
