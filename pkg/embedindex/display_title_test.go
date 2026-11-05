package embedindex

import (
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestFullDisplayLabelDoesNotChangeEmbeddingText(t *testing.T) {
	title := "Review the complete native desktop workflow " + strings.Repeat("including its keyboard shortcuts ", 5)
	content := "- [ ] " + title + " #action-item\n"
	snapshot, err := ontology.BuildDocumentSnapshot("meeting.md", content, time.Time{})
	require.NoError(t, err)
	span := ontology.ParseMarkdownListSourceSpans("meeting.md", content)[0]
	projection := &ontology.NodeProjection{Snapshot: snapshot, Ref: ontology.NodeRef{NotePath: "meeting.md", NodeID: span.ID, Kind: ontology.NodeKindEmbedded}}
	current, err := ontology.IntelOntologyNodeForProjection(nil, projection, nil, 1)
	require.NoError(t, err)
	require.Equal(t, strings.TrimSpace(title), current.DisplayLabel)
	previous := current
	previous.DisplayLabel = span.Title
	chunker := ontologyNodeChunker{}
	before := chunker.nodeBodyText(projection, previous, nil, content, false)
	after := chunker.nodeBodyText(projection, current, nil, content, false)
	require.Equal(t, before, after)
	require.Equal(t, 1, strings.Count(after, title))
}
