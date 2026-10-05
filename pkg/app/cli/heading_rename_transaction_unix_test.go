//go:build !windows

package actions

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenameHeadingParentFailurePreservesEverySourceAndCanRetry(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires filesystem permission enforcement for a non-root user")
	}
	root := t.TempDir()
	before := map[string]string{"target.md": "# Target\n\n## Old Heading\n", "aa-good.md": "[[target#Old Heading]]\n", "zz-locked/ref.md": "[[target#Old Heading|label]]\n"}
	for path, content := range before {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(content), 0o644))
	}
	locked := filepath.Join(root, "zz-locked")
	ref := filepath.Join(locked, "ref.md")
	require.NoError(t, os.Chmod(ref, 0o444))
	require.NoError(t, os.Chmod(locked, 0o555))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755); _ = os.Chmod(ref, 0o644) })
	params := RenameHeadingParams{Context: context.Background(), NoteMetadata: testNoteMetadataIndexer(t), Path: "target.md", OldHeading: "Old Heading", NewHeading: "New Heading", Apply: true, UpgradeToBlockID: HeadingRenameUpgradeNever, Fallback: HeadingRenameFallbackHeading}
	result, err := RenameHeading(stubVault{path: root}, params)
	require.ErrorIs(t, err, os.ErrPermission)
	require.False(t, result.Applied)
	require.Equal(t, 2, result.Rewritten, "counts retain their preview meaning on publication failure")
	for path, want := range before {
		got, readErr := os.ReadFile(filepath.Join(root, path))
		require.NoError(t, readErr)
		require.Equal(t, want, string(got), path)
	}
	require.NoError(t, os.Chmod(locked, 0o755))
	require.NoError(t, os.Chmod(ref, 0o644))
	result, err = RenameHeading(stubVault{path: root}, params)
	require.NoError(t, err)
	require.True(t, result.Applied)
	for path, want := range map[string]string{"target.md": "# Target\n\n## New Heading\n", "aa-good.md": "[[target#New Heading]]\n", "zz-locked/ref.md": "[[target#New Heading|label]]\n"} {
		got, readErr := os.ReadFile(filepath.Join(root, path))
		require.NoError(t, readErr)
		require.Equal(t, want, string(got), path)
	}
}

func TestRenameHeadingUnreadablePotentialBacklinkStopsApply(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires filesystem permission enforcement for a non-root user")
	}
	root := t.TempDir()
	before := map[string]string{"target.md": "# Target\n\n## Old Heading\n", "aa-good.md": "[[target#Old Heading]]\n", "zz-unreadable.md": "[[target#Old Heading]]\n"}
	for path, content := range before {
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(content), 0o644))
	}
	unreadable := filepath.Join(root, "zz-unreadable.md")
	require.NoError(t, os.Chmod(unreadable, 0))
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o644) })
	result, err := RenameHeading(stubVault{path: root}, RenameHeadingParams{NoteMetadata: testNoteMetadataIndexer(t), Path: "target.md", OldHeading: "Old Heading", NewHeading: "New Heading", Apply: true, UpgradeToBlockID: HeadingRenameUpgradeNever, Fallback: HeadingRenameFallbackHeading})
	require.ErrorIs(t, err, os.ErrPermission)
	require.False(t, result.Applied)
	require.NoError(t, os.Chmod(unreadable, 0o644))
	for path, want := range before {
		got, readErr := os.ReadFile(filepath.Join(root, path))
		require.NoError(t, readErr)
		require.Equal(t, want, string(got), path)
	}
}
