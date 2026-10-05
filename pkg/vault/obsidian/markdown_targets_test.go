package obsidian

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnumerateMarkdownTargets_HeadingsBlocksAndDuplicates(t *testing.T) {
	content := "# Root\n\n## Notes ^notes-a\n\n## Notes\n\nParagraph. ^paragraph-a\n\n^standalone-a\n\nSetext title\n------------\n"

	targets := EnumerateMarkdownTargets(content)

	require.Equal(t, []MarkdownTarget{
		{Kind: MarkdownTargetHeading, Text: "Root", NormalizedText: "root", Ordinal: 1, Level: 1, Line: 1, StartByte: 0, EndByte: 6},
		{Kind: MarkdownTargetHeading, Text: "Notes", NormalizedText: "notes", Ordinal: 1, Level: 2, Line: 3, StartByte: 8, EndByte: 25},
		{Kind: MarkdownTargetBlock, Text: "notes-a", NormalizedText: "notes-a", Ordinal: 1, Line: 3, StartByte: 17, EndByte: 25},
		{Kind: MarkdownTargetHeading, Text: "Notes", NormalizedText: "notes", Ordinal: 2, Level: 2, Line: 5, StartByte: 27, EndByte: 35},
		{Kind: MarkdownTargetBlock, Text: "paragraph-a", NormalizedText: "paragraph-a", Ordinal: 1, Line: 7, StartByte: 48, EndByte: 60},
		{Kind: MarkdownTargetBlock, Text: "standalone-a", NormalizedText: "standalone-a", Ordinal: 1, Line: 9, StartByte: 62, EndByte: 75},
		{Kind: MarkdownTargetHeading, Text: "Setext title", NormalizedText: "setext title", Ordinal: 1, Level: 2, Line: 11, StartByte: 77, EndByte: 102},
	}, targets)

	// The health fixture keeps heading ancestry and inline IDs in one canonical parse.
	healthContent := "# Root\n\n## Notes ^root-notes\n\n### Child\n\n## Notes\nid:: ^first\ntext\nid:: ^second\n"
	healthTargets := EnumerateMarkdownTargets(healthContent)
	var headings []MarkdownTarget
	var blocks []string
	for _, target := range healthTargets {
		if target.Kind == MarkdownTargetHeading {
			headings = append(headings, target)
		}
		if target.Kind == MarkdownTargetBlock {
			blocks = append(blocks, target.Text)
		}
	}
	require.Len(t, headings, 4)
	require.Equal(t, []string{"Root", "Notes", "Child", "Notes"}, []string{headings[0].Text, headings[1].Text, headings[2].Text, headings[3].Text})
	require.Equal(t, []int{1, 2, 3, 2}, []int{headings[0].Level, headings[1].Level, headings[2].Level, headings[3].Level})
	require.ElementsMatch(t, []string{"root-notes", "first", "second"}, blocks)
	wrapped := EnumerateHeadings(healthContent)
	require.Equal(t, "Root", wrapped[1].ParentText)
	require.Equal(t, "Notes", wrapped[2].ParentText)
}

func TestEnumerateMarkdownTargets_SetextLevels(t *testing.T) {
	targets := EnumerateMarkdownTargets("Primary\n=======\n\nSecondary\n---\n")

	require.Equal(t, []MarkdownTarget{
		{Kind: MarkdownTargetHeading, Text: "Primary", NormalizedText: "primary", Ordinal: 1, Level: 1, Line: 1, StartByte: 0, EndByte: 15},
		{Kind: MarkdownTargetHeading, Text: "Secondary", NormalizedText: "secondary", Ordinal: 1, Level: 2, Line: 4, StartByte: 17, EndByte: 30},
	}, targets)
}

func TestEnumerateMarkdownTargets_SetextDoesNotCrossContainerBoundary(t *testing.T) {
	tests := map[string]string{
		"block quote":        "> Quoted paragraph\n---\n",
		"unordered list":     "- List item\n---\n",
		"ordered list":       "1. List item\n===\n",
		"indented list item": "  + List item\n---\n",
	}

	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			require.Empty(t, EnumerateMarkdownTargets(content))
		})
	}
}

func TestEnumerateMarkdownTargets_ExcludesFrontmatterAndCodeLookalikes(t *testing.T) {
	content := "---\ntitle: Not a heading\n---\n\n```md\n## Fenced\n^fenced\n```\n\n~~~\nOther fenced\n------------\nparagraph ^tilde\n~~~\n\n    ^indented\n\n`^inline-only`\n\nReal\n----\n\nParagraph with `^inline`\n"

	targets := EnumerateMarkdownTargets(content)

	require.Equal(t, []MarkdownTarget{
		{Kind: MarkdownTargetHeading, Text: "Real", NormalizedText: "real", Ordinal: 1, Level: 2, Line: 20, StartByte: 142, EndByte: 151},
	}, targets)
}

func TestEnumerateMarkdownTargets_InlineCodeCannotExposeTargets(t *testing.T) {
	content := "Paragraph ^false `code`\nTitle\n`code`---\nCandidate\n`multi\n---\nParagraph ^also-false\nline`\n"

	require.Empty(t, EnumerateMarkdownTargets(content))
}

func TestEnumerateMarkdownTargets_ThematicBreakIsNotSetextText(t *testing.T) {
	require.Empty(t, EnumerateMarkdownTargets("***\n---\n\n_ _ _\n---\n"))
}

func TestEnumerateMarkdownTargets_BlockIDsAreCaseSensitive(t *testing.T) {
	targets := EnumerateMarkdownTargets("Paragraph ^Block-A\nParagraph ^block-a\n")

	require.Equal(t, []MarkdownTarget{
		{Kind: MarkdownTargetBlock, Text: "Block-A", NormalizedText: "Block-A", Ordinal: 1, Line: 1, StartByte: 10, EndByte: 18},
		{Kind: MarkdownTargetBlock, Text: "block-a", NormalizedText: "block-a", Ordinal: 1, Line: 2, StartByte: 29, EndByte: 37},
	}, targets)
	require.Equal(t, "heading text", NormalizeMarkdownHeadingFragment("  Heading Text  "))
}

func TestEnumerateMarkdownTargets_FenceClosingMustMatchMarkerAndLength(t *testing.T) {
	content := "````md\n## Hidden\n```\n^also-hidden\n~~~\nStill hidden\n---\n````\n\n## Visible\n"

	targets := EnumerateMarkdownTargets(content)

	require.Equal(t, []MarkdownTarget{
		{Kind: MarkdownTargetHeading, Text: "Visible", NormalizedText: "visible", Ordinal: 1, Level: 2, Line: 10, StartByte: 61, EndByte: 71},
	}, targets)
}

func TestEnumerateMarkdownTargets_CRLFOffsetsRemainSourceByteOffsets(t *testing.T) {
	content := "Title\r\n=====\r\n\r\nParagraph ^Block-A\r\n"

	targets := EnumerateMarkdownTargets(content)

	require.Len(t, targets, 2)
	require.Equal(t, "Title\r\n=====", content[targets[0].StartByte:targets[0].EndByte])
	require.Equal(t, "^Block-A", content[targets[1].StartByte:targets[1].EndByte])
}

func TestEnumerateHeadings_UsesObsidianATXClosingAndIndentRules(t *testing.T) {
	headings := EnumerateHeadings("  ## Heading ##\n")

	require.Equal(t, []HeadingInfo{{
		Text:           "Heading",
		Level:          2,
		NormalizedText: "heading",
		Line:           1,
	}}, headings)
}

func TestEnumerateMarkdownTargets_AllowsSingleMarkerSetext(t *testing.T) {
	require.Equal(t, []MarkdownTarget{
		{Kind: MarkdownTargetHeading, Text: "Primary", NormalizedText: "primary", Ordinal: 1, Level: 1, Line: 1, StartByte: 0, EndByte: 9},
		{Kind: MarkdownTargetHeading, Text: "Secondary", NormalizedText: "secondary", Ordinal: 1, Level: 2, Line: 4, StartByte: 11, EndByte: 22},
	}, EnumerateMarkdownTargets("Primary\n=\n\nSecondary\n-\n"))
}

func TestHealthMarkdownTargetWrappersUseCanonicalParser(t *testing.T) {
	content := "# Root\n\nSetext child\n------------\n\n```md\n## Fake\n^fake\n```\n\nParagraph ^real\n"

	headings := EnumerateHeadings(content)
	require.Equal(t, []HeadingInfo{
		{Text: "Root", Level: 1, NormalizedText: "root", Line: 1},
		{Text: "Setext child", Level: 2, NormalizedText: "setext child", Line: 3, ParentText: "Root"},
	}, headings)

	blocks, headingSet := extractBlockIDsAndHeadings(content)
	require.Equal(t, map[string]struct{}{"real": {}}, blocks)
	require.Equal(t, map[string]struct{}{"root": {}, "setext child": {}}, headingSet)
}
