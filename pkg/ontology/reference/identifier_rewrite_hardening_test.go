package reference

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestIdentifierFieldRewritePlanValidatedSnapshotRejectsMutationAndForgery(t *testing.T) {
	ref := noteRef("docs/specs/a.md", "Spec")
	plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{
		Rewrites: []IdentifierRewrite{{
			OldRef: ref, NewRef: ref, OldIdentifier: "SPEC-0001", NewIdentifier: "SPEC-0002",
			PreferredField: "id", AliasesField: "aliases",
		}},
		Occurrences: []StructuredFieldOccurrence{
			fieldOccurrence(ref, "id", StructuredFieldPreferredIdentifier, "SPEC-0001", 10, 19),
			fieldOccurrence(ref, "aliases", StructuredFieldAliasIdentifier, "SPEC-0001", 30, 39),
		},
	})

	require.NotEmpty(t, plan.Fingerprint)
	snapshot, err := plan.ValidatedSnapshot()
	require.NoError(t, err)
	require.Equal(t, plan, *snapshot)

	plan.Edits[0].Replacement = "FORGED"
	_, err = plan.ValidatedSnapshot()
	require.ErrorContains(t, err, "fingerprint")

	forged := IdentifierFieldRewritePlan{Fingerprint: snapshot.Fingerprint, Edits: append([]StructuredFieldEdit(nil), snapshot.Edits...)}
	_, err = forged.ValidatedSnapshot()
	require.ErrorContains(t, err, "sealed")

	snapshot.Edits[0].Replacement = "MUTATED-COPY"
	require.NotEqual(t, snapshot.Edits[0], plan.Edits[0])
}

func TestPlanIdentifierFieldRewritesTreatsProjectionNodeIDAsNonSemanticEvidence(t *testing.T) {
	semantic := embeddedRef("docs/specs/a.md", "^SPEC-0001-US1", "", "UserStory")
	projected := semantic
	projected.NodeID = "parser-node-42"
	rewrite := IdentifierRewrite{
		OldRef: semantic, NewRef: embeddedRef("docs/specs/b.md", "^SPEC-0002-US1", "", "UserStory"),
		OldIdentifier: "SPEC-0001-US1", NewIdentifier: "SPEC-0002-US1", PreferredField: "id", AliasesField: "aliases",
		DerivedFrom: noteRef("docs/specs/a.md", "Spec"),
	}
	parent := IdentifierRewrite{
		OldRef: rewrite.DerivedFrom, NewRef: noteRef("docs/specs/b.md", "Spec"),
		OldIdentifier: "SPEC-0001", NewIdentifier: "SPEC-0002", PreferredField: "id", AliasesField: "aliases",
	}
	plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{
		Rewrites: []IdentifierRewrite{parent, rewrite},
		Occurrences: []StructuredFieldOccurrence{
			fieldOccurrence(parent.OldRef, "id", StructuredFieldPreferredIdentifier, "SPEC-0001", 1, 10),
			fieldOccurrence(parent.OldRef, "aliases", StructuredFieldAliasIdentifier, "SPEC-0001", 11, 20),
			fieldOccurrence(projected, "id", StructuredFieldPreferredIdentifier, "SPEC-0001-US1", 21, 34),
		},
	})

	for _, edit := range plan.Edits {
		if edit.FieldName == "id" && edit.Expected == "SPEC-0001-US1" {
			require.Equal(t, "SPEC-0002-US1", edit.Replacement)
			return
		}
	}
	t.Fatalf("missing embedded preferred edit: %+v", plan.Diagnostics)
}

func TestPlanIdentifierFieldRewritesPreservesPhysicallyDistinctAmbiguousCandidates(t *testing.T) {
	target := noteRef("docs/specs/a.md", "Spec")
	first := target
	first.NodeID = "physical-a"
	first.Structural = "structural-a"
	second := target
	second.NodeID = "physical-b"
	second.Structural = "structural-b"
	owner := noteRef("docs/consumer.md", "Plan")
	inbound := fieldOccurrence(owner, "spec", StructuredFieldTypedIdentifierReference, "SPEC-0001", 40, 49)
	inbound.Candidates = []ontology.NodeRef{first, second}

	plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{
		Rewrites: []IdentifierRewrite{{
			OldRef: target, NewRef: noteRef("docs/specs/b.md", "Spec"),
			OldIdentifier: "SPEC-0001", NewIdentifier: "SPEC-0002", PreferredField: "id", AliasesField: "aliases",
		}},
		Occurrences: []StructuredFieldOccurrence{
			fieldOccurrence(target, "id", StructuredFieldPreferredIdentifier, "SPEC-0001", 1, 10),
			fieldOccurrence(target, "aliases", StructuredFieldAliasIdentifier, "SPEC-0001", 20, 29),
			inbound,
		},
	})

	for _, edit := range plan.Edits {
		require.NotEqual(t, inbound.Range, edit.Range, "physical ambiguity must block the inbound edit")
	}
	for _, diagnostic := range plan.Diagnostics {
		if diagnostic.Kind == IdentifierRewriteDiagnosticAmbiguousTarget && diagnostic.Range == inbound.Range {
			require.Len(t, diagnostic.Candidates, 2)
			return
		}
	}
	t.Fatalf("missing physical ambiguity diagnostic: %+v", plan.Diagnostics)
}

func TestPlanIdentifierFieldRewritesRejectsInvalidOccurrencePathsAndRanges(t *testing.T) {
	oldRef := noteRef("docs/specs/a.md", "Spec")
	newRef := noteRef("docs/specs/b.md", "Spec")
	rewrite := IdentifierRewrite{
		OldRef: oldRef, NewRef: newRef, OldIdentifier: "SPEC-0001", NewIdentifier: "SPEC-0002",
		PreferredField: "id", AliasesField: "aliases",
	}

	tests := []struct {
		name       string
		occurrence StructuredFieldOccurrence
	}{
		{
			name: "escaping owner",
			occurrence: StructuredFieldOccurrence{
				OwnerRef: noteRef("../outside.md", "Spec"), FieldName: "spec", Kind: StructuredFieldTypedIdentifierReference,
				Value: "SPEC-0001", Range: ontology.ByteRange{Start: 1, End: 10}, Candidates: []ontology.NodeRef{oldRef},
			},
		},
		{
			name: "escaping candidate",
			occurrence: StructuredFieldOccurrence{
				OwnerRef: noteRef("docs/consumer.md", "Plan"), FieldName: "spec", Kind: StructuredFieldTypedIdentifierReference,
				Value: "SPEC-0001", Range: ontology.ByteRange{Start: 1, End: 10}, Candidates: []ontology.NodeRef{noteRef("../outside.md", "Spec")},
			},
		},
		{
			name: "negative range",
			occurrence: StructuredFieldOccurrence{
				OwnerRef: noteRef("docs/consumer.md", "Plan"), FieldName: "spec", Kind: StructuredFieldTypedIdentifierReference,
				Value: "SPEC-0001", Range: ontology.ByteRange{Start: -1, End: 8}, Candidates: []ontology.NodeRef{oldRef},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{Rewrites: []IdentifierRewrite{rewrite}, Occurrences: []StructuredFieldOccurrence{tt.occurrence}})
			require.Empty(t, plan.Edits)
			require.Contains(t, diagnosticKinds(plan.Diagnostics), IdentifierRewriteDiagnosticInvalidInput)
		})
	}
}

func TestPlanIdentifierFieldRewritesBlocksOverlappingPhysicalIntervals(t *testing.T) {
	oldRef := noteRef("docs/specs/a.md", "Spec")
	newRef := noteRef("docs/specs/b.md", "Spec")
	owner := noteRef("docs/consumer.md", "Plan")
	first := fieldOccurrence(owner, "spec", StructuredFieldTypedIdentifierReference, "SPEC-0001", 10, 20)
	first.Candidates = []ontology.NodeRef{oldRef}
	second := fieldOccurrence(owner, "related", StructuredFieldTypedIdentifierReference, "SPEC-0001", 15, 25)
	second.Candidates = []ontology.NodeRef{oldRef}

	plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{
		Rewrites: []IdentifierRewrite{{
			OldRef: oldRef, NewRef: newRef, OldIdentifier: "SPEC-0001", NewIdentifier: "SPEC-0002",
			PreferredField: "id", AliasesField: "aliases",
		}},
		Occurrences: []StructuredFieldOccurrence{first, second},
	})

	require.Empty(t, plan.Edits)
	require.Contains(t, diagnosticKinds(plan.Diagnostics), IdentifierRewriteDiagnosticConflictingEdit)
}

func TestPlanIdentifierFieldRewritesBlocksMultipleSameRefRewriteMatches(t *testing.T) {
	ref := noteRef("docs/specs/a.md", "Spec")
	owner := noteRef("docs/consumer.md", "Plan")
	locator := fieldOccurrence(owner, "spec", StructuredFieldIdentifierBackedLocator, "A-B", 80, 83)
	locator.Candidates = []ontology.NodeRef{ref}

	plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{
		Rewrites: []IdentifierRewrite{
			{Mode: IdentifierRewriteAliasRemoval, OldRef: ref, NewRef: ref, OldIdentifier: "A", NewIdentifier: "SPEC-0001", PreferredField: "id", AliasesField: "aliases"},
			{Mode: IdentifierRewriteAliasRemoval, OldRef: ref, NewRef: ref, OldIdentifier: "A-B", NewIdentifier: "SPEC-0001", PreferredField: "id", AliasesField: "aliases"},
		},
		Occurrences: []StructuredFieldOccurrence{
			fieldOccurrence(ref, "id", StructuredFieldPreferredIdentifier, "SPEC-0001", 10, 19),
			fieldOccurrence(ref, "aliases", StructuredFieldAliasIdentifier, "A", 30, 31),
			fieldOccurrence(ref, "aliases", StructuredFieldAliasIdentifier, "A-B", 32, 35),
			locator,
		},
	})

	require.Contains(t, diagnosticKinds(plan.Diagnostics), IdentifierRewriteDiagnosticConflictingEdit)
	for _, edit := range plan.Edits {
		require.NotEqual(t, owner, edit.OwnerRef)
	}
}

func TestPlanIdentifierFieldRewritesRequiresStructuralDerivedDescent(t *testing.T) {
	parentOld := noteRef("docs/specs/a.md", "Spec")
	parentNew := noteRef("docs/specs/b.md", "Spec")
	baseParent := IdentifierRewrite{
		OldRef: parentOld, NewRef: parentNew, OldIdentifier: "SPEC-0001", NewIdentifier: "SPEC-0002",
		PreferredField: "id", AliasesField: "aliases",
	}

	tests := []struct {
		name  string
		child IdentifierRewrite
	}{
		{
			name: "unrelated source path",
			child: IdentifierRewrite{
				OldRef:        embeddedRef("docs/specs/unrelated.md", "^SPEC-0001-US1", "SPEC-0001-US1", "UserStory"),
				NewRef:        embeddedRef("docs/specs/b.md", "^SPEC-0002-US1", "SPEC-0002-US1", "UserStory"),
				OldIdentifier: "SPEC-0001-US1", NewIdentifier: "SPEC-0002-US1", PreferredField: "id", AliasesField: "aliases", DerivedFrom: parentOld,
			},
		},
		{
			name: "type transition",
			child: IdentifierRewrite{
				OldRef:        embeddedRef("docs/specs/a.md", "^SPEC-0001-US1", "SPEC-0001-US1", "UserStory"),
				NewRef:        embeddedRef("docs/specs/b.md", "^SPEC-0002-US1", "SPEC-0002-US1", "Requirement"),
				OldIdentifier: "SPEC-0001-US1", NewIdentifier: "SPEC-0002-US1", PreferredField: "id", AliasesField: "aliases", DerivedFrom: parentOld,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{Rewrites: []IdentifierRewrite{baseParent, tt.child}})
			require.Contains(t, diagnosticKinds(plan.Diagnostics), IdentifierRewriteDiagnosticInvalidDerivedCascade)
			for _, edit := range plan.Edits {
				require.NotEqual(t, tt.child.OldRef, edit.OwnerRef)
			}
		})
	}
}

func TestPlanIdentifierFieldRewritesInvalidatesDescendantOfConflictingParent(t *testing.T) {
	parentOld := noteRef("docs/specs/a.md", "Spec")
	childOld := embeddedRef("docs/specs/a.md", "^SPEC-0001-US1", "SPEC-0001-US1", "UserStory")
	child := IdentifierRewrite{
		OldRef: childOld, NewRef: embeddedRef("docs/specs/b.md", "^SPEC-0002-US1", "SPEC-0002-US1", "UserStory"),
		OldIdentifier: "SPEC-0001-US1", NewIdentifier: "SPEC-0002-US1", PreferredField: "id", AliasesField: "aliases", DerivedFrom: parentOld,
	}

	plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{Rewrites: []IdentifierRewrite{
		{OldRef: parentOld, NewRef: noteRef("docs/specs/b.md", "Spec"), OldIdentifier: "SPEC-0001", NewIdentifier: "SPEC-0002", PreferredField: "id", AliasesField: "aliases"},
		{OldRef: parentOld, NewRef: noteRef("docs/specs/c.md", "Spec"), OldIdentifier: "SPEC-0009", NewIdentifier: "SPEC-0010", PreferredField: "id", AliasesField: "aliases"},
		child,
	}})

	require.True(t, hasDiagnosticForRef(plan.Diagnostics, IdentifierRewriteDiagnosticInvalidDerivedCascade, childOld))
}

func TestPlanIdentifierFieldRewritesDuplicateRewriteDiagnosticsAreOrderInvariant(t *testing.T) {
	oldRef := noteRef("docs/specs/a.md", "Spec")
	first := IdentifierRewrite{
		OldRef: oldRef, NewRef: noteRef("docs/specs/b.md", "Spec"), OldIdentifier: "SPEC-0001", NewIdentifier: "SPEC-0002",
		PreferredField: "id", AliasesField: "aliases",
	}
	second := IdentifierRewrite{
		OldRef: oldRef, NewRef: noteRef("docs/specs/c.md", "Spec"), OldIdentifier: "spec-0001", NewIdentifier: "SPEC-0003",
		PreferredField: "identifier", AliasesField: "aka",
	}
	second.OldRef.StartByte = 42
	second.OldRef.Structural = "later-snapshot"

	forward := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{Rewrites: []IdentifierRewrite{first, second}})
	reverse := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{Rewrites: []IdentifierRewrite{second, first}})

	require.Equal(t, forward, reverse)
	require.NotEmpty(t, forward.Fingerprint)
}

func TestPlanIdentifierFieldRewritesRejectsAliasRemovalIdentityTransition(t *testing.T) {
	oldRef := noteRef("docs/specs/a.md", "Spec")
	plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{Rewrites: []IdentifierRewrite{{
		Mode:   IdentifierRewriteAliasRemoval,
		OldRef: oldRef, NewRef: noteRef("docs/specs/b.md", "Spec"), OldIdentifier: "SHARED", NewIdentifier: "SPEC-0002",
		PreferredField: "id", AliasesField: "aliases",
	}}})

	require.Contains(t, diagnosticKinds(plan.Diagnostics), IdentifierRewriteDiagnosticInvalidInput)
}
