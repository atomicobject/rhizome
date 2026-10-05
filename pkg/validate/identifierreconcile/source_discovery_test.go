package identifierreconcile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestProductionDiscoveryDerivesCompleteDescendantUnionAndRewritesFieldsAndLinks(t *testing.T) {
	root := t.TempDir()
	inboundContent := "[[specs/SPEC-0001#^SPEC-0001-US1]] [[specs/SPEC-0001#^SPEC-0001-US1-AC1]]\n[[SPEC-0001#SPEC-0001|SPEC-0001]]\nReview SPEC-0001 in prose and `SPEC-0001` in code.\n"
	writeIdentifierDiscoveryFixture(t, root, map[string]string{
		"specs/SPEC-0001.md": `---
id: SPEC-0001
aliases: [SPEC-0001]
---
# Spec
related:: #^SPEC-0001-US1

## User Stories

### Story One
id:: ^SPEC-0001-US1
^SPEC-0001-US1

#### Acceptance Criteria

- Criterion One. ^SPEC-0001-US1-AC1
`,
		"notes/inbound.md": inboundContent,
	})
	oldRoot := ontology.NodeRef{NotePath: "specs/SPEC-0001.md", TypeName: "Spec", Kind: ontology.NodeKindNote}
	newRoot := ontology.NodeRef{NotePath: "specs/SPEC-0002.md", TypeName: "Spec", Kind: ontology.NodeKindNote}
	rootRewrite := reference.IdentifierRewrite{
		Mode: reference.IdentifierRewritePreferredRekey, OldRef: oldRoot, NewRef: newRoot,
		OldIdentifier: "SPEC-0001", NewIdentifier: "SPEC-0002", PreferredField: "id", AliasesField: "aliases",
	}
	fields, err := DiscoverIdentifierFields(context.Background(), IdentifierFieldDiscoveryRequest{
		VaultDef: obsidian.VaultDefinition{Path: root}, RootRewrites: []reference.IdentifierRewrite{rootRewrite},
	})
	require.NoError(t, err)
	fieldSnapshot, err := fields.validatedSnapshotFor(fields.rewrites)
	require.NoError(t, err)
	require.Len(t, fieldSnapshot.Rewrites, 3, "complete discovery must derive the full embedded identity chain")
	var story, criterion reference.IdentifierRewrite
	for _, rewrite := range fieldSnapshot.Rewrites {
		switch rewrite.OldIdentifier {
		case "SPEC-0001-US1":
			story = rewrite
		case "SPEC-0001-US1-AC1":
			criterion = rewrite
		}
	}
	require.Equal(t, "SPEC-0002-US1", story.NewIdentifier)
	require.Equal(t, "^SPEC-0001-US1", story.OldRef.Fragment)
	require.Equal(t, "^SPEC-0002-US1", story.NewRef.Fragment)
	require.Empty(t, story.OldRef.NodeID, "parser-local NodeID must not enter semantic repair identity")
	projected := story.OldRef
	projected.NodeID = "parser-node-42"
	require.True(t, sameRepairRef(projected, story.OldRef), "an ephemeral projection NodeID must not change semantic identity")
	require.Equal(t, repairRefKey(story.OldRef), repairRefKey(projected))
	require.Equal(t, "SPEC-0002-US1-AC1", criterion.NewIdentifier)
	require.True(t, sameRepairRef(criterion.DerivedFrom, story.OldRef))
	require.True(t, hasFieldReplacement(fieldSnapshot.Edits, "specs/SPEC-0001.md", "id", "^SPEC-0001-US1", "^SPEC-0002-US1"))
	require.True(t, hasFieldReplacement(fieldSnapshot.Edits, "specs/SPEC-0001.md", reference.StructuredFieldBlockLocator, "^SPEC-0001-US1-AC1", "^SPEC-0002-US1-AC1"))
	require.True(t, hasFieldReplacement(fieldSnapshot.Edits, "specs/SPEC-0001.md", "related", "#^SPEC-0001-US1", "#^SPEC-0002-US1"))
	var related *reference.StructuredFieldOccurrence
	for index := range fieldSnapshot.Occurrences {
		if fieldSnapshot.Occurrences[index].FieldName == "related" {
			related = &fieldSnapshot.Occurrences[index]
			break
		}
	}
	require.NotNil(t, related)
	require.Equal(t, reference.StructuredFieldIdentifierBackedLocator, related.Kind)
	require.Len(t, related.Candidates, 1, "the universal Note field must still resolve the exact embedded child")
	require.Equal(t, "specs/SPEC-0001.md", related.Candidates[0].NotePath)
	require.Equal(t, "^SPEC-0001-US1", related.Candidates[0].Fragment)
	require.Equal(t, "UserStory", related.Candidates[0].TypeName)
	require.Equal(t, ontology.NodeKindEmbedded, related.Candidates[0].Kind)
	require.True(t, hasFieldRemoval(fieldSnapshot.Edits, "aliases", "SPEC-0001"), "raw aliases must remain discoverable without a type-declared aliases field")

	links, err := DiscoverIdentifierLinks(context.Background(), IdentifierLinkDiscoveryRequest{FieldDiscovery: fields})
	require.NoError(t, err)
	linkSnapshot, err := links.validatedSnapshotFor(fields.rewrites)
	require.NoError(t, err)
	inbound := structuredLinkPlanByPath(t, linkSnapshot.Plans, "notes/inbound.md")
	require.True(t, hasLinkReplacement(inbound.Edits, "specs/SPEC-0001", "specs/SPEC-0002"))
	require.Truef(t, hasLinkReplacement(inbound.Edits, "^SPEC-0001-US1", "^SPEC-0002-US1"), "missing descendant fragment edit: edits=%+v diagnostics=%+v", inbound.Edits, inbound.Diagnostics)
	require.True(t, hasLinkReplacement(inbound.Edits, "^SPEC-0001-US1-AC1", "^SPEC-0002-US1-AC1"))
	review := identifierReviewPlanByPath(t, linkSnapshot.ReviewPlans, "notes/inbound.md")
	require.Len(t, review.Candidates, 2, "structured link targets must be excluded while prose and inline code remain review-only")

	pool := mustPool(t, "SPEC")
	inventory, err := BuildInventory([]Claim{
		claim(t, pool, "SPEC-0001", ClaimPreferred, "specs/000-keeper.md"),
		claim(t, pool, "SPEC-0001", ClaimPreferred, oldRoot.NotePath),
	})
	require.NoError(t, err)
	plan, err := BuildPlan(inventory, nil)
	require.NoError(t, err)
	require.Equal(t, "SPEC-0002", plan.Collisions[0].Losers[0].Replacement)
	moveRequest := obsidian.GovernedMoveRequest{SourcePath: oldRoot.NotePath, OldID: "SPEC-0001", NewID: "SPEC-0002", SiblingPaths: []string{"SPEC-0001.md"}}
	movePlan, moveConflict, err := obsidian.PlanGovernedIdentifierMove(moveRequest)
	require.NoError(t, err)
	require.Nil(t, moveConflict)
	assembly, err := AssembleRekeyRepairIntents(RekeyRepairAssemblyInput{
		Plan: plan,
		Collisions: []CollisionRepairInput{{
			CollisionKey: plan.Collisions[0].Key, Rewrites: []reference.IdentifierRewrite{rootRewrite},
			Moves: []GovernedMoveResult{{Request: moveRequest, Plan: movePlan}},
		}},
		FieldDiscovery: fields, LinkDiscovery: links,
	})
	require.NoError(t, err)
	require.Len(t, assembly.Components, 1)
	component := assembly.Components[0]
	require.Len(t, component.Rewrites, 3, "assembly must consume sealed descendants even though callers supply roots only")
	require.False(t, component.Blocked)
	require.Len(t, component.FieldEdits, 4)
	require.Len(t, component.AliasEdits, 2)
	require.Len(t, component.LinkEdits, 7)
	require.Len(t, component.Moves, 1)
	assertMembership(t, plan.Collisions[0].Key, component)
	require.Equal(t, []SourcePrecondition{
		{NotePath: "notes/inbound.md", SourceHash: fileSHA256(t, root, "notes/inbound.md")},
		{NotePath: "specs/SPEC-0001.md", SourceHash: fileSHA256(t, root, "specs/SPEC-0001.md")},
	}, assembly.SourcePreconditions)
	headingFragmentRewritten := false
	for _, intent := range component.LinkEdits {
		edit := intent.Edit
		headingFragmentRewritten = headingFragmentRewritten || edit.Component == reference.LinkComponentFragment && edit.Expected == "SPEC-0001" && edit.Replacement == "SPEC-0002"
	}
	require.True(t, headingFragmentRewritten, "the heading-fragment link must be rewritten")
	require.Equal(t, []RepairPostcheck{PostcheckIdentifiers, PostcheckOntology, PostcheckBrokenLinks, PostcheckFragileExternal}, postcheckValues(component.Postchecks),
		"a rewritten heading fragment requires the fragile-link postcheck")
	reviewDiagnostics := 0
	for _, diagnostic := range component.Diagnostics {
		if diagnostic.Kind == string(reference.IdentifierRewriteDiagnosticReviewOnly) {
			require.Equal(t, []string{plan.Collisions[0].Key}, diagnostic.MembershipKeys)
			require.False(t, diagnostic.Blocking)
			if diagnostic.Field != nil && diagnostic.Field.OwnerRef.NotePath == "notes/inbound.md" {
				reviewDiagnostics++
			}
		}
	}
	require.Equal(t, 2, reviewDiagnostics)
	require.NoError(t, assembly.RevalidateCompleteSnapshot(context.Background(), obsidian.VaultDefinition{Path: root}))
	inboundPath := filepath.Join(root, "notes", "inbound.md")
	require.NoError(t, os.WriteFile(inboundPath, []byte(strings.Replace(inboundContent, "US1]]", "US2]]", 1)), 0o644))
	require.ErrorContains(t, assembly.RevalidateCompleteSnapshot(context.Background(), obsidian.VaultDefinition{Path: root}), "source inventory")
	require.NoError(t, os.WriteFile(inboundPath, []byte(inboundContent), 0o644))
	zeroPath := filepath.Join(root, "notes", "zero.md")
	require.NoError(t, os.WriteFile(zeroPath, []byte("no identifiers\n"), 0o644))
	require.ErrorContains(t, assembly.RevalidateCompleteSnapshot(context.Background(), obsidian.VaultDefinition{Path: root}), "source inventory")
}

func TestProductionDiscoveryIgnoresFreeformBlockLocatorsDuringIdentifierRekey(t *testing.T) {
	root := t.TempDir()
	writeIdentifierDiscoveryFixture(t, root, map[string]string{
		"specs/SPEC-0001.md": `---
id: SPEC-0001
aliases: [SPEC-0001]
---
# Spec

## User Stories

### Story One
id:: ^SPEC-0001-US1
^SPEC-0001-US1

#### Acceptance Criteria

- Derived criterion. ^SPEC-0001-US1-AC1
- Freeform anchored criterion. ^freeform
`,
	})
	oldRoot := ontology.NodeRef{NotePath: "specs/SPEC-0001.md", TypeName: "Spec", Kind: ontology.NodeKindNote}
	newRoot := ontology.NodeRef{NotePath: "specs/SPEC-0002.md", TypeName: "Spec", Kind: ontology.NodeKindNote}
	rootRewrite := reference.IdentifierRewrite{
		Mode: reference.IdentifierRewritePreferredRekey, OldRef: oldRoot, NewRef: newRoot,
		OldIdentifier: "SPEC-0001", NewIdentifier: "SPEC-0002", PreferredField: "id", AliasesField: "aliases",
	}

	fields, err := DiscoverIdentifierFields(context.Background(), IdentifierFieldDiscoveryRequest{
		VaultDef: obsidian.VaultDefinition{Path: root}, RootRewrites: []reference.IdentifierRewrite{rootRewrite},
	})
	require.NoError(t, err)
	fieldSnapshot, err := fields.validatedSnapshotFor(fields.rewrites)
	require.NoError(t, err)
	require.Len(t, fieldSnapshot.Rewrites, 3, "freeform block locators must not become derived identifier rewrites")
	require.True(t, hasFieldReplacement(fieldSnapshot.Edits, "specs/SPEC-0001.md", reference.StructuredFieldBlockLocator, "^SPEC-0001-US1-AC1", "^SPEC-0002-US1-AC1"))
}

func TestProductionDiscoveryAndRevalidationUseAuthoritativeDisk(t *testing.T) {
	root, _ := identifierFieldDiscoveryFixture(t, map[string]string{
		"specs/loser.md": "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n",
	})
	ref := ontology.NodeRef{NotePath: "specs/loser.md", TypeName: "Spec", Kind: ontology.NodeKindNote}
	rewrite := reference.IdentifierRewrite{
		Mode: reference.IdentifierRewritePreferredRekey, OldRef: ref, NewRef: ref,
		OldIdentifier: "SPEC-0001", NewIdentifier: "SPEC-0002", PreferredField: "id", AliasesField: "aliases",
	}
	discovery, err := DiscoverIdentifierFields(context.Background(), IdentifierFieldDiscoveryRequest{
		VaultDef: obsidian.VaultDefinition{Path: root}, RootRewrites: []reference.IdentifierRewrite{rewrite},
	})
	require.NoError(t, err)
	require.Len(t, discovery.sourcePreconditions, 1, "an empty lagging cache must not hide the live note")

	assembly := &RepairAssembly{PlanFingerprint: "plan", SchemaHash: discovery.schemaHash, SourcePreconditions: append([]SourcePrecondition(nil), discovery.sourcePreconditions...)}
	require.NoError(t, assembly.seal())
	require.NoError(t, assembly.RevalidateCompleteSnapshot(context.Background(), obsidian.VaultDefinition{Path: root}))
}

func TestProductionDiscoveryDerivesCaseFoldedDescendantIdentifier(t *testing.T) {
	root := t.TempDir()
	writeIdentifierDiscoveryFixture(t, root, map[string]string{
		"specs/SPEC-0001.md": `---
id: SPEC-0001
aliases: [SPEC-0001]
---
# Spec

## User Stories

### Story One
id:: ^spec-0001-us1
^spec-0001-us1
`,
	})
	oldRoot := ontology.NodeRef{NotePath: "specs/SPEC-0001.md", TypeName: "Spec", Kind: ontology.NodeKindNote}
	newRoot := ontology.NodeRef{NotePath: "specs/SPEC-0002.md", TypeName: "Spec", Kind: ontology.NodeKindNote}
	rootRewrite := reference.IdentifierRewrite{
		Mode: reference.IdentifierRewritePreferredRekey, OldRef: oldRoot, NewRef: newRoot,
		OldIdentifier: "SPEC-0001", NewIdentifier: "SPEC-0002", PreferredField: "id", AliasesField: "aliases",
	}

	discovery, err := DiscoverIdentifierFields(context.Background(), IdentifierFieldDiscoveryRequest{
		VaultDef: obsidian.VaultDefinition{Path: root}, RootRewrites: []reference.IdentifierRewrite{rootRewrite},
	})
	require.NoError(t, err)
	snapshot, err := discovery.validatedSnapshotFor(discovery.rewrites)
	require.NoError(t, err)
	for _, rewrite := range snapshot.Rewrites {
		if !rewrite.DerivedFrom.IsZero() {
			require.Equal(t, "spec-0001-us1", rewrite.OldIdentifier)
			require.Equal(t, "SPEC-0002-us1", rewrite.NewIdentifier)
			return
		}
	}
	t.Fatal("missing case-folded descendant rewrite")
}

func fileSHA256(t *testing.T, root, notePath string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(notePath)))
	require.NoError(t, err)
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func writeIdentifierDiscoveryFixture(t *testing.T, root string, notes map[string]string) {
	t.Helper()
	schemaPath := filepath.Join(root, ".rhizome", "ontology", "schema.graphql")
	require.NoError(t, os.MkdirAll(filepath.Dir(schemaPath), 0o755))
	require.NoError(t, os.WriteFile(schemaPath, []byte(`
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
  acceptanceCriteria: AcceptanceCriteriaSection @contains(level: H4, heading: "Acceptance Criteria")
}

type AcceptanceCriterion implements Section @node(locator: EMBEDDED) {
  summary: String! @field(sourceKind: ITEM_SUMMARY)
}

type AcceptanceCriteriaSection implements Section {
  criteria: [AcceptanceCriterion!] @contains(shape: LIST_ITEM)
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC")
  # Universal Note links may carry fragments; Section-derived link targets are forbidden.
  related: Note @link(sourceKind: INLINE)
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`), 0o644))
	for notePath, content := range notes {
		absolute := filepath.Join(root, filepath.FromSlash(notePath))
		require.NoError(t, os.MkdirAll(filepath.Dir(absolute), 0o755))
		require.NoError(t, os.WriteFile(absolute, []byte(content), 0o644))
	}
}

func hasFieldReplacement(edits []reference.StructuredFieldEdit, notePath, field, expected, replacement string) bool {
	for _, edit := range edits {
		if edit.OwnerRef.NotePath == notePath && edit.FieldName == field && edit.Expected == expected && edit.Replacement == replacement {
			return true
		}
	}
	return false
}

func hasFieldRemoval(edits []reference.StructuredFieldEdit, field, expected string) bool {
	for _, edit := range edits {
		if edit.FieldName == field && edit.Operation == reference.StructuredFieldEditRemove && edit.Expected == expected {
			return true
		}
	}
	return false
}

func hasLinkReplacement(edits []reference.StructuredLinkEdit, expected, replacement string) bool {
	for _, edit := range edits {
		if edit.Expected == expected && edit.Replacement == replacement {
			return true
		}
	}
	return false
}

func identifierReviewPlanByPath(t *testing.T, plans []reference.IdentifierReviewCandidatePlan, notePath string) reference.IdentifierReviewCandidatePlan {
	t.Helper()
	for _, plan := range plans {
		if plan.NotePath == notePath {
			return plan
		}
	}
	t.Fatalf("missing identifier review plan for %s", notePath)
	return reference.IdentifierReviewCandidatePlan{}
}
