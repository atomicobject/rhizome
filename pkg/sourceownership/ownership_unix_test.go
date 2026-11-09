//go:build unix

package sourceownership

import (
	"github.com/atomicobject/rhizome/pkg/paths"
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

// SPEC-0136 R8: reads share one request's symlink resolutions, but write
// admission resolves fresh, so retargeting an allowed alias into an owned
// directory is still rejected even after a read cached the old target.
func TestWriteAdmissionResolvesRetargetedAliasFresh(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{".rhizome", "owned", "free"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0755))
	}
	alias := filepath.Join(root, "alias")
	require.NoError(t, os.Symlink(filepath.Join(root, "free"), alias))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome/source-ownership.yml"), []byte("sources:\n  - owner: external\n    reason: Use external authoring.\n    paths: [owned/**]\n"), 0644))
	policy := Load(root)
	reads := policy.ForReads(paths.NewSymlinkCache())
	require.Empty(t, reads.ReadOnlyReason("alias/new.md"))
	require.NoError(t, os.Remove(alias))
	require.NoError(t, os.Symlink(filepath.Join(root, "owned"), alias))
	require.Empty(t, reads.ReadOnlyReason("alias/new.md"), "a read request keeps its own resolutions")
	require.ErrorContains(t, policy.Check([]string{"alias/new.md"}), "Use external authoring.")
	require.ErrorContains(t, Check(root, []string{"alias/new.md"}), "Use external authoring.")
}
