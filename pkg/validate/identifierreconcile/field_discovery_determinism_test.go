package identifierreconcile

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestDiscoverIdentifierFieldsReportsUnsafeCustomAliasFieldsDeterministically(t *testing.T) {
	root := customAliasDiscoveryFixture(t, map[string]string{
		"specs/loser.md": "---\nid: SPEC-0001\nalias_b: 'ALIAS-B'\nalias_a: 'ALIAS-A'\n---\n",
	})
	owner := ontology.NodeRef{NotePath: "specs/loser.md", TypeName: "Spec", Kind: ontology.NodeKindNote}
	rewrites := []reference.IdentifierRewrite{
		{
			Mode: reference.IdentifierRewriteAliasRemoval, OldRef: owner, NewRef: owner,
			OldIdentifier: "ALIAS-A", NewIdentifier: "SPEC-0001", PreferredField: "id", AliasesField: "alias_a",
		},
		{
			Mode: reference.IdentifierRewriteAliasRemoval, OldRef: owner, NewRef: owner,
			OldIdentifier: "ALIAS-B", NewIdentifier: "SPEC-0001", PreferredField: "id", AliasesField: "alias_b",
		},
	}
	want := "structured identifier field Spec.alias_a in specs/loser.md is relevant but its authored value range is not exact"

	// Go randomizes map iteration, so repeat enough that an unsorted map
	// walk fails with near certainty, alternating the input order.
	for i := range 32 {
		order := rewrites
		if i%2 == 1 {
			order = []reference.IdentifierRewrite{rewrites[1], rewrites[0]}
		}
		_, err := DiscoverIdentifierFields(context.Background(), IdentifierFieldDiscoveryRequest{
			VaultDef: obsidian.VaultDefinition{Path: root}, RootRewrites: order,
		})
		require.EqualError(t, err, want)
	}
}
