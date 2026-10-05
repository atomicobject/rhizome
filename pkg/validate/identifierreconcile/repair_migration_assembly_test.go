package identifierreconcile

import (
	"sort"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestAssembleRekeyRepairIntentsComposesCollisionAndMigration(t *testing.T) {
	collisionPlan := aliasRepairPlan(t, "SHARED-A")
	collision := collisionPlan.Collisions[0]
	loser := collision.Losers[0]
	collisionRef := repairRef(loser.Claim.Node.NotePath, "", "Spec")
	collisionRewrite := reference.IdentifierRewrite{
		Mode: reference.IdentifierRewriteAliasRemoval, OldRef: collisionRef, NewRef: collisionRef,
		OldIdentifier: loser.Claim.Value, NewIdentifier: "SPEC-0099", PreferredField: "id", AliasesField: "aliases",
	}

	migrationPlan := migrationPlanForAssembly(t, "EFF", "docs/2026-08-06-12-20-effort.md", "^effort", "EFF-0001")
	migrationRewrite := migrationRootRewrite(migrationPlan.Rewrites[0])
	input := rekeyAssemblyInputForTest(t, collisionPlan,
		[]CollisionRepairInput{{CollisionKey: collision.Key, Rewrites: []reference.IdentifierRewrite{collisionRewrite}}},
		[]MigrationRepairInput{{Plan: migrationPlan, Rewrites: []reference.IdentifierRewrite{migrationRewrite}}},
		requiredAliasFieldPlan(collisionRewrite), requiredPreferredFieldPlan(migrationRewrite),
	)

	assembly, err := AssembleRekeyRepairIntents(input)
	require.NoError(t, err)
	require.Len(t, assembly.Components, 2)
	require.NotEmpty(t, assembly.PlanFingerprint)
	require.NotEqual(t, collisionPlan.Fingerprint, assembly.PlanFingerprint)
	components := componentsByMembership(assembly.Components)
	require.Contains(t, components, collision.Key)
	require.Contains(t, components, migrationPlan.Key)
	require.Empty(t, components[migrationPlan.Key].Moves, "embedded migration roots must not synthesize file moves")
	assertMembership(t, collision.Key, components[collision.Key])
	assertMembership(t, migrationPlan.Key, components[migrationPlan.Key])
	_, err = assembly.ValidatedSnapshot()
	require.NoError(t, err)
	embeddedMoveRequest := obsidian.GovernedMoveRequest{SourcePath: migrationRewrite.OldRef.NotePath, OldID: migrationRewrite.OldIdentifier, NewID: migrationRewrite.NewIdentifier}
	embeddedMovePlan, embeddedConflict, err := obsidian.PlanGovernedIdentifierMove(embeddedMoveRequest)
	require.NoError(t, err)
	require.Nil(t, embeddedConflict)
	withEmbeddedMove := input
	withEmbeddedMove.Migrations = append([]MigrationRepairInput(nil), input.Migrations...)
	withEmbeddedMove.Migrations[0].Moves = []GovernedMoveResult{{Request: embeddedMoveRequest, Plan: embeddedMovePlan}}
	_, err = AssembleRekeyRepairIntents(withEmbeddedMove)
	require.ErrorContains(t, err, "extraneous governed move plan")

	assembly.Components[0].Rewrites[0].Rewrite.NewIdentifier = "tampered"
	_, err = assembly.ValidatedSnapshot()
	require.ErrorContains(t, err, "fingerprint")
}

func TestAssembleRekeyRepairIntentsComposesTwoMigrationsAndRequiresNoteMove(t *testing.T) {
	first := migrationPlanForAssembly(t, "EFF", "docs/2026-08-06-12-20-effort.md", "", "EFF-0001")
	second := migrationPlanForAssembly(t, "DEC", "docs/2026-08-06-12-21-decision.md", "^decision", "DEC-0001")
	firstRewrite := migrationRootRewrite(first.Rewrites[0])
	secondRewrite := migrationRootRewrite(second.Rewrites[0])
	movePlan, conflict, err := obsidian.PlanGovernedIdentifierMove(obsidian.GovernedMoveRequest{
		SourcePath: firstRewrite.OldRef.NotePath, OldID: firstRewrite.OldIdentifier, NewID: firstRewrite.NewIdentifier,
	})
	require.NoError(t, err)
	require.Nil(t, conflict)
	input := rekeyAssemblyInputForTest(t, nil, nil, []MigrationRepairInput{
		{Plan: second, Rewrites: []reference.IdentifierRewrite{secondRewrite}},
		{Plan: first, Rewrites: []reference.IdentifierRewrite{firstRewrite}, Moves: []GovernedMoveResult{{
			Request: obsidian.GovernedMoveRequest{SourcePath: firstRewrite.OldRef.NotePath, OldID: firstRewrite.OldIdentifier, NewID: firstRewrite.NewIdentifier},
			Plan:    movePlan,
		}}},
	}, requiredPreferredFieldPlan(firstRewrite), requiredPreferredFieldPlan(secondRewrite))

	assembly, err := AssembleRekeyRepairIntents(input)
	require.NoError(t, err)
	require.Len(t, assembly.Components, 2)
	wantKeys := []string{first.Key, second.Key}
	sort.Strings(wantKeys)
	require.Equal(t, []string{wantKeys[0]}, assembly.Components[0].MembershipKeys)
	require.Equal(t, []string{wantKeys[1]}, assembly.Components[1].MembershipKeys)

	missingMove := input
	for index := range missingMove.Migrations {
		if missingMove.Migrations[index].Plan.Key == first.Key {
			missingMove.Migrations[index].Moves = nil
		}
	}
	_, err = AssembleRekeyRepairIntents(missingMove)
	require.ErrorContains(t, err, "requires exactly one governed move plan")
}

func TestAssembleRekeyRepairIntentsRejectsMigrationPlanAndRootTampering(t *testing.T) {
	plan := migrationPlanForAssembly(t, "EFF", "docs/2026-08-06-12-20-effort.md", "^effort", "EFF-0001")
	rewrite := migrationRootRewrite(plan.Rewrites[0])
	input := rekeyAssemblyInputForTest(t, nil, nil,
		[]MigrationRepairInput{{Plan: plan, Rewrites: []reference.IdentifierRewrite{rewrite}}},
		requiredPreferredFieldPlan(rewrite),
	)

	tamperedRoot := input
	tamperedRoot.Migrations[0].Rewrites = append([]reference.IdentifierRewrite(nil), input.Migrations[0].Rewrites...)
	tamperedRoot.Migrations[0].Rewrites[0].NewIdentifier = "EFF-2026-08-06-12-21"
	_, err := AssembleRekeyRepairIntents(tamperedRoot)
	require.ErrorContains(t, err, "does not match sealed mapping")

	plan.Rewrites[0].NewIdentifier = "EFF-2026-08-06-12-21"
	_, err = AssembleRekeyRepairIntents(input)
	require.ErrorContains(t, err, "fingerprint")
}

func migrationPlanForAssembly(t *testing.T, prefix, notePath, fragment, oldIdentifier string) *MigrationPlan {
	t.Helper()
	node, err := NewCanonicalNodeKey(notePath, fragment, "EffortNote", "id")
	require.NoError(t, err)
	plan, err := BuildMigrationPlan(MigrationInput{
		TargetFormat: ontology.IdentifierFormat{Strategy: ontology.IdentifierStrategyDateTime, Prefix: prefix, Separator: "-"},
		Members:      []MigrationMember{{Node: node, PreferredValue: oldIdentifier, ProspectivePath: notePath}},
	})
	require.NoError(t, err)
	return plan
}

func migrationRootRewrite(planned MigrationRewrite) reference.IdentifierRewrite {
	oldRef := repairRef(planned.Node.NotePath, planned.Node.Fragment, planned.Node.TypeName)
	return reference.IdentifierRewrite{
		Mode: reference.IdentifierRewritePreferredRekey, OldRef: oldRef, NewRef: oldRef,
		OldIdentifier: planned.OldIdentifier, NewIdentifier: planned.NewIdentifier,
		PreferredField: planned.Node.IdentifierField, AliasesField: "aliases",
	}
}

func rekeyAssemblyInputForTest(t *testing.T, plan *Plan, collisions []CollisionRepairInput, migrations []MigrationRepairInput, fieldPlans ...reference.IdentifierFieldRewritePlan) RekeyRepairAssemblyInput {
	t.Helper()
	var rewrites []reference.IdentifierRewrite
	for _, collision := range collisions {
		rewrites = append(rewrites, collision.Rewrites...)
	}
	for _, migration := range migrations {
		rewrites = append(rewrites, migration.Rewrites...)
	}
	discovery := sealFieldDiscoveryForTest(t, rewrites, fieldPlans...)
	completeSources := append([]SourcePrecondition(nil), discovery.sourcePreconditions...)
	sort.Slice(completeSources, func(i, j int) bool { return completeSources[i].NotePath < completeSources[j].NotePath })
	return RekeyRepairAssemblyInput{
		Plan: plan, Collisions: collisions, Migrations: migrations, FieldDiscovery: discovery,
		LinkDiscovery: sealIdentifierLinkDiscoveryForTest(t, rewrites, completeSources, nil),
	}
}

func componentsByMembership(components []CollisionRepairIntent) map[string]CollisionRepairIntent {
	out := make(map[string]CollisionRepairIntent, len(components))
	for _, component := range components {
		out[component.MembershipKeys[0]] = component
	}
	return out
}
