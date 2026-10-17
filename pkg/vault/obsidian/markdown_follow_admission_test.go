package obsidian

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFollowWikilinksPreservesMarkdownRejection(t *testing.T) {
	for _, tc := range []struct {
		name, source, target string
		expected             []string
	}{
		{"root drive", "Source.md", "C%3A/Other/Only.md", []string{"Source.md"}},
		{"nested drive", "Notes/Source.md", "C%3A/Other/Only.md", []string{"Notes/Source.md"}},
		{"root colon", "Source.md", "a%3Ab/Only.md", []string{"Source.md"}},
		{"nested colon", "Notes/Source.md", "a%3Ab/Only.md", []string{"Notes/Source.md"}},
		{"confined parent", "Notes/Source.md", "../Other/Only.md", []string{"Notes/Source.md", "Other/Only.md"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for name, body := range map[string]string{tc.source: "[target](" + tc.target + ")", "Other/Only.md": "# Target"} {
				full := filepath.Join(root, name)
				require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
				require.NoError(t, os.WriteFile(full, []byte(body), 0644))
			}
			vault := VaultDefinition{Path: root, Links: LinkTypeBoth}
			cache := BuildNotePathCache([]string{tc.source, "Other/Only.md"})
			followed, err := FollowWikilinks(vault, &Note{}, tc.source, map[string]bool{}, cache, DefaultFollowWikilinksOptions)
			require.NoError(t, err)
			require.Equal(t, tc.expected, followed)
		})
	}
}
