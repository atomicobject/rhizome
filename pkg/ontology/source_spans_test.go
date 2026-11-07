package ontology

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildDocumentSnapshot_PreservesMarkdownSourceSpans(t *testing.T) {
	content := "## Action Items\n\n- [ ] Update docs #action-item\n"

	snapshot, err := BuildDocumentSnapshot("notes/meeting.md", content, time.Time{})
	require.NoError(t, err)
	require.NotEmpty(t, snapshot.Sections)
	require.Len(t, snapshot.SourceSpans, 2)

	require.Equal(t, ontoschema.EmbeddedSourceShapeSection, snapshot.SourceSpans[0].Shape)
	require.Equal(t, "Action Items", snapshot.SourceSpans[0].Title)
	require.Equal(t, ontoschema.EmbeddedSourceShapeCheckboxItem, snapshot.SourceSpans[1].Shape)
	require.Equal(t, "Update docs", snapshot.SourceSpans[1].Title)
	require.Equal(t, snapshot.SourceSpans[1], snapshot.SourceSpansByID[snapshot.SourceSpans[1].ID])
}

func TestBuildDocumentSnapshot_LinksTopLevelListSiblingsToContainingSection(t *testing.T) {
	content := strings.TrimLeft(`
#### Acceptance Criteria

- **Mixed result handles**: Search returns code anchors and note sections with stable handles.
  Scenario: code and notes both match

  - Given nested context
  - Then nested bullets remain criterion detail

- Results stay compact and support continuation when truncated.

## Requirements

- Search MUST preserve stable result handles.
`, "\n")

	snapshot, err := BuildDocumentSnapshot("docs/specs/product/search.md", content, time.Time{})
	require.NoError(t, err)

	var acceptance *MarkdownSourceSpan
	for _, span := range snapshot.SourceSpans {
		if span.Shape == ontoschema.EmbeddedSourceShapeSection && span.Title == "Acceptance Criteria" {
			acceptance = span
			break
		}
	}
	require.NotNil(t, acceptance)
	children := sourceSpanChildren(acceptance)
	require.Len(t, children, 2)
	require.Equal(t, "Mixed result handles", children[0].Title)
	require.Equal(t, "Results stay compact and support continuation when truncated.", children[1].Title)
}
