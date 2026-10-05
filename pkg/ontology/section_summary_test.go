package ontology

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestSectionSummaryRetainsStructuralBindingAndIndexesCompactBody(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `type Entry @node(paths: ["notes/*.md"]) {
  abstract: Section @contains(level: H2, heading: "Summary") @display(role: SUMMARY, importance: KEY)
 }`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	field := schema.Types["Entry"].ByName["abstract"]
	require.Equal(t, field, SummaryField(schema.Types["Entry"].Fields))
	docs, err := SchemaDocs(schema, "Entry")
	require.NoError(t, err)
	require.Equal(t, "abstract", docs[0].SummaryField)
	for _, tc := range []struct {
		name, content, want string
		present             bool
	}{
		{"present", "# Entry\n\n## Summary\n\nFirst paragraph.\n\nSecond paragraph.\n\n## Evidence\nUnrelated.\n", "First paragraph. Second paragraph.", true},
		{"empty", "# Entry\n\n## Summary\n\n## Evidence\nUnrelated.\n", "", true},
		{"missing", "# Entry\n\n## Evidence\nUnrelated.\n", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, err := BuildDocumentSnapshot("notes/entry.md", tc.content, time.Time{})
			require.NoError(t, err)
			projection, err := ProjectNodeFromSnapshot(snapshot, schema, NodeRef{NotePath: "notes/entry.md", Kind: NodeKindNote})
			require.NoError(t, err)
			model, err := BuildIntelOntologyNodeReadModel(schema, projection, 1)
			require.NoError(t, err)
			if !tc.present {
				require.Empty(t, model.FieldValues)
				return
			}
			require.Len(t, model.FieldValues, 1)
			require.Equal(t, tc.want, model.FieldValues[0].ValueText)
			require.Equal(t, "abstract", model.FieldValues[0].FieldName)
			binding := projection.Fields["abstract"]
			require.Len(t, binding.SectionNodes, 1)
			require.Contains(t, binding.Values[0], "notes/entry.md#")
			require.NotEqual(t, tc.want, binding.Values[0], "source binding must retain canonical section identity")
		})
	}
}

func TestSectionSummaryEditsThroughNarrativeWorkspace(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `type Entry @node(paths: ["notes/*.md"]) {
 summary: Section @contains(level: H2, heading: "Summary") @display(role: SUMMARY)
 }`)
	content := "---\ntags: [keep]\n---\n# Entry\n\n## Summary\n\nOriginal prose.\n\n## Evidence\n\nPreserved evidence.\n"
	writeOntologyNote(t, root, "notes/entry.md", content)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	vault := obsidian.VaultDefinition{Path: root}
	note, err := ProjectNode(context.Background(), vault, &obsidian.Note{}, schema, NodeRef{NotePath: "notes/entry.md", Kind: NodeKindNote})
	require.NoError(t, err)
	sectionRef := note.Fields["summary"].SectionNodes[0]
	section, err := ProjectNodeFromSnapshot(note.Snapshot, schema, sectionRef)
	require.NoError(t, err)
	blocks := BuildNodeBody(section, schema)
	require.NotEmpty(t, blocks)
	require.Equal(t, NodeBodyBlockKindNarrative, blocks[0].Kind)
	session := NewEditSession(vault, &obsidian.Note{}, schema)
	require.NoError(t, session.SetNarrative(sectionRef, blocks[0].Range.Start, blocks[0].Range.End, blocks[0].Markdown, "Revised prose."))
	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.True(t, result.Applied)
	updated, err := os.ReadFile(filepath.Join(root, "notes/entry.md"))
	require.NoError(t, err)
	require.Contains(t, string(updated), "tags: [keep]")
	require.Contains(t, string(updated), "Revised prose.")
	require.Contains(t, string(updated), "## Evidence\n\nPreserved evidence.")
	require.NotContains(t, string(updated), "Original prose.")
}
