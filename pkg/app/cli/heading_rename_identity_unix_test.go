//go:build !windows

package actions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenameHeadingUsesCanonicalParentAlias(t *testing.T) {
	for _, apply := range []bool{false, true} {
		t.Run(map[bool]string{false: "preview", true: "apply"}[apply], func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.Mkdir(filepath.Join(root, "notes"), 0o755))
			require.NoError(t, os.Symlink(filepath.Join(root, "notes"), filepath.Join(root, "alias")))
			target := filepath.Join(root, "notes/Target.md")
			before := "# Old\n[[#Old]] [[notes/Target#Old]]\n"
			require.NoError(t, os.WriteFile(target, []byte(before), 0o644))
			ref := filepath.Join(root, "Ref.md")
			require.NoError(t, os.WriteFile(ref, []byte("[[notes/Target#Old]] [[#Old]]\n"), 0o644))
			result, err := RenameHeading(stubVault{path: root}, RenameHeadingParams{
				NoteMetadata: testNoteMetadataIndexer(t), Path: "alias/Target.md", OldHeading: "Old", NewHeading: "New", Apply: apply,
				UpgradeToBlockID: HeadingRenameUpgradeNever, Fallback: HeadingRenameFallbackHeading,
			})
			require.NoError(t, err)
			require.Equal(t, "notes/Target.md", result.Path)
			require.Equal(t, 3, result.Rewritten)
			wantTarget, wantRef := before, "[[notes/Target#Old]] [[#Old]]\n"
			if apply {
				wantTarget = "# New\n[[#New]] [[notes/Target#New]]\n"
				wantRef = "[[notes/Target#New]] [[#Old]]\n"
			}
			for path, want := range map[string]string{target: wantTarget, ref: wantRef} {
				got, readErr := os.ReadFile(path)
				require.NoError(t, readErr)
				require.Equal(t, want, string(got))
			}
		})
	}
}
