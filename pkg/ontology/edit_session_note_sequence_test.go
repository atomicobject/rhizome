package ontology

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

const noteSequenceSchema = `
type Client @node(matches: ["tag:organization"]) {
  name: String
}

type Project @node(matches: ["tag:project"]) {
  client: Client @link(sourceKind: FRONTMATTER)
  aliases: [String!] @field
}
`

// Adding a new frontmatter key and changing a relation on the same note must
// both land inside the frontmatter, whatever order they were staged in, even
// when a blank line follows the closing delimiter.
func TestEditSession_NewFieldAndRelationOnOneNoteStayInFrontmatter(t *testing.T) {
	for name, stage := range map[string]func(*EditSession, NodeRef) error{
		"field then relation": func(session *EditSession, ref NodeRef) error {
			if err := session.SetScalarListField(ref, "aliases", []string{"Nabu Three"}); err != nil {
				return err
			}
			return session.SetLinkField(ref, "client", []string{"[[Fetch AI]]"})
		},
		"relation then field": func(session *EditSession, ref NodeRef) error {
			if err := session.SetLinkField(ref, "client", []string{"[[Fetch AI]]"}); err != nil {
				return err
			}
			return session.SetScalarListField(ref, "aliases", []string{"Nabu Three"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeOntologyTestConfig(t, root)
			writeOntologySchema(t, root, noteSequenceSchema)
			writeOntologyNote(t, root, "Notes/USP.md", "---\ntags:\n  - organization\n---\n\nUSP\n")
			writeOntologyNote(t, root, "Notes/Fetch AI.md", "---\ntags:\n  - organization\n---\n\nFetch AI\n")
			writeOntologyNote(t, root, "Notes/Nabu.md", "---\ntags:\n  - project\nclient: \"[[USP]]\"\n---\n\nNabu 3 Project\n")
			schema, err := LoadSchema(root)
			require.NoError(t, err)
			session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
			ref := NodeRef{NotePath: "Notes/Nabu.md", Kind: NodeKindNote}

			require.NoError(t, stage(session, ref))
			plan, err := session.Preview(context.Background())
			require.NoError(t, err)
			require.Len(t, plan.Files, 1)
			require.Equal(t,
				"---\ntags:\n  - project\nclient: \"[[Fetch AI]]\"\naliases: [Nabu Three]\n---\n\nNabu 3 Project\n",
				plan.Files[0].UpdatedContentPreview)
		})
	}
}
