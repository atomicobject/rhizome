package obsidian

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// SPEC-0064 scanner agreement: markdown discovery honors include boundaries
// the same way the unified matcher does.
func TestDiscoverFiles_IncludedSubtree(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	write(".gitignore", "app/\n")
	write("app/.gitignore", "drafts/\n")
	write("app/docs/readme.md", "# readme\n")
	write("app/drafts/wip.md", "# wip\n")
	write("top.md", "# top\n")
	write(".rhizome/ignore", "# rhizome: included subtrees\n!/app/\n")

	files, err := DiscoverFiles(VaultDefinition{Path: root})
	require.NoError(t, err)

	rel := make(map[string]bool, len(files))
	for _, f := range files {
		rel[filepath.ToSlash(f)] = true
	}
	require.True(t, rel["top.md"], "got %v", files)
	require.True(t, rel["app/docs/readme.md"], "boundary-included notes must be discovered; got %v", files)
	require.False(t, rel["app/drafts/wip.md"], "subtree's own .gitignore must keep applying; got %v", files)
}
