package ontology

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestDisplayTitlePreservesEmbeddingInputs(t *testing.T) {
	title := "Review the complete native desktop workflow " + strings.Repeat("and its keyboard shortcuts ", 5) + "before publishing"
	content := "- [ ] " + title + " #action-item\n  assignee:: Drew\n"
	spans := ParseMarkdownListSourceSpans("meeting.md", content)
	require.Len(t, spans, 1)
	span := spans[0]
	require.Equal(t, strings.TrimSpace(title[:80]), span.Title)
	require.Equal(t, title, sourceSpanDisplayTitle(content, span))
	itemTitle, summary, detail := sourceSpanItemParts(content, span)
	require.Empty(t, itemTitle)
	require.Equal(t, strings.TrimSpace(title[:80]), summary)
	require.Empty(t, detail)
	require.Equal(t, content, span.Content)
	snapshot, err := BuildDocumentSnapshot("meeting.md", content, time.Time{})
	require.NoError(t, err)
	projection := &NodeProjection{Snapshot: snapshot, Ref: NodeRef{NotePath: "meeting.md", NodeID: span.ID, Kind: NodeKindEmbedded}}
	row, err := IntelOntologyNodeForProjection(nil, projection, nil, 1)
	require.NoError(t, err)
	require.Equal(t, span.Title, row.Title)
	require.Equal(t, title, row.DisplayLabel)
	require.Equal(t, title, nodeProjectionTitle(projection))
}

func TestDisplayTitlePreservesHeadingLabel(t *testing.T) {
	snapshot, err := BuildDocumentSnapshot("meeting.md", "### Validation story\n\nBody\n", time.Time{})
	require.NoError(t, err)
	require.Equal(t, "Validation story", sourceSpanDisplayTitle(snapshot.Content, snapshot.SourceSpans[0]))
}
