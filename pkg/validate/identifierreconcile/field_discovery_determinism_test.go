package identifierreconcile

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	identifierrewrite "github.com/atomicobject/rhizome/pkg/validate/identifierreconcile/rewrite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDiscoverIdentifierFieldsReportsUnsafeCustomAliasFieldsDeterministically(t *testing.T) {
	root := customAliasDiscoveryFixture(t, map[string]string{
		"specs/loser.md": "---\nid: SPEC-0001\nalias_b: 'ALIAS-B'\nalias_a: 'ALIAS-A'\n---\n",
	})
	owner := ontology.NodeRef{NotePath: "specs/loser.md", TypeName: "Spec", Kind: ontology.NodeKindNote}
	rewrites := []identifierrewrite.IdentifierRewrite{
		{
			Mode: identifierrewrite.IdentifierRewriteAliasRemoval, OldRef: owner, NewRef: owner,
			OldIdentifier: "ALIAS-A", NewIdentifier: "SPEC-0001", PreferredField: "id", AliasesField: "alias_a",
		},
		{
			Mode: identifierrewrite.IdentifierRewriteAliasRemoval, OldRef: owner, NewRef: owner,
			OldIdentifier: "ALIAS-B", NewIdentifier: "SPEC-0001", PreferredField: "id", AliasesField: "alias_b",
		},
	}
	want := "structured identifier field Spec.alias_a in specs/loser.md is relevant but its authored value range is not exact"

	// Go randomizes map iteration, so repeat enough that an unsorted map
	// walk fails with near certainty, alternating the input order.
	for i := range 32 {
		order := rewrites
		if i%2 == 1 {
			order = []identifierrewrite.IdentifierRewrite{rewrites[1], rewrites[0]}
		}
		_, err := DiscoverIdentifierFields(context.Background(), IdentifierFieldDiscoveryRequest{NoteMetadata: testNoteMetadata(t),
			VaultDef: obsidian.VaultDefinition{Path: root}, RootRewrites: order,
		})
		require.EqualError(t, err, want)
	}
}
