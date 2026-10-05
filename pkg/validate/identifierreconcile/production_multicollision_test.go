package identifierreconcile

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestProductionDiscoveryAndAssemblyPreserveSameNodeTwoCollisionInboundMembership(t *testing.T) {
	repo := newGitFixture(t)
	const (
		targetPath  = "specs/SPEC-0001-shared.md"
		inboundPath = "notes/shared-inbound.md"
	)
	repo.write(t, ".rhizome/ontology/identifiers.graphql", `
type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC", separator: "-", pad: 4)
}
`)
	repo.write(t, targetPath, "---\nid: SPEC-0001\naliases: [SPEC-0001, SHARED-A]\n---\n# Shared loser\n")
	repo.commit(t, "2026-07-15T09:00:00Z", "add ancestral shared claimant")
	const inboundContent = "[[SPEC-0001]] [[SHARED-A]]\nReview SPEC-0001 and SHARED-A.\n"
	repo.write(t, inboundPath, inboundContent)
	repo.commit(t, "2026-07-15T10:00:00Z", "add shared inbound links")
	repo.write(t, "specs/000-preferred-keeper.md", "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n")
	repo.write(t, "specs/001-alias-keeper.md", "---\nid: SPEC-0099\naliases: [SPEC-0099, SHARED-A]\n---\n")
	repo.commit(t, "2026-07-15T11:00:00Z", "add later collision keepers")

	pool := mustPool(t, "SPEC")
	inventory, err := BuildInventory([]Claim{
		claim(t, pool, "SPEC-0001", ClaimPreferred, "specs/000-preferred-keeper.md"),
		claim(t, pool, "SPEC-0001", ClaimPreferred, targetPath),
		claim(t, pool, "SPEC-0099", ClaimPreferred, "specs/001-alias-keeper.md"),
		claim(t, pool, "SHARED-A", ClaimAlias, "specs/001-alias-keeper.md"),
		claim(t, pool, "SHARED-A", ClaimAlias, targetPath),
	})
	require.NoError(t, err)
	plan, err := BuildPlan(inventory, nil)
	require.NoError(t, err)
	require.Len(t, plan.Collisions, 2)

	oldRef := ontology.NodeRef{NotePath: targetPath, TypeName: "Spec", Kind: ontology.NodeKindNote}
	var preferredReplacement string
	for _, collision := range plan.Collisions {
		for _, loser := range collision.Losers {
			if loser.Claim.Node.NotePath == targetPath && loser.Claim.Kind == ClaimPreferred {
				preferredReplacement = loser.Replacement
			}
		}
	}
	require.NotEmpty(t, preferredReplacement)
	newPath := strings.Replace(targetPath, "SPEC-0001", preferredReplacement, 1)
	newRef := ontology.NodeRef{NotePath: newPath, TypeName: "Spec", Kind: ontology.NodeKindNote}

	inputs := make([]CollisionRepairInput, 0, len(plan.Collisions))
	rewrites := make([]reference.IdentifierRewrite, 0, len(plan.Collisions))
	for _, collision := range plan.Collisions {
		require.Len(t, collision.Losers, 1)
		loser := collision.Losers[0]
		require.Equal(t, targetPath, loser.Claim.Node.NotePath)
		if loser.Claim.Kind == ClaimPreferred {
			rewrite := reference.IdentifierRewrite{
				Mode: reference.IdentifierRewritePreferredRekey, OldRef: oldRef, NewRef: newRef,
				OldIdentifier: loser.Claim.Value, NewIdentifier: preferredReplacement, PreferredField: "id", AliasesField: "aliases",
			}
			moveRequest := obsidian.GovernedMoveRequest{
				SourcePath: targetPath, OldID: loser.Claim.Value, NewID: preferredReplacement,
				SiblingPaths: []string{"000-preferred-keeper.md", "001-alias-keeper.md", "SPEC-0001-shared.md"},
			}
			movePlan, conflict, moveErr := obsidian.PlanGovernedIdentifierMove(moveRequest)
			require.NoError(t, moveErr)
			require.Nil(t, conflict)
			inputs = append(inputs, CollisionRepairInput{
				CollisionKey: collision.Key, Rewrites: []reference.IdentifierRewrite{rewrite},
				Moves: []GovernedMoveResult{{Request: moveRequest, Plan: movePlan}},
			})
			rewrites = append(rewrites, rewrite)
			continue
		}
		rewrite := reference.IdentifierRewrite{
			Mode: reference.IdentifierRewriteAliasRemoval, OldRef: oldRef, NewRef: oldRef,
			OldIdentifier: loser.Claim.Value, NewIdentifier: preferredReplacement, PreferredField: "id", AliasesField: "aliases",
		}
		inputs = append(inputs, CollisionRepairInput{CollisionKey: collision.Key, Rewrites: []reference.IdentifierRewrite{rewrite}})
		rewrites = append(rewrites, rewrite)
	}

	fields, err := DiscoverIdentifierFields(context.Background(), IdentifierFieldDiscoveryRequest{
		VaultDef: obsidian.VaultDefinition{Path: repo.dir}, RootRewrites: rewrites,
	})
	require.NoError(t, err)
	links, err := DiscoverIdentifierLinks(context.Background(), IdentifierLinkDiscoveryRequest{FieldDiscovery: fields})
	require.NoError(t, err)
	assembly, err := AssembleRekeyRepairIntents(RekeyRepairAssemblyInput{
		Plan: plan, Collisions: inputs, FieldDiscovery: fields, LinkDiscovery: links,
	})
	require.NoError(t, err)
	require.NoError(t, assembly.RevalidateCompleteSnapshot(context.Background(), obsidian.VaultDefinition{Path: repo.dir}))
	linkSnapshot, err := links.validatedSnapshotFor(fields.rewrites)
	require.NoError(t, err)
	inboundHash := fileSHA256(t, repo.dir, inboundPath)
	// Real Git ancestry, not a supplied decision, selects the only claimant
	// that existed when each inbound link was introduced.
	require.Len(t, linkSnapshot.ProvenanceDecisions, 2)
	for index, rawTarget := range []string{"SPEC-0001", "SHARED-A"} {
		decision := linkSnapshot.ProvenanceDecisions[index]
		require.Equal(t, inboundPath, decision.NotePath)
		require.Equal(t, inboundHash, decision.SourceHash)
		require.Equal(t, index, decision.LinkIndex)
		require.Equal(t, rawTarget, decision.RawTarget)
		require.True(t, sameRepairRef(oldRef, decision.Winner))
		require.Len(t, decision.Evidence.Candidates, 2)
		for _, candidate := range decision.Evidence.Candidates {
			require.True(t, candidate.Complete)
			require.Equal(t, sameRepairRef(oldRef, candidate.Ref), candidate.Ancestor)
		}
	}
	inboundPlan := structuredLinkPlanByPath(t, linkSnapshot.Plans, inboundPath)
	require.Empty(t, inboundPlan.Diagnostics, "the sealed decision must leave no blocking ambiguity")

	keys := make([]string, 0, len(plan.Collisions))
	for _, collision := range plan.Collisions {
		keys = append(keys, collision.Key)
	}
	sort.Strings(keys)
	var inboundEdits []LinkRepairIntent
	for _, component := range assembly.Components {
		for _, edit := range component.LinkEdits {
			if edit.Edit.NotePath == inboundPath {
				inboundEdits = append(inboundEdits, edit)
			}
		}
	}
	require.Len(t, inboundEdits, 2)
	sort.Slice(inboundEdits, func(i, j int) bool { return inboundEdits[i].Edit.LinkIndex < inboundEdits[j].Edit.LinkIndex })
	require.Equal(t, "SPEC-0001", inboundEdits[0].Edit.Expected)
	require.Equal(t, "SHARED-A", inboundEdits[1].Edit.Expected)
	require.Equal(t, preferredReplacement, inboundEdits[0].Edit.Replacement)
	require.Equal(t, preferredReplacement, inboundEdits[1].Edit.Replacement)
	require.Equal(t, inboundEdits[0].SourceHash, inboundEdits[1].SourceHash, "both links must bind one shared source snapshot")
	require.NotEmpty(t, inboundEdits[0].SourceHash)

	membership := append([]string(nil), inboundEdits[0].MembershipKeys...)
	membership = append(membership, inboundEdits[1].MembershipKeys...)
	sort.Strings(membership)
	require.Equal(t, keys, membership, "each collision membership must survive production discovery and assembly")
	require.Equal(t, inboundHash, inboundEdits[0].SourceHash)
	require.Contains(t, assembly.SourcePreconditions, SourcePrecondition{NotePath: inboundPath, SourceHash: inboundHash})

	// Each collision keeps its own review-only prose evidence on the shared note.
	require.Len(t, assembly.Components, 2)
	reviewedValues := make(map[string]string)
	for _, component := range assembly.Components {
		require.Len(t, component.MembershipKeys, 1)
		var review []RepairDiagnostic
		for _, diagnostic := range component.Diagnostics {
			if diagnostic.Kind == string(reference.IdentifierRewriteDiagnosticReviewOnly) {
				review = append(review, diagnostic)
			}
		}
		require.Len(t, review, 1)
		require.False(t, review[0].Blocking)
		require.Equal(t, component.MembershipKeys, review[0].MembershipKeys)
		require.Equal(t, inboundPath, review[0].Field.OwnerRef.NotePath)
		reviewedValues[component.MembershipKeys[0]] = review[0].Field.Value
	}
	require.ElementsMatch(t, []string{"SPEC-0001", "SHARED-A"}, []string{reviewedValues[keys[0]], reviewedValues[keys[1]]})
}
