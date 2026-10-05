package ontology

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestEditSessionSetScalarListFieldPreservesSequenceStyle(t *testing.T) {
	tests := []struct {
		name string
		list string
		want string
	}{
		{name: "flow", list: "aliases: [OLD, KEEP]", want: "aliases: [KEEP, NEW]"},
		{name: "block", list: "aliases:\n  - OLD\n  - KEEP", want: "aliases:\n  - KEEP\n  - NEW"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeOntologyTestConfig(t, root)
			writeOntologySchema(t, root, `
type Spec @node(paths: ["notes/*.md"]) {
  aliases: [String!] @field
}
`)
			content := "---\ntype: Spec\n" + tt.list + "\n---\n# One\n"
			writeOntologyNote(t, root, "notes/one.md", content)
			schema, err := LoadSchema(root)
			require.NoError(t, err)
			session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
			require.NoError(t, session.SetScalarListField(
				NodeRef{NotePath: "notes/one.md", Kind: NodeKindNote},
				"aliases",
				[]string{"KEEP", "NEW"},
			))

			plan, conflicts, err := session.PreviewCurrent(context.Background())
			require.NoError(t, err)
			require.Empty(t, conflicts)
			require.Len(t, plan.Files, 1)
			require.Contains(t, plan.Files[0].UpdatedContentPreview, tt.want)
			onDisk, err := os.ReadFile(filepath.Join(root, "notes/one.md"))
			require.NoError(t, err)
			require.Equal(t, content, string(onDisk))
		})
	}
}

func TestEditSessionSetRawFrontmatterListAllowsUndeclaredNoteRootField(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Spec @node(paths: ["notes/*.md"]) {
  id: String @field
}
`)
	content := "---\ntype: Spec\nid: SPEC-0001\nlegacy_ids: [OLD, KEEP]\n---\n# One\n"
	writeOntologyNote(t, root, "notes/one.md", content)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetRawFrontmatterList(
		NodeRef{NotePath: "notes/one.md", Kind: NodeKindNote},
		"legacy_ids",
		[]string{"KEEP", "NEW"},
	))

	plan, conflicts, err := session.PreviewCurrent(context.Background())
	require.NoError(t, err)
	require.Empty(t, conflicts)
	require.Len(t, plan.Files, 1)
	require.Contains(t, plan.Files[0].UpdatedContentPreview, "legacy_ids: [KEEP, NEW]")
	onDisk, err := os.ReadFile(filepath.Join(root, "notes/one.md"))
	require.NoError(t, err)
	require.Equal(t, content, string(onDisk))
}

func TestEditSessionSetScalarListFieldAcceptsEnumLists(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
enum Label { alpha beta gamma }
type Spec @node(paths: ["notes/*.md"]) {
  labels: [Label!] @field
}
`)
	writeOntologyNote(t, root, "notes/one.md", "---\ntype: Spec\nlabels: [alpha]\n---\n# One\n")
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarListField(
		NodeRef{NotePath: "notes/one.md", Kind: NodeKindNote},
		"labels",
		[]string{"beta", "gamma"},
	))

	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Contains(t, plan.Files[0].UpdatedContentPreview, "labels: [beta, gamma]")
}
