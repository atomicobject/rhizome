package identifierreconcile

import (
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestAssembleRepairIntentsAcceptsAliasRootToSuppliedPreferred(t *testing.T) {
	plan := aliasRepairPlan(t, "SHARED-A")
	collision := plan.Collisions[0]
	loser := collision.Losers[0]
	ref := repairRef(loser.Claim.Node.NotePath, "", "Spec")
	rewrite := reference.IdentifierRewrite{
		Mode: reference.IdentifierRewriteAliasRemoval, OldRef: ref, NewRef: ref,
		OldIdentifier: loser.Claim.Value, NewIdentifier: "SPEC-0099", PreferredField: "id", AliasesField: "aliases",
	}
	fieldPlan := requiredAliasFieldPlan(rewrite, reference.StructuredFieldOccurrence{OwnerRef: repairRef("docs/consumer.md", "", "Plan"), Kind: reference.StructuredFieldReviewCandidate, Value: loser.Claim.Value, Range: ontology.ByteRange{Start: 50, End: 58}})
	collisions := []CollisionRepairInput{{
		CollisionKey: collision.Key, Rewrites: []reference.IdentifierRewrite{rewrite},
	}}
	assembly, err := AssembleRekeyRepairIntents(repairAssemblyInputForTest(t, plan, collisions, fieldPlan))
	require.NoError(t, err)
	require.Len(t, assembly.Components[0].AliasEdits, 1)
	require.Empty(t, assembly.Components[0].Moves)
	require.False(t, assembly.Components[0].Blocked)
	require.Len(t, assembly.Components[0].Diagnostics, 1)
	require.False(t, assembly.Components[0].Diagnostics[0].Blocking)
	require.Equal(t, []RepairPostcheck{PostcheckIdentifiers, PostcheckOntology, PostcheckBrokenLinks}, postcheckValues(assembly.Components[0].Postchecks))
}

func TestAssembleRepairIntentsRejectsMissingExtraneousAndMutatedPlanInputs(t *testing.T) {
	plan := aliasRepairPlan(t, "SHARED-A")
	collision := plan.Collisions[0]

	_, err := AssembleRekeyRepairIntents(repairAssemblyInputForTest(t, plan, nil))
	require.ErrorContains(t, err, "missing collision input")
	_, err = AssembleRekeyRepairIntents(repairAssemblyInputForTest(t, plan, []CollisionRepairInput{{CollisionKey: "extraneous"}}))
	require.ErrorContains(t, err, "extraneous collision input")

	plan.Collisions[0].Value = "mutated"
	_, err = AssembleRekeyRepairIntents(repairAssemblyInputForTest(t, plan, []CollisionRepairInput{{CollisionKey: collision.Key}}))
	require.ErrorContains(t, err, "mutated")
}

func TestAssembleRepairIntentsRejectsMissingOrMismatchedLoserRoot(t *testing.T) {
	plan := aliasRepairPlan(t, "SHARED-A")
	collision := plan.Collisions[0]
	loser := collision.Losers[0]
	ref := repairRef(loser.Claim.Node.NotePath, "", "Spec")
	base := CollisionRepairInput{CollisionKey: collision.Key}

	_, err := AssembleRekeyRepairIntents(repairAssemblyInputForTest(t, plan, []CollisionRepairInput{base}))
	require.ErrorContains(t, err, "one root rewrite")

	base.Rewrites = []reference.IdentifierRewrite{{
		Mode: reference.IdentifierRewritePreferredRekey, OldRef: ref, NewRef: ref,
		OldIdentifier: loser.Claim.Value, NewIdentifier: "SPEC-0099", PreferredField: "id", AliasesField: "aliases",
	}}
	_, err = AssembleRekeyRepairIntents(repairAssemblyInputForTest(t, plan, []CollisionRepairInput{base}))
	require.ErrorContains(t, err, "alias loser")
}

func TestAssembleRepairIntentsBlocksOnlyConflictingCollisionAndKeepsSharedNoteMembership(t *testing.T) {
	plan := twoAliasCollisionPlan(t)
	inputs := make([]CollisionRepairInput, 0, 2)
	fieldPlans := make([]reference.IdentifierFieldRewritePlan, 0, 2)
	allRewrites := make([]reference.IdentifierRewrite, 0, 2)
	var allResolutions []reference.StructuredLinkResolution
	const sharedContent = "[[SHARED-A]] [[SHARED-A]] [[SHARED-B]]"
	for index, collision := range plan.Collisions {
		loser := collision.Losers[0]
		ref := repairRef(loser.Claim.Node.NotePath, "", "Spec")
		rewrite := reference.IdentifierRewrite{
			Mode: reference.IdentifierRewriteAliasRemoval, OldRef: ref, NewRef: ref,
			OldIdentifier: loser.Claim.Value, NewIdentifier: "SPEC-009" + string(rune('1'+index)), PreferredField: "id", AliasesField: "aliases",
		}
		resolutions := []reference.StructuredLinkResolution{{LinkIndex: 2, Candidates: []ontology.NodeRef{ref}}}
		if index == 0 {
			resolutions = []reference.StructuredLinkResolution{
				{LinkIndex: 0, Candidates: []ontology.NodeRef{ref}},
				{LinkIndex: 1, Candidates: []ontology.NodeRef{ref, repairRef("docs/other.md", "", "Spec")}},
			}
		}
		inputs = append(inputs, CollisionRepairInput{CollisionKey: collision.Key, Rewrites: []reference.IdentifierRewrite{rewrite}})
		fieldPlans = append(fieldPlans, requiredAliasFieldPlan(rewrite))
		allRewrites = append(allRewrites, rewrite)
		allResolutions = append(allResolutions, resolutions...)
	}
	linkPlan := sealedLinkPlan(t, "docs/shared-consumer.md", sharedContent, allRewrites, allResolutions)

	assembly, err := AssembleRekeyRepairIntents(repairAssemblyInputWithLinksForTest(t, plan, inputs, []reference.StructuredLinkRewritePlan{linkPlan}, fieldPlans...))
	require.NoError(t, err)
	require.Len(t, assembly.Components, 2)
	byMembership := componentsByMembership(assembly.Components)
	conflicting, independent := byMembership[plan.Collisions[0].Key], byMembership[plan.Collisions[1].Key]
	require.True(t, conflicting.Blocked)
	require.False(t, independent.Blocked)
	require.Equal(t, "docs/shared-consumer.md", conflicting.LinkEdits[0].Edit.NotePath)
	require.Equal(t, "docs/shared-consumer.md", independent.LinkEdits[0].Edit.NotePath)
	require.Equal(t, []string{plan.Collisions[0].Key}, conflicting.LinkEdits[0].MembershipKeys)
	require.Equal(t, []string{plan.Collisions[1].Key}, independent.LinkEdits[0].MembershipKeys)
}

func TestAssembleRepairIntentsBlocksMoveConflictAndBlockFragmentDoesNotRequestFragileCheck(t *testing.T) {
	plan := preferredRepairPlan(t)
	collision := plan.Collisions[0]
	loser := collision.Losers[0]
	oldRef := repairRef(loser.Claim.Node.NotePath, "", "Spec")
	newRef := repairRef("docs/SPEC-0002-b.md", "", "Spec")
	rewrite := reference.IdentifierRewrite{Mode: reference.IdentifierRewritePreferredRekey, OldRef: oldRef, NewRef: newRef, OldIdentifier: loser.Claim.Value, NewIdentifier: loser.Replacement, PreferredField: "id", AliasesField: "aliases"}
	move := obsidian.GovernedMovePlan{SourcePath: oldRef.NotePath, DestinationPath: newRef.NotePath, Renamed: true}
	conflict := &obsidian.MoveConflict{DestinationPath: newRef.NotePath, ExistingPath: "docs/spec-0002-B.md"}
	linkPlan := sealedLinkPlan(t, "docs/consumer.md", "[[SPEC-0001#^SPEC-0001]]", []reference.IdentifierRewrite{rewrite}, []reference.StructuredLinkResolution{{LinkIndex: 0, Candidates: []ontology.NodeRef{oldRef}}})

	collisions := []CollisionRepairInput{{
		CollisionKey: collision.Key, Rewrites: []reference.IdentifierRewrite{rewrite},
		Moves: []GovernedMoveResult{{Request: obsidian.GovernedMoveRequest{SourcePath: oldRef.NotePath, OldID: "SPEC-0001", NewID: "SPEC-0002", SiblingPaths: []string{conflict.ExistingPath}}, Plan: move, Conflict: conflict}},
	}}
	assembly, err := AssembleRekeyRepairIntents(repairAssemblyInputWithLinksForTest(t, plan, collisions, []reference.StructuredLinkRewritePlan{linkPlan}, requiredPreferredFieldPlan(rewrite)))
	require.NoError(t, err)
	component := assembly.Components[0]
	require.True(t, component.Blocked)
	require.Len(t, component.Moves, 1)
	require.Equal(t, MoveDestinationVacancyPrecondition{
		DestinationPath: newRef.NotePath, RequireExactVacancy: true, RequirePortableCaseFoldVacancy: true,
	}, component.Moves[0].DestinationVacancy)
	require.Len(t, component.Diagnostics, 1)
	require.Equal(t, "MOVE_CONFLICT", component.Diagnostics[0].Kind)
	require.Equal(t, []RepairPostcheck{PostcheckIdentifiers, PostcheckOntology, PostcheckBrokenLinks}, postcheckValues(component.Postchecks))
}

func TestAssembleRepairIntentsRejectsSwappedSameRefAliasMembership(t *testing.T) {
	plan := aliasRepairPlan(t, "SHARED-A")
	collision := plan.Collisions[0]
	loser := collision.Losers[0]
	ref := repairRef(loser.Claim.Node.NotePath, "", "Spec")
	rewrite := reference.IdentifierRewrite{Mode: reference.IdentifierRewriteAliasRemoval, OldRef: ref, NewRef: ref, OldIdentifier: "SHARED-A", NewIdentifier: "SPEC-0099", PreferredField: "id", AliasesField: "aliases"}
	swappedRewrite := rewrite
	swappedRewrite.OldIdentifier = "SHARED-B"

	collisions := []CollisionRepairInput{{
		CollisionKey: collision.Key, Rewrites: []reference.IdentifierRewrite{rewrite},
	}}
	_, err := AssembleRekeyRepairIntents(repairAssemblyInputForTest(t, plan, collisions, requiredAliasFieldPlan(swappedRewrite)))
	require.ErrorContains(t, err, "semantic rewrite membership")
}

func TestAssembleRepairIntentsRejectsInvalidPathsAndRefDrift(t *testing.T) {
	plan := aliasRepairPlan(t, "SHARED-A")
	collision := plan.Collisions[0]
	loser := collision.Losers[0]
	ref := repairRef(loser.Claim.Node.NotePath, "", "Spec")
	base := reference.IdentifierRewrite{Mode: reference.IdentifierRewriteAliasRemoval, OldRef: ref, NewRef: ref, OldIdentifier: loser.Claim.Value, NewIdentifier: "SPEC-0099", PreferredField: "id", AliasesField: "aliases"}

	invalidPath := base
	invalidPath.OldRef.NotePath = "../outside.md"
	_, err := AssembleRekeyRepairIntents(RekeyRepairAssemblyInput{Plan: plan, Collisions: []CollisionRepairInput{{CollisionKey: collision.Key, Rewrites: []reference.IdentifierRewrite{invalidPath}}}, FieldDiscovery: &IdentifierFieldDiscovery{sealed: "test"}})
	require.ErrorContains(t, err, "vault-relative")

	typeDrift := base
	typeDrift.NewRef.TypeName = "Plan"
	typeDriftCollisions := []CollisionRepairInput{{CollisionKey: collision.Key, Rewrites: []reference.IdentifierRewrite{typeDrift}}}
	_, err = AssembleRekeyRepairIntents(repairAssemblyInputForTest(t, plan, typeDriftCollisions))
	require.ErrorContains(t, err, "type and kind")
}

func TestAssembleRepairIntentsCanonicalizesZeroPreferredMode(t *testing.T) {
	plan := preferredRepairPlan(t)
	collision := plan.Collisions[0]
	loser := collision.Losers[0]
	oldRef := repairRef(loser.Claim.Node.NotePath, "", "Spec")
	newRef := repairRef("docs/SPEC-0002-b.md", "", "Spec")
	rewrite := reference.IdentifierRewrite{OldRef: oldRef, NewRef: newRef, OldIdentifier: loser.Claim.Value, NewIdentifier: loser.Replacement, PreferredField: "id", AliasesField: "aliases"}

	collisions := []CollisionRepairInput{{
		CollisionKey: collision.Key, Rewrites: []reference.IdentifierRewrite{rewrite},
		Moves: []GovernedMoveResult{{Request: obsidian.GovernedMoveRequest{SourcePath: oldRef.NotePath, OldID: "SPEC-0001", NewID: "SPEC-0002"}, Plan: obsidian.GovernedMovePlan{SourcePath: oldRef.NotePath, DestinationPath: newRef.NotePath, Renamed: true}}},
	}}
	assembly, err := AssembleRekeyRepairIntents(repairAssemblyInputForTest(t, plan, collisions, requiredPreferredFieldPlan(rewrite)))
	require.NoError(t, err)
	require.Equal(t, reference.IdentifierRewritePreferredRekey, assembly.Components[0].Rewrites[0].Rewrite.Mode)
	require.Equal(t, MoveDestinationVacancyPrecondition{
		DestinationPath: newRef.NotePath, RequireExactVacancy: true, RequirePortableCaseFoldVacancy: true,
	}, assembly.Components[0].Moves[0].DestinationVacancy)
}

func TestAssembleRepairIntentsRejectsCallerChosenUngovernedMoveDestination(t *testing.T) {
	plan := preferredRepairPlan(t)
	collision := plan.Collisions[0]
	loser := collision.Losers[0]
	oldRef := repairRef(loser.Claim.Node.NotePath, "", "Spec")
	evilRef := repairRef("docs/evil.md", "", "Spec")
	rewrite := reference.IdentifierRewrite{Mode: reference.IdentifierRewritePreferredRekey, OldRef: oldRef, NewRef: evilRef, OldIdentifier: loser.Claim.Value, NewIdentifier: loser.Replacement, PreferredField: "id", AliasesField: "aliases"}

	collisions := []CollisionRepairInput{{
		CollisionKey: collision.Key, Rewrites: []reference.IdentifierRewrite{rewrite},
		Moves: []GovernedMoveResult{{
			Request: obsidian.GovernedMoveRequest{SourcePath: oldRef.NotePath, OldID: loser.Claim.Value, NewID: loser.Replacement},
			Plan:    obsidian.GovernedMovePlan{SourcePath: oldRef.NotePath, DestinationPath: evilRef.NotePath, Renamed: true},
		}},
	}}
	_, err := AssembleRekeyRepairIntents(repairAssemblyInputForTest(t, plan, collisions))
	require.ErrorContains(t, err, "rederived governed move")
}

func TestRepairAssemblyValidatedSnapshotRejectsMutationAndReturnsDefensiveCopy(t *testing.T) {
	plan := aliasRepairPlan(t, "SHARED-A")
	collision := plan.Collisions[0]
	loser := collision.Losers[0]
	ref := repairRef(loser.Claim.Node.NotePath, "", "Spec")
	rewrite := reference.IdentifierRewrite{Mode: reference.IdentifierRewriteAliasRemoval, OldRef: ref, NewRef: ref, OldIdentifier: loser.Claim.Value, NewIdentifier: "SPEC-0099", PreferredField: "id", AliasesField: "aliases"}
	collisions := []CollisionRepairInput{{CollisionKey: collision.Key, Rewrites: []reference.IdentifierRewrite{rewrite}}}
	assembly, err := AssembleRekeyRepairIntents(repairAssemblyInputForTest(t, plan, collisions, requiredAliasFieldPlan(rewrite)))
	require.NoError(t, err)

	snapshot, err := assembly.ValidatedSnapshot()
	require.NoError(t, err)
	snapshot.Components[0].MembershipKeys[0] = "caller-mutated-copy"
	require.Equal(t, []string{collision.Key}, assembly.Components[0].MembershipKeys)
	originalSourceHash := assembly.SourcePreconditions[0].SourceHash
	assembly.SourcePreconditions[0].SourceHash = strings.Repeat("0", 64)
	_, err = assembly.ValidatedSnapshot()
	require.ErrorContains(t, err, "source precondition")
	assembly.SourcePreconditions[0].SourceHash = originalSourceHash

	assembly.Components[0].Rewrites[0].MembershipKeys = []string{"wrong-membership"}
	_, err = assembly.ValidatedSnapshot()
	require.ErrorContains(t, err, "membership")

	forged := *snapshot
	forged.sealed = ""
	_, err = forged.ValidatedSnapshot()
	require.ErrorContains(t, err, "not sealed")
}

func TestAssembleRepairIntentsRequiresCompleteFieldDiscoveryAndSealedLinkPlans(t *testing.T) {
	plan := aliasRepairPlan(t, "SHARED-A")
	collision := plan.Collisions[0]
	loser := collision.Losers[0]
	ref := repairRef(loser.Claim.Node.NotePath, "", "Spec")
	rewrite := reference.IdentifierRewrite{Mode: reference.IdentifierRewriteAliasRemoval, OldRef: ref, NewRef: ref, OldIdentifier: loser.Claim.Value, NewIdentifier: "SPEC-0099", PreferredField: "id", AliasesField: "aliases"}

	collisions := []CollisionRepairInput{{CollisionKey: collision.Key, Rewrites: []reference.IdentifierRewrite{rewrite}}}

	t.Run("omitted field discovery", func(t *testing.T) {
		_, err := AssembleRekeyRepairIntents(RekeyRepairAssemblyInput{Plan: plan, Collisions: []CollisionRepairInput{{
			CollisionKey: collision.Key, Rewrites: []reference.IdentifierRewrite{rewrite},
		}}})
		require.ErrorContains(t, err, "complete identifier field discovery")
	})

	t.Run("wrong rewrite union", func(t *testing.T) {
		input := RekeyRepairAssemblyInput{Plan: plan, Collisions: collisions, FieldDiscovery: sealFieldDiscoveryForTest(t, nil)}
		_, err := AssembleRekeyRepairIntents(input)
		require.ErrorContains(t, err, "rewrite union")
	})

	t.Run("link plan", func(t *testing.T) {
		canonical, err := canonicalRepairRewriteSet([]reference.IdentifierRewrite{rewrite})
		require.NoError(t, err)
		rewriteFingerprint, err := repairRewriteSetFingerprint(canonical)
		require.NoError(t, err)
		linkDiscovery := sealRawIdentifierLinkDiscoveryForTest(t, []reference.IdentifierRewrite{rewrite}, nil, []reference.StructuredLinkRewritePlan{{}})
		require.Equal(t, rewriteFingerprint, linkDiscovery.rewriteFingerprint)
		input := RekeyRepairAssemblyInput{
			Plan: plan, Collisions: []CollisionRepairInput{{CollisionKey: collision.Key, Rewrites: []reference.IdentifierRewrite{rewrite}}},
			FieldDiscovery: sealFieldDiscoveryForTest(t, []reference.IdentifierRewrite{rewrite}, requiredAliasFieldPlan(rewrite)),
			LinkDiscovery:  linkDiscovery,
		}
		_, err = AssembleRekeyRepairIntents(input)
		require.ErrorContains(t, err, "changed after planning")
	})
}

func preferredRepairPlan(t *testing.T) *Plan {
	t.Helper()
	pool := mustPool(t, "SPEC")
	inventory, err := BuildInventory([]Claim{
		claim(t, pool, "SPEC-0001", ClaimPreferred, "docs/0-keeper.md"),
		claim(t, pool, "SPEC-0001", ClaimPreferred, "docs/SPEC-0001-b.md"),
	})
	require.NoError(t, err)
	plan, err := BuildPlan(inventory, nil)
	require.NoError(t, err)
	return plan
}

func aliasRepairPlan(t *testing.T, value string) *Plan {
	t.Helper()
	pool := mustPool(t, "SPEC")
	inventory, err := BuildInventory([]Claim{
		claim(t, pool, value, ClaimAlias, "docs/a.md"),
		claim(t, pool, value, ClaimAlias, "docs/b.md"),
	})
	require.NoError(t, err)
	plan, err := BuildPlan(inventory, nil)
	require.NoError(t, err)
	return plan
}

func twoAliasCollisionPlan(t *testing.T) *Plan {
	t.Helper()
	pool := mustPool(t, "SPEC")
	inventory, err := BuildInventory([]Claim{
		claim(t, pool, "SHARED-A", ClaimAlias, "docs/a.md"), claim(t, pool, "SHARED-A", ClaimAlias, "docs/b.md"),
		claim(t, pool, "SHARED-B", ClaimAlias, "docs/c.md"), claim(t, pool, "SHARED-B", ClaimAlias, "docs/d.md"),
	})
	require.NoError(t, err)
	plan, err := BuildPlan(inventory, nil)
	require.NoError(t, err)
	return plan
}

func repairRef(notePath, fragment, typeName string) ontology.NodeRef {
	kind := ontology.NodeKindNote
	if fragment != "" {
		kind = ontology.NodeKindEmbedded
	}
	return ontology.NodeRef{NotePath: notePath, Fragment: fragment, TypeName: typeName, Kind: kind}
}

func assertMembership(t *testing.T, key string, component CollisionRepairIntent) {
	t.Helper()
	for _, item := range component.Rewrites {
		require.Contains(t, item.MembershipKeys, key)
	}
	for _, item := range component.FieldEdits {
		require.Contains(t, item.MembershipKeys, key)
	}
	for _, item := range component.AliasEdits {
		require.Contains(t, item.MembershipKeys, key)
	}
	for _, item := range component.LinkEdits {
		require.Contains(t, item.MembershipKeys, key)
	}
	for _, item := range component.Moves {
		require.Contains(t, item.MembershipKeys, key)
	}
	for _, item := range component.Postchecks {
		require.Contains(t, item.MembershipKeys, key)
	}
}

func postcheckValues(input []RepairPostcheckIntent) []RepairPostcheck {
	out := make([]RepairPostcheck, len(input))
	for index, item := range input {
		out[index] = item.Check
	}
	return out
}

func requiredPreferredFieldPlan(rewrite reference.IdentifierRewrite) reference.IdentifierFieldRewritePlan {
	return reference.PlanIdentifierFieldRewrites(reference.IdentifierFieldRewriteInput{Rewrites: []reference.IdentifierRewrite{rewrite}, Occurrences: []reference.StructuredFieldOccurrence{
		repairOccurrence(rewrite.OldRef, rewrite.PreferredField, reference.StructuredFieldPreferredIdentifier, rewrite.OldIdentifier, 1, 10),
		repairOccurrence(rewrite.OldRef, rewrite.AliasesField, reference.StructuredFieldAliasIdentifier, rewrite.OldIdentifier, 11, 20),
	}})
}

func requiredAliasFieldPlan(rewrite reference.IdentifierRewrite, extra ...reference.StructuredFieldOccurrence) reference.IdentifierFieldRewritePlan {
	occurrences := []reference.StructuredFieldOccurrence{
		repairOccurrence(rewrite.OldRef, rewrite.PreferredField, reference.StructuredFieldPreferredIdentifier, rewrite.NewIdentifier, 1, 10),
		repairOccurrence(rewrite.OldRef, rewrite.AliasesField, reference.StructuredFieldAliasIdentifier, rewrite.OldIdentifier, 11, 20),
	}
	occurrences = append(occurrences, extra...)
	return reference.PlanIdentifierFieldRewrites(reference.IdentifierFieldRewriteInput{Rewrites: []reference.IdentifierRewrite{rewrite}, Occurrences: occurrences})
}

func repairOccurrence(owner ontology.NodeRef, field string, kind reference.StructuredFieldKind, value string, start, end int) reference.StructuredFieldOccurrence {
	return reference.StructuredFieldOccurrence{OwnerRef: owner, FieldName: field, Kind: kind, Value: value, Range: ontology.ByteRange{Start: start, End: end}}
}

func sealedLinkPlan(t *testing.T, notePath, content string, rewrites []reference.IdentifierRewrite, resolutions []reference.StructuredLinkResolution) reference.StructuredLinkRewritePlan {
	t.Helper()
	scan := obsidian.ScanStructuredLinkSnapshot(content)
	return sealedLinkPlanWithSnapshot(t, notePath, content, scan, rewrites, resolutions)
}

func sealedLinkPlanWithSnapshot(t *testing.T, notePath, content string, scan obsidian.StructuredLinkScanSnapshot, rewrites []reference.IdentifierRewrite, resolutions []reference.StructuredLinkResolution) reference.StructuredLinkRewritePlan {
	t.Helper()
	validated, err := scan.ValidatedSnapshot()
	require.NoError(t, err)
	require.NotEmpty(t, validated.Links)
	return reference.PlanStructuredLinkRewrites(reference.StructuredLinkRewriteInput{NotePath: notePath, Content: content, LinkSnapshot: &scan, Rewrites: rewrites, Resolutions: resolutions})
}
