package ontology

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/noteformat/html"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestMarkdownSelectorFactsAgreeAcrossIndexCurrentAndSnapshot(t *testing.T) {
	for _, fixture := range []struct {
		name, content, matcher string
		matches                bool
	}{
		{"frontmatter tags", "---\nTAGS: '#topic/child'\n---\n", "tag:topic", true},
		{"inline tags", "# Source\n\n#topic/child\n", "tag:topic", true},
		{"code-only tags", "# Source\n\n```\n#topic/child\n```\n", "tag:topic", false},
		{"summary is not provider title", "---\nsummary: UniqueSummary\n---\n", "find:UniqueSummary", false},
		{"numeric metadata is not provider title", "---\ntitle: 981742\n---\n", "find:981742", false},
		{"authored title", "---\ntitle: UniqueTitle\n---\n", "find:UniqueTitle", true},
		{"visible heading", "# **UniqueHeading**\n", "find:UniqueHeading", true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			writeOntologySchema(t, root, `type Subject @node(matches: ["`+fixture.matcher+`"]) { name: String }`)
			writeOntologyNote(t, root, "notes/source.md", fixture.content)
			schema, err := ontoschema.LoadSchema(root)
			require.NoError(t, err)
			source, err := testkit.NoteMetadataIndexer(t).ProjectNoteSource("notes/source.md", fixture.content, 0)
			require.NoError(t, err)
			indexed, err := BuildIndexFromNoteSources(context.Background(), obsidian.VaultDefinition{Path: root}, []notemeta.NoteSourceSnapshot{source}, schema, "notes")
			require.NoError(t, err)
			snapshot, err := BuildDocumentSnapshot("notes/source.md", fixture.content, time.Time{})
			require.NoError(t, err)
			projected, err := ProjectNodeFromSnapshot(snapshot, schema, NodeRef{NotePath: "notes/source.md", Kind: NodeKindNote})
			require.NoError(t, err)
			current, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes/source.md")
			require.NoError(t, err)
			require.Equal(t, fixture.matches, len(indexed.NoteTypes) > 0)
			require.Equal(t, fixture.matches, projected.Type != nil)
			require.Equal(t, fixture.matches, current.Type != nil)
		})
	}
}

func TestProviderSelectorFactsSurviveMarkdownSnapshotProjection(t *testing.T) {
	root := t.TempDir()
	writeOntologySchema(t, root, `type Subject @node(matches: ["tag:provider-only AND find:ProviderUnique"]) { name: String }`)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	source := mixedFormatSource(t, markdown.New(), "notes/source.md", []byte("# Different source title\n"))
	source.Title, source.Tags = "ProviderUnique", []string{"provider-only"}
	doc, err := projectMarkdownNoteSource(source)
	require.NoError(t, err)
	projected, err := ProjectNodeFromSnapshot(doc.Snapshot, schema, NodeRef{NotePath: doc.Path, Kind: NodeKindNote})
	require.NoError(t, err)
	require.NotNil(t, projected.Type)
	require.Equal(t, "Subject", projected.Type.Name)
}

func TestRootOnlySelectorDoesNotInferMarkdownTags(t *testing.T) {
	root := t.TempDir()
	writeOntologySchema(t, root, `type Subject @node(matches: ["tag:topic"]) { name: String }`)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	source := mixedFormatSource(t, html.New(), "notes/source.html", []byte("<p> #topic </p>"))
	require.Empty(t, source.Tags)
	rootSnapshot, err := BuildRootDocumentSnapshot(source)
	require.NoError(t, err)
	projected, err := ProjectRootDocumentSnapshot(rootSnapshot, schema)
	require.NoError(t, err)
	require.Nil(t, projected.Type)
	indexed, err := BuildIndexFromNoteSources(context.Background(), obsidian.VaultDefinition{Path: root}, []notemeta.NoteSourceSnapshot{source}, schema, "notes")
	require.NoError(t, err)
	require.Empty(t, indexed.NoteTypes)
}
