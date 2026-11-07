package obsidian

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiscoverFilesExplicitHiddenSelection(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"notes/team.md", ".kb/curation/_conflicts.md", ".kb/curation/_check_first.md", ".other/private.md"} {
		mkfile(t, filepath.Join(root, path))
	}
	for _, tc := range []struct {
		name                     string
		includes, excludes, want []string
	}{
		{"ordinary glob keeps hidden content out", []string{"**/*.md"}, nil, []string{"notes/team.md"}},
		{"exact include selects registry only", []string{"**/*.md", ".kb/curation/_conflicts.md"}, nil, []string{".kb/curation/_conflicts.md", "notes/team.md"}},
		{"scoped hidden glob", []string{".kb/curation/*.md"}, nil, []string{".kb/curation/_check_first.md", ".kb/curation/_conflicts.md"}},
		{"explicit selection respects excludes", []string{"**/*.md", ".kb/curation/_conflicts.md"}, []string{".kb/"}, []string{"notes/team.md"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths, err := DiscoverFiles(VaultDefinition{Root: root, Includes: tc.includes, Excludes: tc.excludes})
			require.NoError(t, err)
			require.ElementsMatch(t, tc.want, paths)
		})
	}
}
