package markdownsyntax

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWikilinkDiagnosticsRetainCodeExamplesWithoutSemanticLinks(t *testing.T) {
	content := "---\nrelated: \"[[frontmatter]]\"\n---\n\n[[live#Old|alias]] ![[embed]] \\[[escaped]]\n\n`[[inline]] \\[[escaped-code]]`\n\n```md\n[[fenced]]\n```\n\n    [[indented]]\n\n[[tail]]"
	links := ScanWikilinks(content, DefaultWikilinkOptions)
	require.Len(t, links, 7)
	for index, expected := range []struct {
		target    string
		raw       string
		line      int
		protected bool
	}{
		{"frontmatter", "[[frontmatter]]", 2, false},
		{"live#Old", "[[live#Old|alias]]", 5, false},
		{"embed", "![[embed]]", 5, false},
		{"inline", "[[inline]]", 7, true},
		{"fenced", "[[fenced]]", 10, true},
		{"indented", "[[indented]]", 13, true},
		{"tail", "[[tail]]", 15, false},
	} {
		require.Equal(t, expected.target, links[index].Target)
		require.Equal(t, expected.raw, links[index].Raw)
		require.Equal(t, expected.line, links[index].Line)
		require.Equal(t, expected.protected, links[index].InsideCodeBlock)
	}
	require.Equal(t, []string{"frontmatter", "inline", "fenced", "indented", "tail"}, diagnosticWikilinkTargets(ScanWikilinks(content, WikilinkOptions{SkipAnchors: true, SkipEmbeds: true})))
	active := []string{"frontmatter", "live#Old", "embed", "tail"}
	require.Equal(t, active, ExtractWikilinks(content, DefaultWikilinkOptions))
	var structuredTargets []string
	for _, link := range ScanStructuredLinkSnapshot(content).Links {
		structuredTargets = append(structuredTargets, link.Target)
	}
	require.Equal(t, active, structuredTargets)
	var allTargets []string
	for _, link := range ScanAllLinks(content, DefaultWikilinkOptions, DefaultMdLinkOptions) {
		allTargets = append(allTargets, link.Target)
	}
	require.Equal(t, active, allTargets)
}

func diagnosticWikilinkTargets(links []WikilinkDetail) []string {
	var targets []string
	for _, link := range links {
		targets = append(targets, link.Target)
	}
	return targets
}
