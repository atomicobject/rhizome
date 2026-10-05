package identifierreconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdentifierAliasFieldAuthorityIsTypeScoped(t *testing.T) {
	for _, tc := range []struct {
		name              string
		extra, identified bool
		kind, aliasField  string
		candidates        int
	}{
		{"original ordinary alias control", false, false, "Plain", "alias", 1},
		{"additional ordinary alias", true, false, "Plain", "alias", 1},
		{"other identifier owner alias", true, true, "Plain", "alias", 2},
		{"same type owner alias", true, false, "Spec", "alias", 2},
		{"ordinary raw aliases", true, false, "Raw", "alias", 2},
		{"custom ordinary field", true, false, "Plain", "former", 1},
		{"custom same type owner field", true, false, "Spec", "former", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			plainID := ""
			if tc.identified {
				plainID = `id: String! @field @identifier(preferred: true, prefix: "OTHER")`
			}
			schema := `interface Record { title: String }
type Spec implements Record @node(paths: ["specs/*.md"]) {
 id: String! @field @identifier(preferred: true, prefix: "SPEC")
 title: String @field
 aliases: [String!] @field
 alias: [String!] @field
}
type Plain implements Record @node(paths: ["plain/*.md"]) {
 title: String @field
 ` + tc.aliasField + `: [String!] @field
 ` + plainID + `
}
type Consumer @node(paths: ["notes/*.md"]) { related: Record @link }
`
			otherPath := "plain/other.md"
			otherBody := "---\ntitle: Other\n" + tc.aliasField + ": [OLD]\n---\n"
			if tc.identified {
				otherBody = "---\ntitle: Other\nid: OTHER-0001\nalias: [OLD]\n---\n"
			}
			if tc.kind == "Raw" {
				otherBody = "---\ntitle: Other\naliases: [OLD]\n---\n"
			}
			if tc.kind == "Spec" {
				otherPath = "specs/other.md"
				otherBody = "---\nid: SPEC-0001\n" + tc.aliasField + ": [OLD]\n---\n"
			}
			files := map[string]string{".rhizome/ontology/schema.graphql": schema, "specs/loser.md": "---\nid: SPEC-0099\naliases: [OLD]\nalias: [EXTRA]\n---\n", otherPath: otherBody, "notes/inbound.md": "---\nrelated: OLD\n---\n[[OLD]]\n"}
			for name, body := range files {
				p := filepath.Join(root, filepath.FromSlash(name))
				require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
				require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
			}
			owner := ontology.NodeRef{NotePath: "specs/loser.md", TypeName: "Spec", Kind: ontology.NodeKindNote}
			rewrite := reference.IdentifierRewrite{Mode: reference.IdentifierRewriteAliasRemoval, OldRef: owner, NewRef: owner, OldIdentifier: "OLD", NewIdentifier: "SPEC-0099", PreferredField: "id", AliasesField: "aliases"}
			if tc.extra {
				rewrite.AdditionalAliasesFields = []string{tc.aliasField}
			}
			fields, err := DiscoverIdentifierFields(context.Background(), IdentifierFieldDiscoveryRequest{VaultDef: obsidian.VaultDefinition{Path: root}, RootRewrites: []reference.IdentifierRewrite{rewrite}})
			require.NoError(t, err)
			fs, err := fields.validatedSnapshotFor(fields.rewrites)
			require.NoError(t, err)
			var found bool
			for _, occ := range fs.Occurrences {
				if occ.OwnerRef.NotePath == "notes/inbound.md" && occ.FieldName == "related" {
					found = true
					t.Logf("typed candidates=%v", occ.Candidates)
					assert.Len(t, occ.Candidates, tc.candidates)
				}
			}
			require.True(t, found)
			links, err := DiscoverIdentifierLinks(context.Background(), IdentifierLinkDiscoveryRequest{FieldDiscovery: fields})
			require.NoError(t, err)
			ls, err := links.validatedSnapshotFor(fields.rewrites)
			require.NoError(t, err)
			inbound := structuredLinkPlanByPath(t, ls.Plans, "notes/inbound.md")
			if tc.candidates == 1 {
				require.Empty(t, inbound.Diagnostics)
				require.True(t, hasLinkReplacement(inbound.Edits, "OLD", "SPEC-0099"))
			} else {
				require.Len(t, inbound.Diagnostics, 1)
				require.Len(t, inbound.Diagnostics[0].Candidates, 2)
			}
		})
	}
}
