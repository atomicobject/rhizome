package obsidian

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRewriteLinksOldPathAlternativesShareOriginalPass(t *testing.T) {
	content := "[[docs/Source]] [[alias/Source]] [canonical](docs/Source.md) [authored](alias/Source.md) `[[docs/Source]]` [[docs/SourceExtra]]\n"
	got, count := RewriteLinksInContentWithOptions(content, "docs/Source.md", "alias/Source.md", false, "alias/Source.md", "docs/Source.md", "alias/Source.md")
	require.Equal(t, "[[alias/Source]] [[alias/Source]] [canonical](alias/Source.md) [authored](alias/Source.md) `[[docs/Source]]` [[docs/SourceExtra]]\n", got)
	require.Equal(t, 4, count)
}

func TestRewriteLinksOldPathAlternativesRetainBasenamePolicy(t *testing.T) {
	for _, unique := range []bool{false, true} {
		got, count := RewriteLinksInContentWithOptions("[[Source]] [[alias/Source]] [[docs/SourceExtra]]", "docs/Source.md", "Archive/Final.md", unique, "alias/Source.md")
		if unique {
			require.Equal(t, "[[Final]] [[Archive/Final]] [[docs/SourceExtra]]", got)
			require.Equal(t, 2, count)
		} else {
			require.Equal(t, "[[Source]] [[Archive/Final]] [[docs/SourceExtra]]", got)
			require.Equal(t, 1, count)
		}
	}
}
