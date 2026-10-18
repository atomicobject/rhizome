package ontology

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestEditSessionFrontmatterEncodingPreservesUntouchedBytes(t *testing.T) {
	for _, tc := range []struct {
		name, eol, bom string
		existing       bool
	}{
		{name: "LF", eol: "\n", existing: true},
		{name: "BOM", eol: "\n", bom: "\xef\xbb\xbf", existing: true},
		{name: "CRLF BOM", eol: "\r\n", bom: "\xef\xbb\xbf", existing: true},
		{name: "new CRLF", eol: "\r\n"},
		{name: "new BOM CRLF", eol: "\r\n", bom: "\xef\xbb\xbf"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeOntologyTestConfig(t, root)
			writeOntologySchema(t, root, `type Spec @node(paths: ["notes/*.md"]) { summary: String }`)
			body := "\n\n# Body\nKeep body.\n"
			frontmatter := ""
			if tc.existing {
				frontmatter = "---\n# keep comment\nsummary: Old # inline\ndate: 2024-01-01\n---\n"
			}
			original := tc.bom + strings.ReplaceAll(frontmatter+body, "\n", tc.eol)
			writeOntologyNote(t, root, "notes/spec.md", original)
			schema, err := ontoschema.LoadSchema(root)
			require.NoError(t, err)
			session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
			require.NoError(t, session.SetScalarField(NodeRef{NotePath: "notes/spec.md", Kind: NodeKindNote}, "summary", "Updated"))
			plan, err := session.Preview(context.Background())
			require.NoError(t, err)
			require.Len(t, plan.Files, 1)
			updated := plan.Files[0].UpdatedContentPreview
			expected := tc.bom + strings.ReplaceAll("---\nsummary: Updated\n---\n"+body, "\n", tc.eol)
			if tc.existing {
				expected = strings.Replace(original, "summary: Old", "summary: Updated", 1)
			}
			require.Equal(t, expected, updated)
			decoded, err := markdownsyntax.ExtractFrontmatter(updated)
			require.NoError(t, err)
			require.Equal(t, "Updated", decoded["summary"])
		})
	}
}

func TestFrontmatterEditsRejectLeadingBlankFrontmatter(t *testing.T) {
	source := "\n---\nsummary: Old\n---\nBody\n"
	_, err := setFrontmatterField(source, &ontoschema.Field{Name: "summary", Source: "summary", TypeName: "String", Kind: ontoschema.FieldKindScalar}, []string{"Updated"}, false)
	require.Error(t, err)
	_, err = unsetFrontmatterField(source, &ontoschema.Field{Name: "summary", Source: "summary", TypeName: "String", Kind: ontoschema.FieldKindScalar})
	require.Error(t, err)
}
