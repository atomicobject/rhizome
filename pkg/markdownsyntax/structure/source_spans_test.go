package structure

import (
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestParseMarkdownListSourceSpans_CapturesListAndCheckboxOwnership(t *testing.T) {
	content := strings.TrimLeft(`
---
type: Meeting
ignored:
  - [ ] not an item
---

## Action Items

- [ ] Update SPEC-0042 assigned-to:: Gabe #action-item
  wrapped continuation with [[Context]]
  - nested child detail
- [x] Already done #action-item ^done-1

`+"```md"+`
- [ ] ignored code fence #action-item
`+"```"+`

- Plain follow-up #note-item
`, "\n")

	items := ParseMarkdownListSourceSpans("notes/meeting.md", content)
	require.Len(t, items, 4)

	first := items[0]
	require.Equal(t, CheckboxItem, first.Shape)
	require.Equal(t, "notes/meeting.md#item-", first.ID[:len("notes/meeting.md#item-")])
	require.Equal(t, "notes/meeting.md", first.NotePath)
	require.NotNil(t, first.Checkbox)
	require.False(t, first.Checkbox.Checked)
	require.Equal(t, "[ ]", content[first.Checkbox.TokenRange.Start:first.Checkbox.TokenRange.End])
	require.Equal(t, "- [ ] ", content[first.MarkerRange.Start:first.MarkerRange.End])
	require.Equal(t, "Update SPEC-0042", first.Title)
	require.Contains(t, first.Content, "wrapped continuation")
	require.Contains(t, first.Content, "nested child detail")
	require.NotContains(t, first.Content, "Already done")
	require.Len(t, first.Children, 1)
	require.Equal(t, first.ID, first.Children[0].ParentID)
	require.Len(t, first.OwnContentRanges, 1)
	require.Contains(t, content[first.OwnContentRanges[0].Start:first.OwnContentRanges[0].End], "wrapped continuation")
	require.NotContains(t, content[first.OwnContentRanges[0].Start:first.OwnContentRanges[0].End], "nested child detail")
	require.NotEmpty(t, first.Markers)
	require.Equal(t, "#action-item", first.Markers[0].Value)
	require.True(t, first.Range.Valid(len(content)))
	require.True(t, first.ContentRange.Valid(len(content)))

	nested := items[1]
	require.Equal(t, ListItem, nested.Shape)
	require.Equal(t, first.ID, nested.ParentID)
	require.Contains(t, nested.Content, "nested child detail")

	done := items[2]
	require.Equal(t, CheckboxItem, done.Shape)
	require.Equal(t, "Already done", done.Title)
	require.NotNil(t, done.Checkbox)
	require.True(t, done.Checkbox.Checked)
	require.Equal(t, "done-1", done.BlockID)
	require.Equal(t, "notes/meeting.md#^done-1", done.ID)

	plain := items[3]
	require.Equal(t, ListItem, plain.Shape)
	require.Nil(t, plain.Checkbox)
	require.Contains(t, plain.Content, "Plain follow-up")
}

func TestParseMarkdownListSourceSpans_IgnoresInlineCodeIdentifierBlockIDExamples(t *testing.T) {
	items := ParseMarkdownListSourceSpans("notes/spec.md", "- Prefer a metadata bullet such as `- id:: ^SPEC-0023-US1`.\n")
	require.Len(t, items, 1)
	require.Empty(t, items[0].BlockID)
	require.Empty(t, items[0].MalformedBlockID)
}

func TestParseMarkdownListSourceSpans_KeepsTopLevelSiblingAfterNestedDetail(t *testing.T) {
	content := strings.TrimLeft(`
#### Acceptance Criteria

- **Mixed result handles**: Search returns code anchors and note sections with stable handles.
  Scenario: code and notes both match

  - Given nested context
  - Then nested bullets remain criterion detail

- Results stay compact and support continuation when truncated.
`, "\n")

	items := ParseMarkdownListSourceSpans("docs/specs/product/search.md", content)
	require.Len(t, items, 4)
	require.Equal(t, "Mixed result handles", items[0].Title)
	require.Empty(t, items[0].ParentID)
	require.Equal(t, items[0].ID, items[1].ParentID)
	require.Equal(t, items[0].ID, items[2].ParentID)
	require.Equal(t, "Results stay compact and support continuation when truncated.", items[3].Title)
	require.Empty(t, items[3].ParentID)
	require.NotContains(t, items[0].Content, "Results stay compact")
}

func TestParseStartsNewListAfterHeading(t *testing.T) {
	content := "- one\n\n## Next\n  - two\n"
	spans := ParseMarkdownListSourceSpans("note.md", content)
	require.Len(t, spans, 2)
	require.Empty(t, spans[1].ParentID)
	require.NoError(t, noteformat.ValidateStructureFacts(Parse("note.md", content), len(content)))
}
