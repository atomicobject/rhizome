package obsidian

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindBrokenLinksContextDecodesMarkdownPathsOnce(t *testing.T) {
	for _, indexed := range []bool{true, false} {
		mode := "indexed"
		if !indexed {
			mode = "filesystem fallback"
		}
		t.Run(mode, func(t *testing.T) {
			for _, tc := range []struct{ name, actual, encoded, decoy, display string }{
				{"literal percent escape", "targets/Budget%20USD.md", "../targets/Budget%2520USD.md", "targets/Budget USD.md", "../targets/Budget%20USD.md"},
				{"literal hash", "targets/Budget#USD.md", "../targets/Budget%23USD.md", "targets/Budget.md", "../targets/Budget#USD.md"},
				{"space", "targets/Budget USD.md", "../targets/Budget%20USD.md", "targets/Budget.md", "../targets/Budget USD.md"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					root := t.TempDir()
					const source = "notes/source.md"
					goodHeading := "[right heading](" + tc.encoded + "#Right%20Heading)"
					badHeading := "[wrong heading](" + tc.encoded + "#Wrong%20Heading)"
					goodBlock := "[right block](" + tc.encoded + "#%5ERightBlock)"
					badBlock := "[wrong block](" + tc.encoded + "#%5EWrongBlock)"
					contents := map[string]string{
						source:    goodHeading + "\n" + badHeading + "\n" + goodBlock + "\n" + badBlock + "\n",
						tc.actual: "# Right Heading\n\nParagraph ^RightBlock\n",
						tc.decoy:  "# Wrong Heading\n\nParagraph ^WrongBlock\n",
					}
					for name, content := range contents {
						file := filepath.Join(root, filepath.FromSlash(name))
						require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
						require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
					}
					notes := []string{source, tc.decoy}
					if indexed {
						notes = append(notes, tc.actual)
					}
					reader := &healthContextReader{notes: notes, contents: contents}
					broken, err := FindBrokenLinksContext(context.Background(), VaultDefinition{Name: "test", Path: root}, reader, DefaultBrokenLinksOptions)
					require.NoError(t, err)
					require.ElementsMatch(t, []BrokenLink{
						{Source: source, Target: tc.display, LinkType: BacklinkTypeBasic, Fragment: "Wrong%20Heading", Reason: BrokenLinkReasonHeadingMissing, Line: 2, Raw: badHeading, Markdown: true},
						{Source: source, Target: tc.display, LinkType: BacklinkTypeBasic, Fragment: "%5EWrongBlock", Reason: BrokenLinkReasonBlockMissing, Line: 4, Raw: badBlock, Markdown: true},
					}, broken, "only the actual target's missing heading and block are broken; the decoy has both")
				})
			}
		})
	}
}
