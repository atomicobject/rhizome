package identifierreconcile

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestProductionAssemblyAcceptsConstructorSealedSynthesizedDescendants(t *testing.T) {
	root := t.TempDir()
	writeIdentifierDiscoveryFixture(t, root, map[string]string{
		"specs/SPEC-0001.md": `---
id: SPEC-0001
aliases: [SPEC-0001]
---
# Spec

## User Stories

### Story One

#### Acceptance Criteria

- Criterion One. ^SPEC-0001-US1-AC1
- Criterion without a locator.
`,
		"notes/inbound.md": "[[SPEC-0001-US1]] [[specs/SPEC-0001#^SPEC-0001-US1-AC1|SPEC-0001.US1.AC1]]\n",
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
	require.Len(t, fieldSnapshot.Rewrites, 3)
	require.Len(t, fieldSnapshot.SynthesizedDerived, 1)
	for _, rewrite := range fieldSnapshot.Rewrites {
		if rewrite.DerivedFrom.IsZero() {
			continue
		}
		if rewrite.PreferredField == reference.StructuredFieldBlockLocator {
			continue
		}
		for _, edit := range fieldSnapshot.Edits {
			require.False(t, requiredDerivedFieldEdit(edit, rewrite), "synthesized identity must not invent an authored field edit")
		}
	}

	links, err := DiscoverIdentifierLinks(context.Background(), IdentifierLinkDiscoveryRequest{FieldDiscovery: fields})
	require.NoError(t, err)
	linkSnapshot, err := links.validatedSnapshotFor(fields.rewrites)
	require.NoError(t, err)
	inbound := structuredLinkPlanByPath(t, linkSnapshot.Plans, "notes/inbound.md")
	require.Truef(t, hasLinkReplacement(inbound.Edits, "SPEC-0001-US1", "SPEC-0002-US1"), "edits=%+v diagnostics=%+v", inbound.Edits, inbound.Diagnostics)
	require.Truef(t, hasLinkReplacement(inbound.Edits, "^SPEC-0001-US1-AC1", "^SPEC-0002-US1-AC1"), "edits=%+v diagnostics=%+v", inbound.Edits, inbound.Diagnostics)

	pool := mustPool(t, "SPEC")
	inventory, err := BuildInventory([]Claim{
		claim(t, pool, "SPEC-0001", ClaimPreferred, "specs/000-keeper.md"),
		claim(t, pool, "SPEC-0001", ClaimPreferred, oldRoot.NotePath),
	})
	require.NoError(t, err)
	plan, err := BuildPlan(inventory, nil)
	require.NoError(t, err)
	moveRequest := obsidian.GovernedMoveRequest{SourcePath: oldRoot.NotePath, OldID: "SPEC-0001", NewID: "SPEC-0002", SiblingPaths: []string{"SPEC-0001.md"}}
	movePlan, conflict, err := obsidian.PlanGovernedIdentifierMove(moveRequest)
	require.NoError(t, err)
	require.Nil(t, conflict)
	assembly, err := AssembleRekeyRepairIntents(RekeyRepairAssemblyInput{
		Plan: plan,
		Collisions: []CollisionRepairInput{{
			CollisionKey: plan.Collisions[0].Key, Rewrites: []reference.IdentifierRewrite{rootRewrite},
			Moves: []GovernedMoveResult{{Request: moveRequest, Plan: movePlan}},
		}},
		FieldDiscovery: fields, LinkDiscovery: links,
	})
	require.NoError(t, err)
	require.Len(t, assembly.Components[0].Rewrites, 3)
	fields.synthesizedDerived[0].Value = "forged"
	_, err = fields.validatedSnapshotFor(fields.rewrites)
	require.ErrorContains(t, err, "changed after sealing")
}
