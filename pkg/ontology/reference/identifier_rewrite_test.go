package reference

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestPlanIdentifierFieldRewritesMigratesPreferredAndAliases(t *testing.T) {
	oldRef := noteRef("docs/specs/duplicate.md", "Spec")
	newRef := noteRef("docs/specs/SPEC-0081-duplicate.md", "Spec")

	plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{
		Rewrites: []IdentifierRewrite{{
			OldRef: oldRef, NewRef: newRef,
			OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0081",
			PreferredField: "id", AliasesField: "aliases",
		}},
		Occurrences: []StructuredFieldOccurrence{
			fieldOccurrence(oldRef, "id", StructuredFieldPreferredIdentifier, "SPEC-0075", 10, 19),
			fieldOccurrence(oldRef, "aliases", StructuredFieldAliasIdentifier, "spec-0075", 30, 39),
			fieldOccurrence(oldRef, "aliases", StructuredFieldAliasIdentifier, "identifier-strategy", 40, 59),
		},
	})

	require.Empty(t, plan.Diagnostics)
	require.Equal(t, []StructuredFieldEdit{
		{OwnerRef: oldRef, FieldName: "id", Kind: StructuredFieldPreferredIdentifier, Operation: StructuredFieldEditReplace, Range: ontology.ByteRange{Start: 10, End: 19}, Expected: "SPEC-0075", Replacement: "SPEC-0081", TargetOldRef: oldRef, TargetNewRef: newRef},
		{OwnerRef: oldRef, FieldName: "aliases", Kind: StructuredFieldAliasIdentifier, Operation: StructuredFieldEditRemove, Range: ontology.ByteRange{Start: 30, End: 39}, Expected: "spec-0075", TargetOldRef: oldRef, TargetNewRef: newRef},
		{OwnerRef: oldRef, FieldName: "aliases", Kind: StructuredFieldAliasIdentifier, Operation: StructuredFieldEditAppend, Replacement: "SPEC-0081", TargetOldRef: oldRef, TargetNewRef: newRef},
	}, plan.Edits)
}

func TestPlanIdentifierFieldRewritesDoesNotDuplicateAliasMirror(t *testing.T) {
	oldRef := noteRef("docs/specs/duplicate.md", "Spec")
	newRef := noteRef("docs/specs/rekeyed.md", "Spec")
	plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{
		Rewrites: []IdentifierRewrite{{
			OldRef: oldRef, NewRef: newRef,
			OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0081",
			PreferredField: "id", AliasesField: "aliases",
		}},
		Occurrences: []StructuredFieldOccurrence{
			fieldOccurrence(oldRef, "id", StructuredFieldPreferredIdentifier, "SPEC-0075", 1, 10),
			fieldOccurrence(oldRef, "aliases", StructuredFieldAliasIdentifier, "SPEC-0075", 11, 20),
			fieldOccurrence(oldRef, "aliases", StructuredFieldAliasIdentifier, "spec-0081", 21, 30),
		},
	})

	require.Empty(t, plan.Diagnostics)
	require.Len(t, plan.Edits, 2)
	require.Equal(t, StructuredFieldEditRemove, plan.Edits[1].Operation)
}

func TestPlanIdentifierFieldRewritesPreferredRekeyCanKeepCanonicalRef(t *testing.T) {
	ref := noteRef("docs/specs/stable-filename.md", "Spec")
	plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{
		Rewrites: []IdentifierRewrite{{
			OldRef: ref, NewRef: ref,
			OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0081",
			PreferredField: "id", AliasesField: "aliases",
		}},
		Occurrences: []StructuredFieldOccurrence{
			fieldOccurrence(ref, "id", StructuredFieldPreferredIdentifier, "SPEC-0075", 10, 19),
			fieldOccurrence(ref, "aliases", StructuredFieldAliasIdentifier, "SPEC-0075", 30, 39),
		},
	})

	require.Empty(t, plan.Diagnostics)
	require.Len(t, plan.Edits, 3)
	require.Equal(t, "SPEC-0081", plan.Edits[0].Replacement)
}

func TestPlanIdentifierFieldRewritesRemovesAliasCollisionWithoutRekeyingOwner(t *testing.T) {
	loserRef := noteRef("docs/specs/loser.md", "Spec")
	sourceRef := noteRef("docs/efforts/consumer.md", "Effort")
	inbound := fieldOccurrence(sourceRef, "specs", StructuredFieldTypedIdentifierReference, "SHARED-ALIAS", 90, 102)
	inbound.Candidates = []ontology.NodeRef{loserRef}

	plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{
		Rewrites: []IdentifierRewrite{{
			Mode:   IdentifierRewriteAliasRemoval,
			OldRef: loserRef, NewRef: loserRef,
			OldIdentifier: "SHARED-ALIAS", NewIdentifier: "SPEC-0081",
			PreferredField: "id", AliasesField: "aliases",
		}},
		Occurrences: []StructuredFieldOccurrence{
			fieldOccurrence(loserRef, "id", StructuredFieldPreferredIdentifier, "SPEC-0081", 10, 19),
			fieldOccurrence(loserRef, "aliases", StructuredFieldAliasIdentifier, "shared-alias", 30, 42),
			inbound,
		},
	})

	require.Empty(t, plan.Diagnostics)
	require.Equal(t, []StructuredFieldEdit{
		{OwnerRef: sourceRef, FieldName: "specs", Kind: StructuredFieldTypedIdentifierReference, Operation: StructuredFieldEditReplace, Range: ontology.ByteRange{Start: 90, End: 102}, Expected: "SHARED-ALIAS", Replacement: "SPEC-0081", TargetOldRef: loserRef, TargetNewRef: loserRef},
		{OwnerRef: loserRef, FieldName: "aliases", Kind: StructuredFieldAliasIdentifier, Operation: StructuredFieldEditRemove, Range: ontology.ByteRange{Start: 30, End: 42}, Expected: "shared-alias", TargetOldRef: loserRef, TargetNewRef: loserRef},
	}, plan.Edits)
}

func TestPlanIdentifierFieldRewritesPreservesUnicodeFoldedLocatorBytes(t *testing.T) {
	loserRef := noteRef("docs/specs/loser.md", "Spec")
	sourceRef := noteRef("docs/efforts/consumer.md", "Effort")
	locator := fieldOccurrence(sourceRef, "spec", StructuredFieldIdentifierBackedLocator, "#^K-child", 70, 81)
	locator.Candidates = []ontology.NodeRef{loserRef}

	plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{
		Rewrites: []IdentifierRewrite{{
			Mode: IdentifierRewriteAliasRemoval, OldRef: loserRef, NewRef: loserRef,
			OldIdentifier: "K", NewIdentifier: "SPEC-0002", PreferredField: "id", AliasesField: "aliases",
		}},
		Occurrences: []StructuredFieldOccurrence{
			fieldOccurrence(loserRef, "id", StructuredFieldPreferredIdentifier, "SPEC-0002", 10, 19),
			fieldOccurrence(loserRef, "aliases", StructuredFieldAliasIdentifier, "K", 30, 33),
			locator,
		},
	})

	require.Empty(t, plan.Diagnostics)
	require.Equal(t, []StructuredFieldEdit{
		{
			OwnerRef: sourceRef, FieldName: "spec", Kind: StructuredFieldIdentifierBackedLocator,
			Operation: StructuredFieldEditReplace, Range: ontology.ByteRange{Start: 70, End: 81},
			Expected: "#^K-child", Replacement: "#^SPEC-0002-child", TargetOldRef: loserRef, TargetNewRef: loserRef,
		},
		{
			OwnerRef: loserRef, FieldName: "aliases", Kind: StructuredFieldAliasIdentifier,
			Operation: StructuredFieldEditRemove, Range: ontology.ByteRange{Start: 30, End: 33},
			Expected: "K", TargetOldRef: loserRef, TargetNewRef: loserRef,
		},
	}, plan.Edits)
}

func TestReplaceBoundedIdentifierPreservesWidthChangingUnicodeFold(t *testing.T) {
	replacement, ok := replaceBoundedIdentifier("prefix/#^ẞ-child", "ß", "SPEC-0002")
	require.True(t, ok)
	require.Equal(t, "prefix/#^SPEC-0002-child", replacement)

	_, ok = replaceBoundedIdentifier("#^Aẞ-child", "ß", "SPEC-0002")
	require.False(t, ok)
}

func TestPlanIdentifierFieldRewritesAliasToPreferredCollisionKeepsLoserPreferred(t *testing.T) {
	keeperRef := noteRef("docs/specs/keeper.md", "Spec")
	loserRef := noteRef("docs/specs/loser.md", "Spec")
	sourceRef := noteRef("docs/plans/consumer.md", "Plan")
	inbound := fieldOccurrence(sourceRef, "spec", StructuredFieldTypedIdentifierReference, "SPEC-0075", 70, 79)
	inbound.Candidates = []ontology.NodeRef{loserRef}

	plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{
		Rewrites: []IdentifierRewrite{{
			Mode:   IdentifierRewriteAliasRemoval,
			OldRef: loserRef, NewRef: loserRef,
			OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0090",
			PreferredField: "id", AliasesField: "aliases",
		}},
		Occurrences: []StructuredFieldOccurrence{
			fieldOccurrence(keeperRef, "id", StructuredFieldPreferredIdentifier, "SPEC-0075", 1, 10),
			fieldOccurrence(loserRef, "id", StructuredFieldPreferredIdentifier, "SPEC-0090", 20, 29),
			fieldOccurrence(loserRef, "aliases", StructuredFieldAliasIdentifier, "SPEC-0075", 30, 39),
			inbound,
		},
	})

	require.Empty(t, plan.Diagnostics)
	require.Len(t, plan.Edits, 2)
	for _, edit := range plan.Edits {
		require.False(t, edit.OwnerRef == loserRef && edit.FieldName == "id", "loser's preferred identifier must remain unchanged: %+v", edit)
	}
	require.Equal(t, "SPEC-0090", replacementForKind(t, plan.Edits, StructuredFieldTypedIdentifierReference))
}

func TestPlanIdentifierFieldRewritesSupportsPreferredAndMultipleAliasCollisionsOnOneNode(t *testing.T) {
	ref := noteRef("docs/specs/multi-collision.md", "Spec")
	rewrites := []IdentifierRewrite{
		{OldRef: ref, NewRef: ref, OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0090", PreferredField: "id", AliasesField: "aliases"},
		{Mode: IdentifierRewriteAliasRemoval, OldRef: ref, NewRef: ref, OldIdentifier: "SHARED-A", NewIdentifier: "SPEC-0090", PreferredField: "id", AliasesField: "aliases"},
		{Mode: IdentifierRewriteAliasRemoval, OldRef: ref, NewRef: ref, OldIdentifier: "SHARED-B", NewIdentifier: "SPEC-0090", PreferredField: "id", AliasesField: "aliases"},
	}
	occurrences := []StructuredFieldOccurrence{
		fieldOccurrence(ref, "id", StructuredFieldPreferredIdentifier, "SPEC-0075", 10, 19),
		fieldOccurrence(ref, "aliases", StructuredFieldAliasIdentifier, "SPEC-0075", 30, 39),
		fieldOccurrence(ref, "aliases", StructuredFieldAliasIdentifier, "SHARED-A", 40, 48),
		fieldOccurrence(ref, "aliases", StructuredFieldAliasIdentifier, "SHARED-B", 49, 57),
	}

	forward := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{Rewrites: rewrites, Occurrences: occurrences})
	reverse := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{
		Rewrites:    []IdentifierRewrite{rewrites[2], rewrites[1], rewrites[0]},
		Occurrences: []StructuredFieldOccurrence{occurrences[3], occurrences[2], occurrences[1], occurrences[0]},
	})

	require.Empty(t, forward.Diagnostics)
	require.Equal(t, forward, reverse)
	require.Len(t, forward.Edits, 5)
	require.Equal(t, StructuredFieldEditReplace, forward.Edits[0].Operation)
	require.Equal(t, StructuredFieldEditAppend, forward.Edits[4].Operation)
}

func TestPlanIdentifierFieldRewritesRewritesOnlyUniquelyResolvedTypedOccurrence(t *testing.T) {
	oldRef := noteRef("docs/specs/duplicate.md", "Spec")
	newRef := noteRef("docs/specs/rekeyed.md", "Spec")
	sourceRef := noteRef("docs/efforts/consumer.md", "Effort")
	occurrence := fieldOccurrence(sourceRef, "specs", StructuredFieldTypedIdentifierReference, "SPEC-0075", 90, 99)
	occurrence.Candidates = []ontology.NodeRef{oldRef}

	plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{
		Rewrites: []IdentifierRewrite{{
			OldRef: oldRef, NewRef: newRef,
			OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0081",
			PreferredField: "id", AliasesField: "aliases",
		}},
		Occurrences: []StructuredFieldOccurrence{occurrence},
	})

	require.Equal(t, []StructuredFieldEdit{{
		OwnerRef: sourceRef, FieldName: "specs", Kind: StructuredFieldTypedIdentifierReference,
		Operation: StructuredFieldEditReplace, Range: ontology.ByteRange{Start: 90, End: 99},
		Expected: "SPEC-0075", Replacement: "SPEC-0081", TargetOldRef: oldRef, TargetNewRef: newRef,
	}}, plan.Edits)
	require.Contains(t, diagnosticKinds(plan.Diagnostics), IdentifierRewriteDiagnosticMissingPreferred)
}

func TestPlanIdentifierFieldRewritesReportsAmbiguousStructuredTarget(t *testing.T) {
	oldRef := noteRef("docs/specs/a.md", "Spec")
	otherRef := noteRef("docs/specs/b.md", "Spec")
	newRef := noteRef("docs/specs/rekeyed.md", "Spec")
	sourceRef := noteRef("docs/efforts/consumer.md", "Effort")
	occurrence := fieldOccurrence(sourceRef, "specs", StructuredFieldTypedIdentifierReference, "SPEC-0075", 90, 99)
	occurrence.Candidates = []ontology.NodeRef{otherRef, oldRef}

	plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{
		Rewrites: []IdentifierRewrite{{
			OldRef: oldRef, NewRef: newRef,
			OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0081",
			PreferredField: "id", AliasesField: "aliases",
		}},
		Occurrences: []StructuredFieldOccurrence{occurrence},
	})

	require.Empty(t, plan.Edits)
	require.Equal(t, IdentifierRewriteDiagnosticAmbiguousTarget, plan.Diagnostics[0].Kind)
	require.Equal(t, []ontology.NodeRef{oldRef, otherRef}, plan.Diagnostics[0].Candidates)
}

func TestPlanIdentifierFieldRewritesReportsUnresolvedStructuredOldIdentifier(t *testing.T) {
	oldRef := noteRef("docs/specs/a.md", "Spec")
	newRef := noteRef("docs/specs/rekeyed.md", "Spec")
	sourceRef := noteRef("docs/efforts/consumer.md", "Effort")

	plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{
		Rewrites: []IdentifierRewrite{{
			OldRef: oldRef, NewRef: newRef,
			OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0081",
			PreferredField: "id", AliasesField: "aliases",
		}},
		Occurrences: []StructuredFieldOccurrence{
			fieldOccurrence(sourceRef, "specs", StructuredFieldTypedIdentifierReference, "spec-0075", 90, 99),
		},
	})

	require.Empty(t, plan.Edits)
	require.Equal(t, IdentifierRewriteDiagnosticUnresolvedTarget, plan.Diagnostics[0].Kind)
}

func TestPlanIdentifierFieldRewritesCascadesDeclaredDerivedDescendant(t *testing.T) {
	parentOld := noteRef("docs/specs/duplicate.md", "Spec")
	parentNew := noteRef("docs/specs/rekeyed.md", "Spec")
	childOld := embeddedRef("docs/specs/duplicate.md", "^SPEC-0075-US1", "SPEC-0075-US1", "UserStory")
	childNew := embeddedRef("docs/specs/rekeyed.md", "^SPEC-0081-US1", "SPEC-0081-US1", "UserStory")
	sourceRef := noteRef("docs/plans/consumer.md", "Plan")
	locator := fieldOccurrence(sourceRef, "stories", StructuredFieldIdentifierBackedLocator, childOld.String(), 100, 134)
	locator.Candidates = []ontology.NodeRef{childOld}

	plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{
		Rewrites: []IdentifierRewrite{
			{OldRef: parentOld, NewRef: parentNew, OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0081", PreferredField: "id", AliasesField: "aliases"},
			{OldRef: childOld, NewRef: childNew, OldIdentifier: "SPEC-0075-US1", NewIdentifier: "SPEC-0081-US1", PreferredField: "id", AliasesField: "aliases", DerivedFrom: parentOld},
		},
		Occurrences: []StructuredFieldOccurrence{locator},
	})

	require.False(t, hasDiagnosticForRef(plan.Diagnostics, IdentifierRewriteDiagnosticMissingPreferred, childOld))
	require.Contains(t, plan.Edits, StructuredFieldEdit{
		OwnerRef: sourceRef, FieldName: "stories", Kind: StructuredFieldIdentifierBackedLocator,
		Operation: StructuredFieldEditReplace, Range: ontology.ByteRange{Start: 100, End: 134},
		Expected: childOld.String(), Replacement: childNew.String(), TargetOldRef: childOld, TargetNewRef: childNew,
	})
}

func TestPlanIdentifierFieldRewritesPreservesFragmentOnlyLocatorStyle(t *testing.T) {
	oldRef := embeddedRef("docs/specs/duplicate.md", "^SPEC-0075-US1", "SPEC-0075-US1", "UserStory")
	newRef := embeddedRef("docs/specs/rekeyed.md", "^SPEC-0081-US1", "SPEC-0081-US1", "UserStory")
	sourceRef := noteRef("docs/plans/consumer.md", "Plan")
	occurrence := fieldOccurrence(sourceRef, "story", StructuredFieldIdentifierBackedLocator, "#^SPEC-0075-US1", 10, 25)
	occurrence.Candidates = []ontology.NodeRef{oldRef}

	plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{
		Rewrites:    []IdentifierRewrite{{OldRef: oldRef, NewRef: newRef, OldIdentifier: "SPEC-0075-US1", NewIdentifier: "SPEC-0081-US1", PreferredField: "id", AliasesField: "aliases"}},
		Occurrences: []StructuredFieldOccurrence{occurrence},
	})

	require.Len(t, plan.Edits, 1)
	require.Equal(t, "#^SPEC-0081-US1", plan.Edits[0].Replacement)
}

func TestPlanIdentifierFieldRewritesDoesNotTreatUnicodeLetterAsIdentifierBoundary(t *testing.T) {
	oldRef := embeddedRef("docs/specs/duplicate.md", "^SPEC-0075-US1", "SPEC-0075-US1", "UserStory")
	newRef := embeddedRef("docs/specs/rekeyed.md", "^SPEC-0081-US1", "SPEC-0081-US1", "UserStory")
	sourceRef := noteRef("docs/plans/consumer.md", "Plan")
	occurrence := fieldOccurrence(sourceRef, "story", StructuredFieldIdentifierBackedLocator, "éSPEC-0075-US1", 10, 26)
	occurrence.Candidates = []ontology.NodeRef{oldRef}

	plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{
		Rewrites:    []IdentifierRewrite{{OldRef: oldRef, NewRef: newRef, OldIdentifier: "SPEC-0075-US1", NewIdentifier: "SPEC-0081-US1", PreferredField: "id", AliasesField: "aliases"}},
		Occurrences: []StructuredFieldOccurrence{occurrence},
	})

	require.Empty(t, plan.Edits)
	require.Contains(t, diagnosticKinds(plan.Diagnostics), IdentifierRewriteDiagnosticInvalidInput)
}

func TestPlanIdentifierFieldRewritesRejectsInvalidDerivedCascade(t *testing.T) {
	parentOld := noteRef("docs/specs/duplicate.md", "Spec")
	parentNew := noteRef("docs/specs/rekeyed.md", "Spec")
	childOld := embeddedRef("docs/specs/duplicate.md", "^OTHER-US1", "OTHER-US1", "UserStory")
	childNew := embeddedRef("docs/specs/rekeyed.md", "^SPEC-0081-US1", "SPEC-0081-US1", "UserStory")

	plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{Rewrites: []IdentifierRewrite{
		{OldRef: parentOld, NewRef: parentNew, OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0081", PreferredField: "id", AliasesField: "aliases"},
		{OldRef: childOld, NewRef: childNew, OldIdentifier: "OTHER-US1", NewIdentifier: "SPEC-0081-US1", PreferredField: "id", AliasesField: "aliases", DerivedFrom: parentOld},
	}})

	require.Empty(t, plan.Edits)
	require.Contains(t, diagnosticKinds(plan.Diagnostics), IdentifierRewriteDiagnosticInvalidDerivedCascade)
}

func TestPlanIdentifierFieldRewritesRejectsNonVaultRelativeCanonicalRefs(t *testing.T) {
	for _, invalidPath := range []string{"/absolute/spec.md", "../outside/spec.md", "C:/outside/spec.md"} {
		t.Run(invalidPath, func(t *testing.T) {
			invalidRef := noteRef(invalidPath, "Spec")
			plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{Rewrites: []IdentifierRewrite{{
				OldRef: invalidRef, NewRef: noteRef("docs/specs/new.md", "Spec"),
				OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0081", PreferredField: "id", AliasesField: "aliases",
			}}})

			require.Empty(t, plan.Edits)
			require.Contains(t, diagnosticKinds(plan.Diagnostics), IdentifierRewriteDiagnosticInvalidInput)
		})
	}
}

func TestPlanIdentifierFieldRewritesKeepsReviewCandidatesOutOfEdits(t *testing.T) {
	oldRef := noteRef("docs/specs/a.md", "Spec")
	newRef := noteRef("docs/specs/b.md", "Spec")
	sourceRef := noteRef("pkg/example.go", "CodeFile")
	occurrence := fieldOccurrence(sourceRef, "", StructuredFieldReviewCandidate, "SPEC-0075", 30, 39)
	occurrence.Candidates = []ontology.NodeRef{oldRef}

	plan := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{
		Rewrites:    []IdentifierRewrite{{OldRef: oldRef, NewRef: newRef, OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0081", PreferredField: "id", AliasesField: "aliases"}},
		Occurrences: []StructuredFieldOccurrence{occurrence},
	})

	require.Empty(t, plan.Edits)
	require.Equal(t, IdentifierRewriteDiagnosticReviewOnly, plan.Diagnostics[0].Kind)
}

func TestPlanIdentifierFieldRewritesIsDeterministicAcrossInputOrder(t *testing.T) {
	aOld := noteRef("docs/a.md", "Spec")
	aNew := noteRef("docs/a-new.md", "Spec")
	bOld := noteRef("docs/b.md", "Spec")
	bNew := noteRef("docs/b-new.md", "Spec")
	aOccurrence := fieldOccurrence(aOld, "id", StructuredFieldPreferredIdentifier, "A-1", 2, 5)
	bOccurrence := fieldOccurrence(bOld, "id", StructuredFieldPreferredIdentifier, "B-1", 8, 11)

	forward := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{
		Rewrites: []IdentifierRewrite{
			{OldRef: aOld, NewRef: aNew, OldIdentifier: "A-1", NewIdentifier: "A-2", PreferredField: "id", AliasesField: "aliases"},
			{OldRef: bOld, NewRef: bNew, OldIdentifier: "B-1", NewIdentifier: "B-2", PreferredField: "id", AliasesField: "aliases"},
		},
		Occurrences: []StructuredFieldOccurrence{aOccurrence, bOccurrence},
	})
	reverse := PlanIdentifierFieldRewrites(IdentifierFieldRewriteInput{
		Rewrites: []IdentifierRewrite{
			{OldRef: bOld, NewRef: bNew, OldIdentifier: "B-1", NewIdentifier: "B-2", PreferredField: "id", AliasesField: "aliases"},
			{OldRef: aOld, NewRef: aNew, OldIdentifier: "A-1", NewIdentifier: "A-2", PreferredField: "id", AliasesField: "aliases"},
		},
		Occurrences: []StructuredFieldOccurrence{bOccurrence, aOccurrence},
	})

	require.Equal(t, forward, reverse)
}

func fieldOccurrence(owner ontology.NodeRef, field string, kind StructuredFieldKind, value string, start, end int) StructuredFieldOccurrence {
	return StructuredFieldOccurrence{OwnerRef: owner, FieldName: field, Kind: kind, Value: value, Range: ontology.ByteRange{Start: start, End: end}}
}

func noteRef(path, typeName string) ontology.NodeRef {
	return ontology.NodeRef{NotePath: path, TypeName: typeName, Kind: ontology.NodeKindNote}
}

func embeddedRef(path, fragment, nodeID, typeName string) ontology.NodeRef {
	return ontology.NodeRef{NotePath: path, Fragment: fragment, NodeID: nodeID, TypeName: typeName, Kind: ontology.NodeKindEmbedded}
}

func diagnosticKinds(diagnostics []IdentifierRewriteDiagnostic) []IdentifierRewriteDiagnosticKind {
	out := make([]IdentifierRewriteDiagnosticKind, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		out = append(out, diagnostic.Kind)
	}
	return out
}

func hasDiagnosticForRef(diagnostics []IdentifierRewriteDiagnostic, kind IdentifierRewriteDiagnosticKind, ref ontology.NodeRef) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Kind == kind && diagnostic.OwnerRef == ref {
			return true
		}
	}
	return false
}

func replacementForKind(t *testing.T, edits []StructuredFieldEdit, kind StructuredFieldKind) string {
	t.Helper()
	for _, edit := range edits {
		if edit.Kind == kind {
			return edit.Replacement
		}
	}
	t.Fatalf("missing edit kind %s", kind)
	return ""
}
