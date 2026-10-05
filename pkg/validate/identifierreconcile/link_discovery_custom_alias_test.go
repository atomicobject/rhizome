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

func TestDiscoverIdentifierLinksIndexesConstructorSealedUndeclaredAliasField(t *testing.T) {
	root := customAliasDiscoveryFixture(t, map[string]string{
		"specs/loser.md":   "---\nid: SPEC-0099\nlegacy_ids: [OLD-CUSTOM]\n---\n",
		"notes/inbound.md": "[[OLD-CUSTOM|OLD-CUSTOM]]\n",
	})
	loser := ontology.NodeRef{NotePath: "specs/loser.md", TypeName: "Spec", Kind: ontology.NodeKindNote}
	rewrite := reference.IdentifierRewrite{
		Mode: reference.IdentifierRewriteAliasRemoval, OldRef: loser, NewRef: loser,
		OldIdentifier: "OLD-CUSTOM", NewIdentifier: "SPEC-0099", PreferredField: "id", AliasesField: "legacy_ids",
	}
	fields, err := DiscoverIdentifierFields(context.Background(), IdentifierFieldDiscoveryRequest{
		VaultDef: obsidian.VaultDefinition{Path: root}, RootRewrites: []reference.IdentifierRewrite{rewrite},
	})
	require.NoError(t, err)
	fieldSnapshot, err := fields.validatedSnapshotFor(fields.rewrites)
	require.NoError(t, err)
	require.True(t, hasFieldRemoval(fieldSnapshot.Edits, "legacy_ids", "OLD-CUSTOM"))

	links, err := DiscoverIdentifierLinks(context.Background(), IdentifierLinkDiscoveryRequest{FieldDiscovery: fields})
	require.NoError(t, err)
	linkSnapshot, err := links.validatedSnapshotFor(fields.rewrites)
	require.NoError(t, err)
	inbound := structuredLinkPlanByPath(t, linkSnapshot.Plans, "notes/inbound.md")
	require.Empty(t, inbound.Diagnostics)
	require.True(t, hasLinkReplacement(inbound.Edits, "OLD-CUSTOM", "SPEC-0099"))
}

func TestDiscoverIdentifierLinksPreservesAmbiguousUndeclaredAliasClaimants(t *testing.T) {
	root := customAliasDiscoveryFixture(t, map[string]string{
		"specs/loser.md":   "---\nid: SPEC-0099\nlegacy_ids: [SHARED-CUSTOM]\n---\n",
		"specs/keeper.md":  "---\nid: SPEC-0001\nlegacy_ids: [SHARED-CUSTOM]\n---\n",
		"notes/inbound.md": "[[SHARED-CUSTOM]]\n",
	})
	loser := ontology.NodeRef{NotePath: "specs/loser.md", TypeName: "Spec", Kind: ontology.NodeKindNote}
	rewrite := reference.IdentifierRewrite{
		Mode: reference.IdentifierRewriteAliasRemoval, OldRef: loser, NewRef: loser,
		OldIdentifier: "SHARED-CUSTOM", NewIdentifier: "SPEC-0099", PreferredField: "id", AliasesField: "legacy_ids",
	}
	fields, err := DiscoverIdentifierFields(context.Background(), IdentifierFieldDiscoveryRequest{
		VaultDef: obsidian.VaultDefinition{Path: root}, RootRewrites: []reference.IdentifierRewrite{rewrite},
	})
	require.NoError(t, err)
	links, err := DiscoverIdentifierLinks(context.Background(), IdentifierLinkDiscoveryRequest{FieldDiscovery: fields})
	require.NoError(t, err)
	snapshot, err := links.validatedSnapshotFor(fields.rewrites)
	require.NoError(t, err)
	inbound := structuredLinkPlanByPath(t, snapshot.Plans, "notes/inbound.md")
	require.Empty(t, inbound.Edits)
	require.Len(t, inbound.Diagnostics, 1)
	require.Equal(t, reference.LinkRewriteDiagnosticAmbiguousTarget, inbound.Diagnostics[0].Kind)
	require.Len(t, inbound.Diagnostics[0].Candidates, 2)
}

func customAliasDiscoveryFixture(t *testing.T, notes map[string]string) string {
	t.Helper()
	root := t.TempDir()
	schemaPath := filepath.Join(root, ".rhizome", "ontology", "schema.graphql")
	require.NoError(t, os.MkdirAll(filepath.Dir(schemaPath), 0o755))
	require.NoError(t, os.WriteFile(schemaPath, []byte(`
type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC")
}
`), 0o644))
	for notePath, content := range notes {
		absolute := filepath.Join(root, filepath.FromSlash(notePath))
		require.NoError(t, os.MkdirAll(filepath.Dir(absolute), 0o755))
		require.NoError(t, os.WriteFile(absolute, []byte(content), 0o644))
	}
	return root
}
