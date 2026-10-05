package ontology

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRootDocumentSnapshotSeparatesProviderRootFromMarkdownStructure(t *testing.T) {
	htmlSource := []byte("<!doctype html>\r\n<h1>Prototype</h1>\r\n")
	region := noteformat.SearchRegionFact{Origin: noteformat.SearchRegionDerived, Kind: noteformat.SearchRegionVisible, Text: "Prototype", MediaType: "text/html"}
	htmlProjection, err := noteformat.NewProjectionWithFacts(
		"html-provider-v1", "html-projection-v1", noteformat.ProjectionStatusCurrent, nil,
		noteformat.MustCapabilities(noteformat.CapabilitySourceReading),
		noteformat.ProjectionFacts{SearchRegions: []noteformat.SearchRegionFact{region}},
	)
	require.NoError(t, err)

	root, err := BuildRootDocumentSnapshot(notemeta.NoteSourceSnapshot{
		Path: paths.NotePath("prototypes/demo.html"), Format: "html",
		RawSource: htmlSource, Projection: htmlProjection,
		SearchRegions: []noteformat.SearchRegionFact{region},
	})
	require.NoError(t, err)
	assert.Equal(t, NodeRef{NotePath: "prototypes/demo.html", Kind: NodeKindNote}, root.Root)
	assert.Equal(t, htmlSource, root.RawSource)
	assert.NotEmpty(t, root.ContentHash)
	assert.Equal(t, SourceRepresentationUTF8, root.SourceRepresentation)
	assert.Equal(t, EvidenceRepresentationProviderProjection, root.EvidenceRepresentation)
	assert.Equal(t, []noteformat.SearchRegionFact{region}, root.SearchRegions)
	assert.Nil(t, root.MarkdownStructure)

	root.RawSource[0] = 'X'
	assert.Equal(t, byte('<'), htmlSource[0], "root source must be sealed from caller mutation")
}

func TestMarkdownProjectionAttachesGenuineMarkdownSnapshot(t *testing.T) {
	content := "---\ntitle: Example\n---\n# Heading\n"
	projection, err := noteformat.NewProjectionWithFacts(
		"markdown-provider-v1", "markdown-projection-v3", noteformat.ProjectionStatusCurrent, nil,
		noteformat.MustCapabilities(noteformat.CapabilitySourceReading), noteformat.ProjectionFacts{},
	)
	require.NoError(t, err)

	source := notemeta.NoteSourceSnapshot{
		Path: paths.NotePath("notes/example.md"), Format: "markdown",
		Content: content, RawSource: []byte(content), Projection: projection,
	}
	doc, err := projectMarkdownNoteSource(source)
	require.NoError(t, err)
	require.NotNil(t, doc.Snapshot)
	assert.Equal(t, "Example", doc.Snapshot.Frontmatter["title"])
	require.Len(t, doc.Snapshot.Sections, 1)
	assert.Equal(t, "Heading", doc.Snapshot.Sections[0].Title)
}
