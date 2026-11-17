package obsidian

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCollectionIncludesFollowTheRootFilesystemCase(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"Docs/Guide.md", ".kb/curation/Review.md", "Other/Note.md"} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte("# note\n"), 0o644))
	}
	actual, err := os.Stat(filepath.Join(root, "Docs"))
	require.NoError(t, err)
	alias, err := os.Stat(filepath.Join(root, "docs"))
	fold := err == nil && os.SameFile(actual, alias)
	t.Logf("vault filesystem folds case: %v", fold)

	cfg := VaultDefinition{Name: "case", Root: root, Includes: []string{"docs/**/*.md", ".KB/curation/*.md"}}
	require.Equal(t, fold, NotePathMatchesIncludes(cfg, "Docs/Guide.md"), "include spelled docs/ against the Docs/ entry")
	require.Equal(t, fold, NotePathMatchesIncludes(cfg, ".kb/curation/Review.md"), "a hidden parent still needs an explicit include, compared under the same policy")
	require.False(t, NotePathMatchesIncludes(cfg, "Other/Note.md"), "different letters never match")

	files, err := DiscoverFiles(cfg)
	require.NoError(t, err)
	if fold {
		require.Equal(t, []string{".kb/curation/Review.md", "Docs/Guide.md"}, files, "discovery keeps the directory entries' spelling")
	} else {
		require.Empty(t, files, "exact: differently cased includes select nothing")
	}
}
