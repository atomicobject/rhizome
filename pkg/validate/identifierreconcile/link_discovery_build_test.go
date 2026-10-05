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

func TestDiscoverIdentifierLinksSealsEverySourceAndRewritesAliasOnlyInbound(t *testing.T) {
	const (
		loserContent   = "---\nid: SPEC-0099\naliases: [OLD-ALIAS]\n---\n# Loser\n"
		inboundContent = "# Inbound\n\n[[OLD-ALIAS#Missing]]\n"
		zeroContent    = "# Zero\n\nNo links or identifiers here.\n"
	)
	root, schema := identifierLinkDiscoveryFixture(t, map[string]string{
		"specs/loser.md":   loserContent,
		"notes/inbound.md": inboundContent,
		"notes/zero.md":    zeroContent,
	})
	loser := ontology.NodeRef{NotePath: "specs/loser.md", Kind: ontology.NodeKindNote, TypeName: "Spec"}
	rewrite := reference.IdentifierRewrite{
		Mode: reference.IdentifierRewriteAliasRemoval, OldRef: loser, NewRef: loser,
		OldIdentifier: "OLD-ALIAS", NewIdentifier: "SPEC-0099", PreferredField: "id", AliasesField: "aliases",
	}
	fieldDiscovery := discoverIdentifierLinkFields(t, root, schema, rewrite)
	counts := make(map[string]int)
	discovery, err := discoverIdentifierLinks(context.Background(), IdentifierLinkDiscoveryRequest{
		FieldDiscovery: fieldDiscovery,
	}, identifierLinkDiscoveryDependencies{scan: func(content string) obsidian.StructuredLinkScanSnapshot {
		counts[content]++
		return obsidian.ScanStructuredLinkSnapshot(content)
	}})
	require.NoError(t, err)
	snapshot, err := discovery.validatedSnapshotFor([]reference.IdentifierRewrite{rewrite})
	require.NoError(t, err)
	require.Equal(t, schema.Hash, snapshot.SchemaHash)
	require.Equal(t, []string{"notes/inbound.md", "notes/zero.md", "specs/loser.md"}, linkSourcePaths(snapshot.SourcePreconditions))
	require.Equal(t, linkSourcePaths(snapshot.SourcePreconditions), linkPlanPaths(snapshot.Plans), "even zero-link notes require sealed plans")
	require.Len(t, snapshot.ReviewPlans, len(snapshot.SourcePreconditions))
	require.Equal(t, map[string]int{loserContent: 1, inboundContent: 1, zeroContent: 1}, counts,
		"every captured source, including a zero-link note, is structurally scanned exactly once")

	inbound := structuredLinkPlanByPath(t, snapshot.Plans, "notes/inbound.md")
	require.Len(t, inbound.Edits, 1)
	require.Equal(t, "OLD-ALIAS", inbound.Edits[0].Expected)
	require.Equal(t, "SPEC-0099", inbound.Edits[0].Replacement)
	zero := structuredLinkPlanByPath(t, snapshot.Plans, "notes/zero.md")
	require.Empty(t, zero.Edits)
	require.Empty(t, zero.Diagnostics)

	discovery.plans = discovery.plans[:len(discovery.plans)-1]
	_, err = discovery.validatedSnapshotFor([]reference.IdentifierRewrite{rewrite})
	require.ErrorContains(t, err, "changed after sealing")
}

func TestDiscoverIdentifierLinksPreservesAmbiguityWithoutUniqueGitProof(t *testing.T) {
	root, schema := identifierLinkDiscoveryFixture(t, map[string]string{
		"specs/loser.md":   "---\nid: SPEC-0099\naliases: [SHARED]\n---\n",
		"specs/keeper.md":  "---\nid: SPEC-0001\naliases: [SHARED]\n---\n",
		"notes/inbound.md": "[[SHARED]]\n",
	})
	loser := ontology.NodeRef{NotePath: "specs/loser.md", Kind: ontology.NodeKindNote, TypeName: "Spec"}
	rewrite := reference.IdentifierRewrite{
		Mode: reference.IdentifierRewriteAliasRemoval, OldRef: loser, NewRef: loser,
		OldIdentifier: "SHARED", NewIdentifier: "SPEC-0099", PreferredField: "id", AliasesField: "aliases",
	}
	fieldDiscovery := discoverIdentifierLinkFields(t, root, schema, rewrite)
	discovery, err := DiscoverIdentifierLinks(context.Background(), IdentifierLinkDiscoveryRequest{
		FieldDiscovery: fieldDiscovery,
	})
	require.NoError(t, err)
	snapshot, err := discovery.validatedSnapshotFor([]reference.IdentifierRewrite{rewrite})
	require.NoError(t, err)
	inbound := structuredLinkPlanByPath(t, snapshot.Plans, "notes/inbound.md")
	require.Empty(t, inbound.Edits)
	require.Len(t, inbound.Diagnostics, 1)
	require.Equal(t, reference.LinkRewriteDiagnosticAmbiguousTarget, inbound.Diagnostics[0].Kind)
	require.Len(t, inbound.Diagnostics[0].Candidates, 2)
}

func TestDiscoverIdentifierLinksDoesNotUseFragmentExistenceToEraseBaseAmbiguity(t *testing.T) {
	root, schema := identifierLinkDiscoveryFixture(t, map[string]string{
		"specs/loser.md":   "---\nid: SPEC-0099\naliases: [SHARED]\n---\n## Only Loser\n",
		"specs/keeper.md":  "---\nid: SPEC-0001\naliases: [SHARED]\n---\n",
		"notes/inbound.md": "[[SHARED#Only Loser]]\n",
	})
	loser := ontology.NodeRef{NotePath: "specs/loser.md", Kind: ontology.NodeKindNote, TypeName: "Spec"}
	rewrite := reference.IdentifierRewrite{
		Mode: reference.IdentifierRewriteAliasRemoval, OldRef: loser, NewRef: loser,
		OldIdentifier: "SHARED", NewIdentifier: "SPEC-0099", PreferredField: "id", AliasesField: "aliases",
	}
	fieldDiscovery := discoverIdentifierLinkFields(t, root, schema, rewrite)
	discovery, err := DiscoverIdentifierLinks(context.Background(), IdentifierLinkDiscoveryRequest{FieldDiscovery: fieldDiscovery})
	require.NoError(t, err)
	snapshot, err := discovery.validatedSnapshotFor(fieldDiscovery.rewrites)
	require.NoError(t, err)
	inbound := structuredLinkPlanByPath(t, snapshot.Plans, "notes/inbound.md")
	require.Empty(t, inbound.Edits)
	require.Len(t, inbound.Diagnostics, 1)
	require.Equal(t, reference.LinkRewriteDiagnosticAmbiguousTarget, inbound.Diagnostics[0].Kind)
	require.Len(t, inbound.Diagnostics[0].Candidates, 2)
}

func TestDiscoverIdentifierLinksConsumesTheSealedFieldSourceSnapshotWithoutRereading(t *testing.T) {
	root, schema := identifierLinkDiscoveryFixture(t, map[string]string{
		"specs/loser.md":   "---\nid: SPEC-0099\naliases: [OLD-ALIAS]\n---\n",
		"notes/inbound.md": "[[OLD-ALIAS]]\n",
	})
	loser := ontology.NodeRef{NotePath: "specs/loser.md", Kind: ontology.NodeKindNote, TypeName: "Spec"}
	rewrite := reference.IdentifierRewrite{
		Mode: reference.IdentifierRewriteAliasRemoval, OldRef: loser, NewRef: loser,
		OldIdentifier: "OLD-ALIAS", NewIdentifier: "SPEC-0099", PreferredField: "id", AliasesField: "aliases",
	}
	fieldDiscovery := discoverIdentifierLinkFields(t, root, schema, rewrite)
	sealedHash := fileSHA256(t, root, "notes/inbound.md")
	// The live note no longer links anywhere; a reread would find no edit.
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "inbound.md"), []byte("# Link removed\n"), 0o644))

	discovery, err := DiscoverIdentifierLinks(context.Background(), IdentifierLinkDiscoveryRequest{
		FieldDiscovery: fieldDiscovery,
	})
	require.NoError(t, err)
	snapshot, err := discovery.validatedSnapshotFor(fieldDiscovery.rewrites)
	require.NoError(t, err)
	inbound := structuredLinkPlanByPath(t, snapshot.Plans, "notes/inbound.md")
	require.Equal(t, sealedHash, inbound.SourceFingerprint)
	require.Contains(t, snapshot.SourcePreconditions, SourcePrecondition{NotePath: "notes/inbound.md", SourceHash: sealedHash})
	require.Len(t, inbound.Edits, 1)
	require.Equal(t, "OLD-ALIAS", inbound.Edits[0].Expected)
	require.Equal(t, "SPEC-0099", inbound.Edits[0].Replacement)
	require.Equal(t, ontology.ByteRange{Start: 2, End: 11}, inbound.Edits[0].Range)

	// Drift is caught by current-source revalidation, not by rereading.
	assembly := &RepairAssembly{PlanFingerprint: "plan", SchemaHash: snapshot.SchemaHash, SourcePreconditions: snapshot.SourcePreconditions}
	require.NoError(t, assembly.seal())
	require.ErrorContains(t, assembly.RevalidateCompleteSnapshot(context.Background(), obsidian.VaultDefinition{Path: root}), "source inventory changed")
}

func identifierLinkDiscoveryFixture(t *testing.T, notes map[string]string) (string, *ontology.Schema) {
	t.Helper()
	root := t.TempDir()
	schemaPath := filepath.Join(root, ".rhizome", "ontology", "schema.graphql")
	require.NoError(t, os.MkdirAll(filepath.Dir(schemaPath), 0o755))
	require.NoError(t, os.WriteFile(schemaPath, []byte(`
type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC", separator: "-", pad: 4)
  aliases: [String!] @field
}
`), 0o644))
	for notePath, content := range notes {
		absolute := filepath.Join(root, filepath.FromSlash(notePath))
		require.NoError(t, os.MkdirAll(filepath.Dir(absolute), 0o755))
		require.NoError(t, os.WriteFile(absolute, []byte(content), 0o644))
	}
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	return root, schema
}

func discoverIdentifierLinkFields(t *testing.T, root string, schema *ontology.Schema, rewrite reference.IdentifierRewrite) *IdentifierFieldDiscovery {
	t.Helper()
	discovery, err := DiscoverIdentifierFields(context.Background(), IdentifierFieldDiscoveryRequest{
		VaultDef: obsidian.VaultDefinition{Path: root}, RootRewrites: []reference.IdentifierRewrite{rewrite},
	})
	require.NoError(t, err)
	return discovery
}

func structuredLinkPlanByPath(t *testing.T, plans []reference.StructuredLinkRewritePlan, notePath string) reference.StructuredLinkRewritePlan {
	t.Helper()
	for _, plan := range plans {
		if plan.NotePath == notePath {
			return plan
		}
	}
	t.Fatalf("missing structured link plan for %s", notePath)
	return reference.StructuredLinkRewritePlan{}
}
