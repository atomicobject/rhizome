package actions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenameHeadingUsesActualPathOnCaseInsensitiveFilesystem(t *testing.T) {
	for _, nested := range []bool{false, true} {
		for _, apply := range []bool{false, true} {
			name := map[bool]string{false: "leaf", true: "parent and leaf"}[nested] + "/" + map[bool]string{false: "preview", true: "apply"}[apply]
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				actual, requested, link := "Target.md", "target.md", "Target"
				if nested {
					actual, requested, link = "Notes/Target.md", "notes/target.md", "Notes/Target"
				}
				target := filepath.Join(root, actual)
				require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
				before := "# Old\n[[#Old]] [[" + link + "#Old|self]]\n"
				require.NoError(t, os.WriteFile(target, []byte(before), 0o644))
				actualInfo, err := os.Stat(target)
				require.NoError(t, err)
				requestedInfo, err := os.Stat(filepath.Join(root, requested))
				if os.IsNotExist(err) {
					t.Skip("case-sensitive filesystem")
				}
				require.NoError(t, err)
				require.True(t, os.SameFile(actualInfo, requestedInfo))
				ref := filepath.Join(root, "Ref.md")
				refBefore := "[[" + link + "#Old]] [[#Old]]\n"
				require.NoError(t, os.WriteFile(ref, []byte(refBefore), 0o644))

				result, err := RenameHeading(stubVault{path: root}, RenameHeadingParams{
					NoteMetadata: testNoteMetadataIndexer(t), Path: requested, OldHeading: "Old", NewHeading: "New", Apply: apply,
					UpgradeToBlockID: HeadingRenameUpgradeNever, Fallback: HeadingRenameFallbackHeading,
				})
				require.NoError(t, err)
				require.Equal(t, actual, result.Path)
				require.Equal(t, 3, result.MatchedReferences)
				require.Equal(t, 3, result.Rewritten)
				require.Equal(t, apply, result.Applied)
				wantTarget, wantRef := before, refBefore
				if apply {
					wantTarget = "# New\n[[#New]] [[" + link + "#New|self]]\n"
					wantRef = "[[" + link + "#New]] [[#Old]]\n"
				}
				for path, want := range map[string]string{target: wantTarget, ref: wantRef} {
					got, readErr := os.ReadFile(path)
					require.NoError(t, readErr)
					require.Equal(t, want, string(got))
				}
			})
		}
	}
}

func TestRenameHeadingKeepsDistinctCaseSensitiveNotes(t *testing.T) {
	for _, apply := range []bool{false, true} {
		t.Run(map[bool]string{false: "preview", true: "apply"}[apply], func(t *testing.T) {
			root := t.TempDir()
			upper := filepath.Join(root, "Target.md")
			upperBefore := "# Old\n[[#Old]] [[Target#Old]]\n"
			require.NoError(t, os.WriteFile(upper, []byte(upperBefore), 0o644))
			lower := filepath.Join(root, "target.md")
			_, err := os.Stat(lower)
			if err == nil {
				t.Skip("case-insensitive filesystem")
			}
			require.ErrorIs(t, err, os.ErrNotExist)
			lowerBefore := "# Old\n[[#Old]] [[target#Old]]\n"
			require.NoError(t, os.WriteFile(lower, []byte(lowerBefore), 0o644))
			ref := filepath.Join(root, "Ref.md")
			refBefore := "[[Target#Old]] [[target#Old]] [[#Old]]\n"
			require.NoError(t, os.WriteFile(ref, []byte(refBefore), 0o644))

			result, err := RenameHeading(stubVault{path: root}, RenameHeadingParams{
				NoteMetadata: testNoteMetadataIndexer(t), Path: "target.md", OldHeading: "Old", NewHeading: "New", Apply: apply,
				UpgradeToBlockID: HeadingRenameUpgradeNever, Fallback: HeadingRenameFallbackHeading,
			})
			require.NoError(t, err)
			require.Equal(t, "target.md", result.Path)
			require.Equal(t, 3, result.Rewritten)
			wantLower, wantRef := lowerBefore, refBefore
			if apply {
				wantLower = "# New\n[[#New]] [[target#New]]\n"
				wantRef = "[[Target#Old]] [[target#New]] [[#Old]]\n"
			}
			for path, want := range map[string]string{upper: upperBefore, lower: wantLower, ref: wantRef} {
				got, readErr := os.ReadFile(path)
				require.NoError(t, readErr)
				require.Equal(t, want, string(got))
			}
		})
	}
}
