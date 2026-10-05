package identifierreconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestDerivedAliasAuthorityExcludesParentOnlyCustomFields(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		".rhizome/ontology/schema.graphql": `type Spec @node(paths: ["specs/*.md"]) {
 id: String! @field @identifier(preferred: true, prefix: "SPEC")
 stories: Stories @contains(level: H2, heading: "Stories")
}
type Stories implements Section { items: [Story!] @contains(level: H3) }
type Story implements Section @node(locator: EMBEDDED) {
 id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
 aliases: [String!] @field
 parentOnly: [String!] @field
}`,
		"specs/SPEC-0001.md": "---\nid: SPEC-0001\naliases: [SPEC-0001]\nparentOnly: [SPEC-0001]\n---\n# Spec\n\n## Stories\n### First\nid:: ^SPEC-0001-US1\naliases:: SPEC-0001-US1\nparent-only:: CHILD-ORDINARY\n",
		"notes/inbound.md":   "[[CHILD-ORDINARY]]\n",
	}
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	old := ontology.NodeRef{NotePath: "specs/SPEC-0001.md", TypeName: "Spec", Kind: ontology.NodeKindNote}
	next := old
	next.NotePath = "specs/SPEC-0002.md"
	rewrite := reference.IdentifierRewrite{Mode: reference.IdentifierRewritePreferredRekey, OldRef: old, NewRef: next, OldIdentifier: "SPEC-0001", NewIdentifier: "SPEC-0002", PreferredField: "id", AliasesField: "aliases", AdditionalAliasesFields: []string{"parentOnly"}}
	fields, err := DiscoverIdentifierFields(context.Background(), IdentifierFieldDiscoveryRequest{VaultDef: obsidian.VaultDefinition{Path: root}, RootRewrites: []reference.IdentifierRewrite{rewrite}})
	require.NoError(t, err)
	snapshot, err := fields.validatedSnapshotFor(fields.rewrites)
	require.NoError(t, err)
	require.Len(t, snapshot.Rewrites, 2)
	for _, r := range snapshot.Rewrites {
		if !r.DerivedFrom.IsZero() {
			require.Equal(t, "aliases", r.AliasesField)
			require.Empty(t, r.AdditionalAliasesFields)
		}
	}
	require.True(t, hasFieldRemoval(snapshot.Edits, "parentOnly", "SPEC-0001"))
	var childProjected bool
	for _, projection := range fields.source.projections {
		if projection.Ref.Kind == ontology.NodeKindEmbedded && projection.Type != nil && projection.Type.Name == "Story" {
			childProjected = true
			require.Equal(t, []string{"CHILD-ORDINARY"}, projection.Fields["parentOnly"].Values)
		}
	}
	require.True(t, childProjected)
	for _, occ := range snapshot.Occurrences {
		require.False(t, occ.OwnerRef.Kind == ontology.NodeKindEmbedded && occ.FieldName == "parentOnly", "an ordinary child field must not become an alias occurrence")
	}
	links, err := DiscoverIdentifierLinks(context.Background(), IdentifierLinkDiscoveryRequest{FieldDiscovery: fields})
	require.NoError(t, err)
	ls, err := links.validatedSnapshotFor(fields.rewrites)
	require.NoError(t, err)
	require.Empty(t, structuredLinkPlanByPath(t, ls.Plans, "notes/inbound.md").Edits)
	inventory, err := newIdentifierLinkResolutionInventory(fields.source.sources, fields.source.projections, snapshot.Occurrences, snapshot.Rewrites)
	require.NoError(t, err)
	require.Empty(t, inventory.resolve("notes/inbound.md", obsidian.StructuredLink{Kind: obsidian.StructuredLinkWikilink, Path: "CHILD-ORDINARY"}), "the ordinary child value must not resolve as an identity")
}
