package identifierreconcile

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestIdentifierFieldDiscoverySealsAdditionalAliasAuthority(t *testing.T) {
	root, _ := identifierFieldDiscoveryFixture(t, map[string]string{
		"specs/loser.md": "---\nid: SPEC-0099\naliases: [SHARED]\nalternate: [SHARED, KEEP]\n---\n",
	})
	owner := ontology.NodeRef{NotePath: "specs/loser.md", Kind: ontology.NodeKindNote, TypeName: "Spec"}
	rewrite := reference.IdentifierRewrite{
		Mode: reference.IdentifierRewriteAliasRemoval, OldRef: owner, NewRef: owner,
		OldIdentifier: "SHARED", NewIdentifier: "SPEC-0099", PreferredField: "id", AliasesField: "aliases",
		AdditionalAliasesFields: []string{"alternate", " aliases ", "alternate"},
	}
	discovery, err := DiscoverIdentifierFields(context.Background(), IdentifierFieldDiscoveryRequest{
		VaultDef: obsidian.VaultDefinition{Path: root}, RootRewrites: []reference.IdentifierRewrite{rewrite},
	})
	require.NoError(t, err)
	snapshot, err := discovery.validatedSnapshotFor([]reference.IdentifierRewrite{rewrite})
	require.NoError(t, err)
	require.Empty(t, snapshot.Diagnostics)
	require.Len(t, snapshot.Edits, 2)
	for _, edit := range snapshot.Edits {
		require.Equal(t, reference.StructuredFieldEditRemove, edit.Operation)
		require.Equal(t, "SHARED", edit.Expected)
	}
	rewrite.AdditionalAliasesFields[0] = "unowned"
	canonical := reference.IdentifierRewrite{
		Mode: reference.IdentifierRewriteAliasRemoval, OldRef: owner, NewRef: owner,
		OldIdentifier: "SHARED", NewIdentifier: "SPEC-0099", PreferredField: "id", AliasesField: "aliases",
		AdditionalAliasesFields: []string{"alternate"},
	}
	snapshot.Rewrites[0].AdditionalAliasesFields[0] = "snapshot-mutated"
	_, err = discovery.validatedSnapshotFor([]reference.IdentifierRewrite{canonical})
	require.NoError(t, err, "caller and returned snapshot cannot mutate sealed authority")
	discovery.rewrites[0].AdditionalAliasesFields[0] = "sealed-mutated"
	_, err = discovery.validatedSnapshotFor([]reference.IdentifierRewrite{canonical})
	require.Error(t, err, "mutated private authority fails its seal")
	canonical.AdditionalAliasesFields = nil
	_, err = discovery.validatedSnapshotFor([]reference.IdentifierRewrite{canonical})
	require.Error(t, err, "narrowing field authority invalidates the reviewed discovery")
	canonical.AdditionalAliasesFields = []string{" "}
	_, err = canonicalRepairRewriteSet([]reference.IdentifierRewrite{canonical})
	require.ErrorContains(t, err, "additional aliases field is empty")
}
