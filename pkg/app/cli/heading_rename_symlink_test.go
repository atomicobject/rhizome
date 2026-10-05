package actions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenameHeadingRefusesFileSymlinkAlias(t *testing.T) {
	for _, apply := range []bool{false, true} {
		t.Run(map[bool]string{false: "preview", true: "apply"}[apply], func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "Target.md")
			ref := filepath.Join(root, "Ref.md")
			require.NoError(t, os.WriteFile(target, []byte("# Old\n"), 0o644))
			require.NoError(t, os.WriteFile(ref, []byte("[[Alias#Old]]\n"), 0o644))
			alias := filepath.Join(root, "Alias.md")
			if err := os.Symlink(target, alias); err != nil {
				t.Skipf("file symlink unavailable: %v", err)
			}
			result, err := RenameHeading(stubVault{path: root}, RenameHeadingParams{
				NoteMetadata: testNoteMetadataIndexer(t), Path: "Alias.md", OldHeading: "Old", NewHeading: "New", Apply: apply,
				UpgradeToBlockID: HeadingRenameUpgradeNever, Fallback: HeadingRenameFallbackHeading,
			})
			require.ErrorContains(t, err, "unsupported symbolic-link alias")
			require.False(t, result.Applied)
			for path, want := range map[string]string{target: "# Old\n", ref: "[[Alias#Old]]\n"} {
				got, readErr := os.ReadFile(path)
				require.NoError(t, readErr)
				require.Equal(t, want, string(got))
			}
			info, err := os.Lstat(alias)
			require.NoError(t, err)
			require.NotZero(t, info.Mode()&os.ModeSymlink)
			link, err := os.Readlink(alias)
			require.NoError(t, err)
			require.Equal(t, target, link)
		})
	}
}
