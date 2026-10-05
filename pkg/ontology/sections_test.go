package ontology

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSections_IgnoresHeadingsInsideCodeFences(t *testing.T) {
	nodes := ParseSections("notes/spec.md", `
# Intro

~~~md
## Requirements
~~~

## Real Requirements

Body.
`)
	require.Len(t, nodes, 1)
	require.Equal(t, "Intro", nodes[0].Title)
	require.Len(t, nodes[0].Children, 1)
	require.Equal(t, "Real Requirements", nodes[0].Children[0].Title)
}

func TestParseSections_IgnoresIndentedCodeBlockHeadings(t *testing.T) {
	nodes := ParseSections("notes/spec.md", `
# Intro

    ## Fake Requirements

   ## Real Requirements

Body.
`)
	require.Len(t, nodes, 1)
	require.Equal(t, "Intro", nodes[0].Title)
	require.Len(t, nodes[0].Children, 1)
	require.Equal(t, "Real Requirements", nodes[0].Children[0].Title)
}

func TestParseSections_ClosesSectionAtNextSameOrHigherHeading(t *testing.T) {
	nodes := ParseSections("notes/spec.md", `
## Requirements

Keep this.

### Detail

Nested.

## Decisions

Later.
`)
	require.Len(t, nodes, 2)
	require.Equal(t, "Requirements", nodes[0].Title)
	require.Equal(t, "Decisions", nodes[1].Title)
	require.Len(t, nodes[0].Children, 1)
	require.Equal(t, "Detail", nodes[0].Children[0].Title)
	require.Contains(t, nodes[0].Content, "### Detail")
	require.NotContains(t, nodes[0].Content, "## Decisions")
}

func TestFindMatchingSections_ReturnsDocumentOrder(t *testing.T) {
	nodes := ParseSections("notes/spec.md", `
## Requirements

First.

## Requirements

Second.
`)
	matches := FindMatchingSections(nodes, SectionLevelH2, "Requirements")
	require.Len(t, matches, 2)
	require.Contains(t, matches[0].Content, "First.")
	require.Contains(t, matches[1].Content, "Second.")
}

func TestUnwrapSingleH1SectionRoot_ReturnsChildrenForWrappedNote(t *testing.T) {
	nodes := ParseSections("notes/spec.md", `
# Spec title

## Requirements

Body.

## Decisions

Later.
`)
	require.Len(t, nodes, 1)
	roots := UnwrapSingleH1SectionRoot(nodes)
	require.Len(t, roots, 2)
	require.Equal(t, "Requirements", roots[0].Title)
	require.Equal(t, "Decisions", roots[1].Title)
}

func TestUnwrapSingleH1SectionRoot_LeavesOtherShapesUntouched(t *testing.T) {
	nodes := ParseSections("notes/spec.md", `
## Requirements

Body.
`)
	roots := UnwrapSingleH1SectionRoot(nodes)
	require.Len(t, roots, 1)
	require.Equal(t, "Requirements", roots[0].Title)
}

func TestParseSections_StripsClosingATXHashesFromTitles(t *testing.T) {
	nodes := ParseSections("notes/spec.md", `
## Requirements ##

Body.

### Detail ###

Nested.
`)
	require.Len(t, nodes, 1)
	require.Equal(t, "Requirements", nodes[0].Title)
	require.Len(t, nodes[0].Children, 1)
	require.Equal(t, "Detail", nodes[0].Children[0].Title)

	matches := FindMatchingSections(nodes, SectionLevelH2, "Requirements")
	require.Len(t, matches, 1)
	require.Equal(t, "Requirements", matches[0].Title)
}

func TestParseSections_UsesBlockIDForStableSectionIdentity(t *testing.T) {
	nodes := ParseSections("notes/spec.md", `
## Requirements

Paragraph.

^req-001
`)
	require.Len(t, nodes, 1)
	require.Equal(t, "req-001", nodes[0].BlockID)
	require.Equal(t, "notes/spec.md#^req-001", nodes[0].ID)
}

func TestParseSections_UsesProseBlockIDForStableSectionIdentity(t *testing.T) {
	nodes := ParseSections("notes/spec.md", `
## Requirements

Paragraph with an Obsidian-compatible prose block identifier. ^req-001
`)
	require.Len(t, nodes, 1)
	require.Equal(t, "req-001", nodes[0].BlockID)
	require.Empty(t, nodes[0].MalformedBlockID)
	require.Equal(t, "notes/spec.md#^req-001", nodes[0].ID)
}

func TestParseSections_RejectsBlockIDInHeading(t *testing.T) {
	nodes := ParseSections("notes/spec.md", `
## Requirements ^req-001

Paragraph.
`)
	require.Len(t, nodes, 1)
	require.Empty(t, nodes[0].BlockID)
	require.Equal(t, "req-001", nodes[0].MalformedBlockID)
}

func TestParseSections_IgnoresInlineCodeBlockIDExamples(t *testing.T) {
	nodes := ParseSections("notes/spec.md", `
## Requirements

Write examples like `+"`^req-001`"+` without making this section linkable.
`)
	require.Len(t, nodes, 1)
	require.Empty(t, nodes[0].BlockID)
	require.Empty(t, nodes[0].MalformedBlockID)
}

func TestParseSections_IgnoresInlineCodeIdentifierBlockIDExamples(t *testing.T) {
	nodes := ParseSections("notes/spec.md", `
## Requirements

Prefer a metadata bullet such as `+"`- id:: ^SPEC-0023-US1`"+`. Spec-driven stories use this policy.
`)
	require.Len(t, nodes, 1)
	require.Empty(t, nodes[0].BlockID)
	require.Empty(t, nodes[0].MalformedBlockID)
}

func TestParseSections_UsesIdentifierBackedBlockIDForStableSectionIdentity(t *testing.T) {
	nodes := ParseSections("notes/spec.md", `
## Requirements

id:: ^SPEC-0023-US1
status:: ready
`)
	require.Len(t, nodes, 1)
	require.Equal(t, "SPEC-0023-US1", nodes[0].BlockID)
	require.Equal(t, "notes/spec.md#^SPEC-0023-US1", nodes[0].ID)
}

func TestParseSections_UsesListIdentifierBackedBlockIDForStableSectionIdentity(t *testing.T) {
	nodes := ParseSections("notes/spec.md", `
## Requirements

- id:: ^SPEC-0023-US1
- [ ] Acceptance criterion ^criterion-1
`)
	require.Len(t, nodes, 1)
	require.Equal(t, "SPEC-0023-US1", nodes[0].BlockID)
	require.Equal(t, "notes/spec.md#^SPEC-0023-US1", nodes[0].ID)
}

func TestParseSections_DoesNotStealListItemBlockID(t *testing.T) {
	nodes := ParseSections("notes/spec.md", `
## Requirements

- [ ] Acceptance criterion ^criterion-1
`)
	require.Len(t, nodes, 1)
	require.Empty(t, nodes[0].BlockID)
	require.NotEqual(t, "notes/spec.md#^criterion-1", nodes[0].ID)
}

func TestParseSections_UsesInlinePropertyBlockIDForStableSectionIdentity(t *testing.T) {
	nodes := ParseSections("notes/spec.md", `
## Requirements

storyKey:: ^SPEC-0023-US1
status:: ready
`)
	require.Len(t, nodes, 1)
	require.Equal(t, "SPEC-0023-US1", nodes[0].BlockID)
	require.Equal(t, "notes/spec.md#^SPEC-0023-US1", nodes[0].ID)
}

func TestParseSections_IgnoresBlockIDShapedLinesInsideCodeFences(t *testing.T) {
	nodes := ParseSections("notes/spec.md", `
## Requirements

~~~md
^req-001
~~~

Paragraph.
`)
	require.Len(t, nodes, 1)
	require.Empty(t, nodes[0].BlockID)
	require.Contains(t, SectionOwnContent(nodes[0]), "^req-001")
}

func TestSectionOwnContent_NilOrEmpty(t *testing.T) {
	require.Equal(t, "", SectionOwnContent(nil))

	nodes := ParseSections("notes/spec.md", `## Requirements`)
	require.Len(t, nodes, 1)
	require.Equal(t, "", SectionOwnContent(nodes[0]))
}

func TestSectionOwnContent_NoChildrenMatchesSectionBody(t *testing.T) {
	nodes := ParseSections("notes/spec.md", `
## Requirements

Paragraph one.

Paragraph two.
`)
	require.Len(t, nodes, 1)
	require.Equal(t, SectionBody(nodes[0]), SectionOwnContent(nodes[0]))
	require.Contains(t, SectionOwnContent(nodes[0]), "Paragraph one.")
	require.Contains(t, SectionOwnContent(nodes[0]), "Paragraph two.")
}

func TestSectionOwnContent_StripsStandaloneBlockIDLine(t *testing.T) {
	nodes := ParseSections("notes/spec.md", `
## Requirements

Paragraph one.

^req-001
`)
	require.Len(t, nodes, 1)
	own := SectionOwnContent(nodes[0])
	require.Contains(t, own, "Paragraph one.")
	require.NotContains(t, own, "^req-001")
}

func TestSectionOwnContent_StripsSingleChildAtStart(t *testing.T) {
	nodes := ParseSections("notes/spec.md", `
## Requirements
### Req 1

body1

Tail paragraph.
`)
	require.Len(t, nodes, 1)
	own := SectionOwnContent(nodes[0])
	require.NotContains(t, own, "### Req 1")
	require.NotContains(t, own, "body1")
	// Tail paragraph is nested inside Req 1 (no subsequent sibling closes it),
	// so it belongs to the child, not the parent.
	require.Equal(t, "", own)
}

func TestSectionOwnContent_PreservesContentBetweenChildren(t *testing.T) {
	nodes := ParseSections("notes/spec.md", `
## Requirements

Intro prop:: value

### Req 1

body1

### Req 2

body2
`)
	require.Len(t, nodes, 1)
	own := SectionOwnContent(nodes[0])
	require.Contains(t, own, "Intro prop:: value")
	require.NotContains(t, own, "### Req 1")
	require.NotContains(t, own, "body1")
	require.NotContains(t, own, "### Req 2")
	require.NotContains(t, own, "body2")
}

func TestSectionOwnContent_InlinePropsBeforeFirstChild(t *testing.T) {
	nodes := ParseSections("notes/spec.md", `
## Requirements

Owner:: @drew
Status:: in-progress

### Req 1

body
`)
	require.Len(t, nodes, 1)
	own := SectionOwnContent(nodes[0])
	require.Contains(t, own, "Owner:: @drew")
	require.Contains(t, own, "Status:: in-progress")
	require.NotContains(t, own, "### Req 1")
	require.NotContains(t, own, "body")
}

func TestSectionOwnContent_NestedGrandchildrenSubtractedViaDirectChild(t *testing.T) {
	nodes := ParseSections("notes/spec.md", `
## Requirements

Summary:: top

### Req 1

body1

#### Sub 1

leaf

### Req 2

body2
`)
	require.Len(t, nodes, 1)
	own := SectionOwnContent(nodes[0])
	require.Contains(t, own, "Summary:: top")
	require.NotContains(t, own, "Sub 1")
	require.NotContains(t, own, "leaf")
	require.NotContains(t, own, "### Req 1")
	require.NotContains(t, own, "### Req 2")
}
