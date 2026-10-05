package ontology

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildIndexFromNoteSourcesProjectsMixedFormatRootsWithoutHTMLStructure(t *testing.T) {
	repo := t.TempDir()
	writeOntologyTestConfig(t, repo)
	writeOntologySchema(t, repo, `
type Project @node(paths: ["notes/*.md", "notes/*.html"]) {
  status: String! @field
  summary: String @field
}
`)
	schema, err := LoadSchema(repo)
	require.NoError(t, err)

	markdownSource := mixedFormatSource(t, markdown.New(), "notes/alpha.md", []byte("---\ntype: Project\nstatus: active\nsummary: Shared facts\n---\n# Alpha\n"))
	htmlSource := mixedFormatSource(t, testHTMLRootProjector{}, "notes/beta.html", []byte(`<!doctype html><html><body><h1 id="details">Details</h1></body></html>`))

	result, err := BuildIndexFromNoteSources(context.Background(), obsidian.VaultDefinition{Path: repo}, []notemeta.NoteSourceSnapshot{markdownSource, htmlSource}, schema, "mixed")
	require.NoError(t, err)
	require.Len(t, result.Nodes, 2)
	for _, node := range result.Nodes {
		assert.Equal(t, "NOTE", node.NodeKind)
		assert.Equal(t, "Project", node.TypeName)
	}
	var htmlNodeID string
	for _, node := range result.Nodes {
		if node.NotePath == "notes/beta.html" {
			htmlNodeID = node.NodeID
			assert.Equal(t, "HTML Project", node.Title)
		}
	}
	require.NotEmpty(t, htmlNodeID)
	fields := map[string]string{}
	for _, row := range result.NodeFieldValues {
		if row.NodeID == htmlNodeID {
			fields[row.FieldName] = row.ValueText
		}
	}
	assert.Equal(t, map[string]string{"status": "active", "summary": "Shared facts"}, fields)
}

type testHTMLRootProjector struct{}

func (testHTMLRootProjector) Descriptor() noteformat.Descriptor {
	return noteformat.Descriptor{
		ID: "html", Extensions: []string{".html", ".htm"},
		ProviderVersion: "test-html-v1", ProjectionVersion: "test-root-v1",
		OwnershipPolicy: noteformat.OwnershipExplicitInclude,
		Capabilities: noteformat.MustCapabilities(
			noteformat.CapabilitySourceReading,
			noteformat.CapabilityRootMetadataReading,
			noteformat.CapabilityActiveContentViewing,
		),
	}
}

func (p testHTMLRootProjector) Project(noteformat.AuthoredSource) (noteformat.Projection, error) {
	facts := noteformat.ProjectionFacts{Title: &noteformat.TitleFact{Value: "HTML Project"}}
	for _, item := range []struct {
		key   string
		value any
	}{{"type", "Project"}, {"status", "active"}, {"summary", "Shared facts"}, {"title", "HTML Project"}} {
		value, err := noteformat.NewMetadataValue(item.value)
		if err != nil {
			return noteformat.Projection{}, err
		}
		facts.RootMetadata = append(facts.RootMetadata, noteformat.RootMetadataFact{Key: item.key, Value: value})
	}
	descriptor := p.Descriptor()
	return noteformat.NewProjectionWithFacts(descriptor.ProviderVersion, descriptor.ProjectionVersion, noteformat.ProjectionStatusCurrent, nil, descriptor.Capabilities, facts)
}

func mixedFormatSource(t *testing.T, projector noteformat.Projector, notePath string, content []byte) notemeta.NoteSourceSnapshot {
	t.Helper()
	canonical, err := paths.CleanNotePath(notePath)
	require.NoError(t, err)
	source, err := noteformat.NewAuthoredSource(canonical, projector.Descriptor(), content, 1)
	require.NoError(t, err)
	projection, err := projector.Project(source)
	require.NoError(t, err)
	metadata := map[string]any{}
	for _, fact := range projection.Facts.RootMetadata {
		metadata[fact.Key] = fact.Value.Export()
	}
	title := ""
	if projection.Facts.Title != nil {
		title = projection.Facts.Title.Value
	}
	return notemeta.NoteSourceSnapshot{
		Path: canonical, Format: projector.Descriptor().ID,
		Projection: projection, Content: string(content), RawSource: content,
		ContentHash: source.ContentHash(), Mtime: 1, Size: int64(len(content)),
		Title: title, Frontmatter: metadata, SearchRegions: projection.Facts.SearchRegions,
	}
}
