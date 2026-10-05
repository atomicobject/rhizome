package identifierreconcile

import (
	"sort"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestAssembleRepairIntentsRejectsInboundEditAsDerivedIdentityObligation(t *testing.T) {
	plan := preferredRepairPlan(t)
	collision := plan.Collisions[0]
	loser := collision.Losers[0]
	oldRoot := repairRef(loser.Claim.Node.NotePath, "", "Spec")
	newRoot := repairRef("docs/SPEC-0002-b.md", "", "Spec")
	oldChild := repairRef(oldRoot.NotePath, "^SPEC-0001-US1", "UserStory")
	newChild := repairRef(newRoot.NotePath, "^SPEC-0002-US1", "UserStory")
	rewrites := []reference.IdentifierRewrite{
		{Mode: reference.IdentifierRewritePreferredRekey, OldRef: oldRoot, NewRef: newRoot, OldIdentifier: loser.Claim.Value, NewIdentifier: loser.Replacement, PreferredField: "id", AliasesField: "aliases"},
		{Mode: reference.IdentifierRewritePreferredRekey, OldRef: oldChild, NewRef: newChild, OldIdentifier: "SPEC-0001-US1", NewIdentifier: "SPEC-0002-US1", PreferredField: "id", AliasesField: "aliases", DerivedFrom: oldRoot},
	}
	inbound := repairOccurrence(repairRef("docs/consumer.md", "", "Plan"), "stories", reference.StructuredFieldTypedIdentifierReference, "SPEC-0001-US1", 40, 53)
	inbound.Candidates = []ontology.NodeRef{oldChild}
	fieldPlan := reference.PlanIdentifierFieldRewrites(reference.IdentifierFieldRewriteInput{Rewrites: rewrites, Occurrences: []reference.StructuredFieldOccurrence{
		repairOccurrence(oldRoot, "id", reference.StructuredFieldPreferredIdentifier, loser.Claim.Value, 1, 10),
		repairOccurrence(oldRoot, "aliases", reference.StructuredFieldAliasIdentifier, loser.Claim.Value, 11, 20),
		inbound,
	}})

	collisions := []CollisionRepairInput{{
		CollisionKey: collision.Key, Rewrites: rewrites,
		Moves: []GovernedMoveResult{{
			Request: obsidian.GovernedMoveRequest{SourcePath: oldRoot.NotePath, OldID: loser.Claim.Value, NewID: loser.Replacement},
			Plan:    obsidian.GovernedMovePlan{SourcePath: oldRoot.NotePath, DestinationPath: newRoot.NotePath, Renamed: true},
		}},
	}}
	_, err := AssembleRekeyRepairIntents(repairAssemblyInputForTest(t, plan, collisions, fieldPlan))
	require.ErrorContains(t, err, "missing its required descendant field edit")
}

func TestValidateRequiredFieldObligationsUsesRewriteOrderForRoots(t *testing.T) {
	aliasRef := repairRef("docs/alias.md", "", "Spec")
	preferredRef := repairRef("docs/preferred.md", "", "Spec")
	rewrites := []reference.IdentifierRewrite{
		{Mode: reference.IdentifierRewriteAliasRemoval, OldRef: aliasRef, NewRef: aliasRef, OldIdentifier: "SHARED", NewIdentifier: "SPEC-0001", PreferredField: "id", AliasesField: "aliases"},
		{Mode: reference.IdentifierRewritePreferredRekey, OldRef: preferredRef, NewRef: preferredRef, OldIdentifier: "SPEC-0002", NewIdentifier: "SPEC-0003", PreferredField: "id", AliasesField: "aliases"},
	}
	roots := map[int]struct{}{0: {}, 1: {}}

	for range 128 {
		err := validateRequiredFieldObligations(CollisionRepairIntent{}, rewrites, roots, nil)
		require.EqualError(t, err, "alias removal root is missing its required alias edit")
	}
}

func repairAssemblyInputForTest(t *testing.T, plan *Plan, collisions []CollisionRepairInput, fieldPlans ...reference.IdentifierFieldRewritePlan) RekeyRepairAssemblyInput {
	return repairAssemblyInputWithLinksForTest(t, plan, collisions, nil, fieldPlans...)
}

func repairAssemblyInputWithLinksForTest(t *testing.T, plan *Plan, collisions []CollisionRepairInput, linkPlans []reference.StructuredLinkRewritePlan, fieldPlans ...reference.IdentifierFieldRewritePlan) RekeyRepairAssemblyInput {
	t.Helper()
	var rewrites []reference.IdentifierRewrite
	for _, collision := range collisions {
		rewrites = append(rewrites, collision.Rewrites...)
	}
	rootCollisions := append([]CollisionRepairInput(nil), collisions...)
	for index := range rootCollisions {
		rootCollisions[index].Rewrites = nil
		for _, rewrite := range collisions[index].Rewrites {
			if rewrite.DerivedFrom.IsZero() {
				rootCollisions[index].Rewrites = append(rootCollisions[index].Rewrites, rewrite)
			}
		}
	}
	discovery := sealFieldDiscoveryForTest(t, rewrites, fieldPlans...)
	sourceByPath := make(map[string]string)
	for _, source := range discovery.sourcePreconditions {
		sourceByPath[source.NotePath] = source.SourceHash
	}
	for _, raw := range linkPlans {
		linkPlan, err := raw.ValidatedSnapshot()
		require.NoError(t, err)
		sourceByPath[linkPlan.NotePath] = linkPlan.SourceFingerprint
	}
	completeSources := make([]SourcePrecondition, 0, len(sourceByPath))
	for notePath, sourceHash := range sourceByPath {
		completeSources = append(completeSources, SourcePrecondition{NotePath: notePath, SourceHash: sourceHash})
	}
	sort.Slice(completeSources, func(i, j int) bool { return completeSources[i].NotePath < completeSources[j].NotePath })
	discovery.sourcePreconditions = append([]SourcePrecondition(nil), completeSources...)
	discovery.sealed = sealFieldDiscoveryMembersForTest(t, discovery)
	return RekeyRepairAssemblyInput{
		Plan: plan, Collisions: rootCollisions, FieldDiscovery: discovery,
		LinkDiscovery: sealIdentifierLinkDiscoveryForTest(t, rewrites, completeSources, linkPlans),
	}
}

func sealFieldDiscoveryForTest(t *testing.T, rewrites []reference.IdentifierRewrite, fieldPlans ...reference.IdentifierFieldRewritePlan) *IdentifierFieldDiscovery {
	t.Helper()
	canonical, err := canonicalRepairRewriteSet(rewrites)
	require.NoError(t, err)
	rewriteFingerprint, err := repairRewriteSetFingerprint(canonical)
	require.NoError(t, err)
	snapshot := identifierFieldDiscoverySnapshot{Rewrites: canonical, SchemaHash: "test-schema-v1", RewriteFingerprint: rewriteFingerprint}
	sourcePaths := make(map[string]struct{})
	for index := range fieldPlans {
		plan, err := fieldPlans[index].ValidatedSnapshot()
		require.NoError(t, err)
		snapshot.Edits = append(snapshot.Edits, plan.Edits...)
		snapshot.Diagnostics = append(snapshot.Diagnostics, plan.Diagnostics...)
		for _, edit := range plan.Edits {
			sourcePaths[edit.OwnerRef.NotePath] = struct{}{}
		}
		for _, diagnostic := range plan.Diagnostics {
			if !diagnostic.OwnerRef.IsZero() {
				sourcePaths[diagnostic.OwnerRef.NotePath] = struct{}{}
			}
		}
	}
	for notePath := range sourcePaths {
		snapshot.SourcePreconditions = append(snapshot.SourcePreconditions, SourcePrecondition{NotePath: notePath, SourceHash: obsidian.StructuredLinkSourceFingerprint("test-source:" + notePath)})
	}
	sort.Slice(snapshot.SourcePreconditions, func(i, j int) bool {
		return snapshot.SourcePreconditions[i].NotePath < snapshot.SourcePreconditions[j].NotePath
	})
	sort.Slice(snapshot.Edits, func(i, j int) bool { return jsonKey(snapshot.Edits[i]) < jsonKey(snapshot.Edits[j]) })
	sort.Slice(snapshot.Diagnostics, func(i, j int) bool { return jsonKey(snapshot.Diagnostics[i]) < jsonKey(snapshot.Diagnostics[j]) })
	sealed, err := identifierFieldDiscoveryFingerprint(snapshot)
	require.NoError(t, err)
	return &IdentifierFieldDiscovery{
		rewrites:            canonical,
		sourcePreconditions: snapshot.SourcePreconditions, schemaHash: snapshot.SchemaHash, rewriteFingerprint: snapshot.RewriteFingerprint,
		edits: snapshot.Edits, diagnostics: snapshot.Diagnostics, sealed: sealed,
	}
}

func sealFieldDiscoveryMembersForTest(t *testing.T, discovery *IdentifierFieldDiscovery) string {
	t.Helper()
	sealed, err := identifierFieldDiscoveryFingerprint(identifierFieldDiscoverySnapshot{
		Rewrites:            discovery.rewrites,
		SourcePreconditions: discovery.sourcePreconditions, SchemaHash: discovery.schemaHash, RewriteFingerprint: discovery.rewriteFingerprint,
		Edits: discovery.edits, Diagnostics: discovery.diagnostics,
	})
	require.NoError(t, err)
	return sealed
}

func TestAssembleRepairIntentsAcceptsCaseFoldedAuthoredBasenamePathEdit(t *testing.T) {
	plan := preferredRepairPlan(t)
	collision := plan.Collisions[0]
	loser := collision.Losers[0]
	oldRef := repairRef(loser.Claim.Node.NotePath, "", "Spec")
	newRef := repairRef("docs/SPEC-0002-b.md", "", "Spec")
	rewrite := reference.IdentifierRewrite{Mode: reference.IdentifierRewritePreferredRekey, OldRef: oldRef, NewRef: newRef, OldIdentifier: loser.Claim.Value, NewIdentifier: loser.Replacement, PreferredField: "id", AliasesField: "aliases"}
	linkPlan := sealedLinkPlan(t, "docs/consumer.md", "[[spec-0001-b]]", []reference.IdentifierRewrite{rewrite}, []reference.StructuredLinkResolution{{LinkIndex: 0, Candidates: []ontology.NodeRef{oldRef}}})

	collisions := []CollisionRepairInput{{
		CollisionKey: collision.Key, Rewrites: []reference.IdentifierRewrite{rewrite},
		Moves: []GovernedMoveResult{{
			Request: obsidian.GovernedMoveRequest{SourcePath: oldRef.NotePath, OldID: loser.Claim.Value, NewID: loser.Replacement},
			Plan:    obsidian.GovernedMovePlan{SourcePath: oldRef.NotePath, DestinationPath: newRef.NotePath, Renamed: true},
		}},
	}}
	assembly, err := AssembleRekeyRepairIntents(repairAssemblyInputWithLinksForTest(t, plan, collisions, []reference.StructuredLinkRewritePlan{linkPlan}, requiredPreferredFieldPlan(rewrite)))
	require.NoError(t, err)
	require.Len(t, assembly.Components[0].LinkEdits, 1)
	require.Equal(t, "spec-0001-b", assembly.Components[0].LinkEdits[0].Edit.Expected)
	require.Equal(t, "SPEC-0002-b", assembly.Components[0].LinkEdits[0].Edit.Replacement)
}
