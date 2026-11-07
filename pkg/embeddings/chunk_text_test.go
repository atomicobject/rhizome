package embeddings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChunkTextForContentMatchesPathChunking(t *testing.T) {
	root := t.TempDir()
	relPath := "docs/example.md"
	content := []byte("# One\nfirst body\n\n## Two\nsecond body\n")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, relPath), content, 0o644))

	for idx, want := range []string{
		"Title: example\nPath: docs/example.md\nHeadings: example > One\n\nfirst body",
		"Title: example\nPath: docs/example.md\nHeadings: example > One > Two\n\nsecond body",
	} {
		require.Equal(t, want, ChunkTextForPath(root, relPath, idx, nil))
		require.Equal(t, want, ChunkTextForContent(relPath, idx, content))
	}
	require.Empty(t, ChunkTextForContent(relPath, 99, content))
}

// Search hits carry the section's position in the index's order, so display
// must skip frontmatter and indented code the way sectionparse does.
func TestChunkTextForContentUsesIndexSectionOrder(t *testing.T) {
	content := []byte("---\n# draft\ntags: [a]\n---\n# Intro\nintro body\n\n    # install deps\n\n## Details\ndetails body\n")
	require.Equal(t, "Title: example\nPath: docs/example.md\nHeadings: example > Intro\n\nintro body\n\n    # install deps", ChunkTextForContent("docs/example.md", 0, content))
	require.Equal(t, "Title: example\nPath: docs/example.md\nHeadings: example > Intro > Details\n\ndetails body", ChunkTextForContent("docs/example.md", 1, content))
	require.Empty(t, ChunkTextForContent("docs/example.md", 2, content))
}
