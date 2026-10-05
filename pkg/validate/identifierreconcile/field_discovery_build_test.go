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

func TestDiscoverIdentifierFieldsEnumeratesCompleteVaultAndAliasOnlyRepair(t *testing.T) {
	root, schema := identifierFieldDiscoveryFixture(t, map[string]string{
		"specs/loser.md":   "---\nid: SPEC-0099\naliases:\n  - SHARED-A\n---\n",
		"specs/keeper.md":  "---\nid: SPEC-0001\naliases:\n  - SHARED-A\n---\n",
		"plans/inbound.md": "---\nid: PLAN-0001\nrelated: [SHARED-A]\n---\n",
		"notes/zero.md":    "# No structured fields\n",
	})
	loser := ontology.NodeRef{NotePath: "specs/loser.md", Kind: ontology.NodeKindNote, TypeName: "Spec"}
	rewrite := reference.IdentifierRewrite{
		Mode: reference.IdentifierRewriteAliasRemoval, OldRef: loser, NewRef: loser,
		OldIdentifier: "SHARED-A", NewIdentifier: "SPEC-0099", PreferredField: "id", AliasesField: "aliases",
	}

	discovery, err := DiscoverIdentifierFields(context.Background(), IdentifierFieldDiscoveryRequest{
		VaultDef: obsidian.VaultDefinition{Path: root}, RootRewrites: []reference.IdentifierRewrite{rewrite},
	})
	require.NoError(t, err)
	snapshot, err := discovery.validatedSnapshotFor([]reference.IdentifierRewrite{rewrite})
	require.NoError(t, err)
	require.Equal(t, schema.Hash, snapshot.SchemaHash)
	require.Equal(t, []string{"notes/zero.md", "plans/inbound.md", "specs/keeper.md", "specs/loser.md"}, fieldDiscoverySourcePaths(snapshot.SourcePreconditions))
	require.Len(t, snapshot.Edits, 1)
	require.Equal(t, reference.StructuredFieldAliasIdentifier, snapshot.Edits[0].Kind)
	require.Equal(t, reference.StructuredFieldEditRemove, snapshot.Edits[0].Operation)
	require.Equal(t, "SHARED-A", snapshot.Edits[0].Expected)
	var inbound *reference.StructuredFieldOccurrence
	for index := range snapshot.Occurrences {
		if snapshot.Occurrences[index].OwnerRef.NotePath == "plans/inbound.md" && snapshot.Occurrences[index].FieldName == "related" {
			inbound = &snapshot.Occurrences[index]
			break
		}
	}
	require.NotNil(t, inbound)
	require.Equal(t, reference.StructuredFieldTypedIdentifierReference, inbound.Kind)
	require.Len(t, inbound.Candidates, 2, "alias-only inbound evidence must preserve every claimant")

	discovery.occurrences[0].Value = "mutated"
	_, err = discovery.validatedSnapshotFor([]reference.IdentifierRewrite{rewrite})
	require.ErrorContains(t, err, "changed after sealing")
}

func TestDiscoverIdentifierFieldsRejectsRelevantUnsafeAuthoredShape(t *testing.T) {
	root, _ := identifierFieldDiscoveryFixture(t, map[string]string{
		"specs/loser.md":  "---\nid: 'SPEC-0001'\naliases: [SPEC-0001]\n---\n",
		"specs/keeper.md": "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n",
	})
	oldRef := ontology.NodeRef{NotePath: "specs/loser.md", Kind: ontology.NodeKindNote, TypeName: "Spec"}
	newRef := ontology.NodeRef{NotePath: "specs/loser.md", Kind: ontology.NodeKindNote, TypeName: "Spec"}
	rewrite := reference.IdentifierRewrite{
		Mode: reference.IdentifierRewritePreferredRekey, OldRef: oldRef, NewRef: newRef,
		OldIdentifier: "SPEC-0001", NewIdentifier: "SPEC-0002", PreferredField: "id", AliasesField: "aliases",
	}

	_, err := DiscoverIdentifierFields(context.Background(), IdentifierFieldDiscoveryRequest{
		VaultDef: obsidian.VaultDefinition{Path: root}, RootRewrites: []reference.IdentifierRewrite{rewrite},
	})
	require.ErrorContains(t, err, "not exact")
	require.ErrorContains(t, err, "specs/loser.md")
	require.ErrorContains(t, err, "id")
}

func TestDiscoverIdentifierFieldsUsesOntologyInterfaceSemanticsForTypedLinks(t *testing.T) {
	root := t.TempDir()
	schemaPath := filepath.Join(root, ".rhizome", "ontology", "schema.graphql")
	require.NoError(t, os.MkdirAll(filepath.Dir(schemaPath), 0o755))
	require.NoError(t, os.WriteFile(schemaPath, []byte(`
interface SpecLike {
  id: String!
  aliases: [String!]
}

type ProductSpec implements SpecLike @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true)
  aliases: [String!] @field
}

type Decision @node(paths: ["decisions/*.md"]) {
  id: String! @field @identifier(preferred: true)
  aliases: [String!] @field
}

type Plan @node(paths: ["plans/*.md"]) {
  related: [SpecLike!] @link
}
`), 0o644))
	for notePath, content := range map[string]string{
		"specs/product.md":       "---\nid: SPEC-0001\naliases: [SHARED]\n---\n",
		"decisions/other.md":     "---\nid: DEC-0001\naliases: [SHARED]\n---\n",
		"plans/typed-inbound.md": "---\nrelated: [SHARED]\n---\n",
	} {
		absolute := filepath.Join(root, filepath.FromSlash(notePath))
		require.NoError(t, os.MkdirAll(filepath.Dir(absolute), 0o755))
		require.NoError(t, os.WriteFile(absolute, []byte(content), 0o644))
	}
	product := ontology.NodeRef{NotePath: "specs/product.md", Kind: ontology.NodeKindNote, TypeName: "ProductSpec"}
	rewrite := reference.IdentifierRewrite{
		Mode: reference.IdentifierRewriteAliasRemoval, OldRef: product, NewRef: product,
		OldIdentifier: "SHARED", NewIdentifier: "SPEC-0001", PreferredField: "id", AliasesField: "aliases",
	}
	discovery, err := DiscoverIdentifierFields(context.Background(), IdentifierFieldDiscoveryRequest{
		VaultDef: obsidian.VaultDefinition{Path: root}, RootRewrites: []reference.IdentifierRewrite{rewrite},
	})
	require.NoError(t, err)
	snapshot, err := discovery.validatedSnapshotFor([]reference.IdentifierRewrite{rewrite})
	require.NoError(t, err)
	for _, occurrence := range snapshot.Occurrences {
		if occurrence.OwnerRef.NotePath == "plans/typed-inbound.md" && occurrence.FieldName == "related" {
			require.Len(t, occurrence.Candidates, 1)
			require.True(t, sameRepairRef(product, occurrence.Candidates[0]))
			return
		}
	}
	t.Fatal("missing typed inbound occurrence")
}

func identifierFieldDiscoveryFixture(t *testing.T, notes map[string]string) (string, *ontology.Schema) {
	t.Helper()
	root := t.TempDir()
	schemaPath := filepath.Join(root, ".rhizome", "ontology", "schema.graphql")
	require.NoError(t, os.MkdirAll(filepath.Dir(schemaPath), 0o755))
	require.NoError(t, os.WriteFile(schemaPath, []byte(`
type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true)
  aliases: [String!] @field
}

type Plan @node(paths: ["plans/*.md"]) {
  id: String @field @identifier(preferred: true)
  related: [Spec!] @link
}
`), 0o644))
	for path, content := range notes {
		absolute := filepath.Join(root, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(absolute), 0o755))
		require.NoError(t, os.WriteFile(absolute, []byte(content), 0o644))
	}
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	return root, schema
}

func fieldDiscoverySourcePaths(input []SourcePrecondition) []string {
	out := make([]string, 0, len(input))
	for _, source := range input {
		out = append(out, source.NotePath)
	}
	return out
}
