package reference

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestPlanStructuredLinkRewritesEmitsExactComponentEdits(t *testing.T) {
	content := "[[docs/SPEC-0075-contract#^SPEC-0075-US1|SPEC-0075-US1]] and [SPEC-0075-US1](../specs/SPEC-0075-contract.md#^SPEC-0075-US1)"
	links := obsidian.ScanStructuredLinks(content)
	oldRef := embeddedRef("docs/specs/SPEC-0075-contract.md", "^SPEC-0075-US1", "SPEC-0075-US1", "UserStory")
	newRef := embeddedRef("docs/specs/SPEC-0081-contract.md", "^SPEC-0081-US1", "SPEC-0081-US1", "UserStory")

	plan := PlanStructuredLinkRewrites(StructuredLinkRewriteInput{
		NotePath: "docs/plans/consumer.md",
		Content:  content,
		Links:    links,
		Rewrites: []IdentifierRewrite{{
			OldRef: oldRef, NewRef: newRef,
			OldIdentifier: "SPEC-0075-US1", NewIdentifier: "SPEC-0081-US1",
		}},
		Resolutions: []StructuredLinkResolution{
			{LinkIndex: 0, Candidates: []ontology.NodeRef{oldRef}},
			{LinkIndex: 1, Candidates: []ontology.NodeRef{oldRef}},
		},
	})

	require.Empty(t, plan.Diagnostics)
	require.Equal(t, "docs/plans/consumer.md", plan.NotePath)
	require.Equal(t, []StructuredLinkEdit{
		linkEdit("docs/plans/consumer.md", 0, LinkComponentPath, links[0].PathSpan, "docs/SPEC-0075-contract", "docs/SPEC-0081-contract", oldRef, newRef),
		linkEdit("docs/plans/consumer.md", 0, LinkComponentFragment, links[0].FragmentSpan, "^SPEC-0075-US1", "^SPEC-0081-US1", oldRef, newRef),
		linkEdit("docs/plans/consumer.md", 0, LinkComponentDisplay, links[0].DisplaySpan, "SPEC-0075-US1", "SPEC-0081-US1", oldRef, newRef),
		linkEdit("docs/plans/consumer.md", 1, LinkComponentDisplay, links[1].DisplaySpan, "SPEC-0075-US1", "SPEC-0081-US1", oldRef, newRef),
		linkEdit("docs/plans/consumer.md", 1, LinkComponentPath, links[1].PathSpan, "../specs/SPEC-0075-contract.md", "../specs/SPEC-0081-contract.md", oldRef, newRef),
		linkEdit("docs/plans/consumer.md", 1, LinkComponentFragment, links[1].FragmentSpan, "^SPEC-0075-US1", "^SPEC-0081-US1", oldRef, newRef),
	}, plan.Edits)
}

func TestPlanStructuredLinkRewritesCascadesDeclaredDescendantRewrite(t *testing.T) {
	content := "[[SPEC-0075-contract#^SPEC-0075-US1|SPEC-0075-US1]]"
	links := obsidian.ScanStructuredLinks(content)
	parentOld := noteRef("docs/specs/SPEC-0075-contract.md", "Spec")
	parentNew := noteRef("docs/specs/SPEC-0081-contract.md", "Spec")
	childOld := embeddedRef(parentOld.NotePath, "^SPEC-0075-US1", "SPEC-0075-US1", "UserStory")
	childNew := embeddedRef(parentNew.NotePath, "^SPEC-0081-US1", "SPEC-0081-US1", "UserStory")

	plan := PlanStructuredLinkRewrites(StructuredLinkRewriteInput{
		NotePath: "docs/plans/consumer.md",
		Content:  content,
		Links:    links,
		Rewrites: []IdentifierRewrite{
			{OldRef: parentOld, NewRef: parentNew, OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0081"},
			{OldRef: childOld, NewRef: childNew, OldIdentifier: "SPEC-0075-US1", NewIdentifier: "SPEC-0081-US1", DerivedFrom: parentOld},
		},
		Resolutions: []StructuredLinkResolution{{LinkIndex: 0, Candidates: []ontology.NodeRef{childOld}}},
	})

	require.Empty(t, plan.Diagnostics)
	require.Len(t, plan.Edits, 3)
	require.Equal(t, "SPEC-0081-contract", plan.Edits[0].Replacement)
	require.Equal(t, "^SPEC-0081-US1", plan.Edits[1].Replacement)
	require.Equal(t, "SPEC-0081-US1", plan.Edits[2].Replacement)
}

func TestPlanStructuredLinkRewritesUpdatesIdentifierWithinDisplayLabel(t *testing.T) {
	content := "[[SPEC-0001#^SPEC-0001-US1-TC1|SPEC-0001.US1.TC1]] [[SPEC-0001|SPEC-0001 contract]]"
	links := obsidian.ScanStructuredLinks(content)
	rootOld := noteRef("docs/specs/SPEC-0001.md", "Spec")
	rootNew := noteRef("docs/specs/SPEC-0002.md", "Spec")
	childOld := embeddedRef(rootOld.NotePath, "^SPEC-0001-US1-TC1", "SPEC-0001-US1-TC1", "TestCase")
	childNew := embeddedRef(rootNew.NotePath, "^SPEC-0002-US1-TC1", "SPEC-0002-US1-TC1", "TestCase")

	plan := PlanStructuredLinkRewrites(StructuredLinkRewriteInput{
		NotePath: "docs/consumer.md", Content: content, Links: links,
		Rewrites: []IdentifierRewrite{
			{OldRef: rootOld, NewRef: rootNew, OldIdentifier: "SPEC-0001", NewIdentifier: "SPEC-0002"},
			{OldRef: childOld, NewRef: childNew, OldIdentifier: "SPEC-0001-US1-TC1", NewIdentifier: "SPEC-0002-US1-TC1", DerivedFrom: rootOld},
		},
		Resolutions: []StructuredLinkResolution{
			{LinkIndex: 0, Candidates: []ontology.NodeRef{childOld}},
			{LinkIndex: 1, Candidates: []ontology.NodeRef{rootOld}},
		},
	})

	replacements := make([]string, 0)
	for _, edit := range plan.Edits {
		if edit.Component == LinkComponentDisplay {
			replacements = append(replacements, edit.Replacement)
		}
	}
	require.Equal(t, []string{"SPEC-0002.US1.TC1", "SPEC-0002 contract"}, replacements)
}

func TestPlanStructuredLinkRewritesAliasRemovalTargetsExistingPreferredID(t *testing.T) {
	content := "[[SPEC-0075|spec-0075]]"
	links := obsidian.ScanStructuredLinks(content)
	loser := noteRef("docs/specs/other.md", "Spec")

	plan := PlanStructuredLinkRewrites(StructuredLinkRewriteInput{
		NotePath: "docs/consumer.md",
		Content:  content,
		Links:    links,
		Rewrites: []IdentifierRewrite{{
			Mode:   IdentifierRewriteAliasRemoval,
			OldRef: loser, NewRef: loser,
			OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0042",
		}},
		Resolutions: []StructuredLinkResolution{{LinkIndex: 0, Candidates: []ontology.NodeRef{loser}}},
	})

	require.Empty(t, plan.Diagnostics)
	require.Equal(t, []StructuredLinkEdit{
		linkEdit("docs/consumer.md", 0, LinkComponentPath, links[0].PathSpan, "SPEC-0075", "SPEC-0042", loser, loser),
		linkEdit("docs/consumer.md", 0, LinkComponentDisplay, links[0].DisplaySpan, "spec-0075", "SPEC-0042", loser, loser),
	}, plan.Edits)
}

func TestPlanStructuredLinkRewritesSelectsAuthoredAliasAmongSameRefRewrites(t *testing.T) {
	content := "[[SPEC-0075|spec-0075]] [[OLD-ALIAS|old-alias]]"
	links := obsidian.ScanStructuredLinks(content)
	loser := noteRef("docs/specs/other.md", "Spec")
	first := IdentifierRewrite{Mode: IdentifierRewriteAliasRemoval, OldRef: loser, NewRef: loser, OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0042"}
	second := IdentifierRewrite{Mode: IdentifierRewriteAliasRemoval, OldRef: loser, NewRef: loser, OldIdentifier: "OLD-ALIAS", NewIdentifier: "SPEC-0042"}
	firstResolution := StructuredLinkResolution{LinkIndex: 0, Candidates: []ontology.NodeRef{loser}}
	secondResolution := StructuredLinkResolution{LinkIndex: 1, Candidates: []ontology.NodeRef{loser}}

	forward := PlanStructuredLinkRewrites(StructuredLinkRewriteInput{
		NotePath: "docs/consumer.md", Content: content, Links: links,
		Rewrites: []IdentifierRewrite{first, second}, Resolutions: []StructuredLinkResolution{firstResolution, secondResolution},
	})
	reverse := PlanStructuredLinkRewrites(StructuredLinkRewriteInput{
		NotePath: "docs/consumer.md", Content: content, Links: links,
		Rewrites: []IdentifierRewrite{second, first}, Resolutions: []StructuredLinkResolution{secondResolution, firstResolution},
	})

	require.Equal(t, forward, reverse)
	require.Empty(t, forward.Diagnostics)
	require.Len(t, forward.Edits, 4)
	for _, edit := range forward.Edits {
		require.Equal(t, "SPEC-0042", edit.Replacement)
	}
}

func TestPlanStructuredLinkRewritesBlocksConflictingSameRefSemanticRewrites(t *testing.T) {
	content := "[[OLD-ALIAS]]"
	links := obsidian.ScanStructuredLinks(content)
	loser := noteRef("docs/specs/other.md", "Spec")
	resolution := StructuredLinkResolution{LinkIndex: 0, Candidates: []ontology.NodeRef{loser}}

	plan := PlanStructuredLinkRewrites(StructuredLinkRewriteInput{
		NotePath: "docs/consumer.md", Content: content, Links: links,
		Rewrites: []IdentifierRewrite{
			{Mode: IdentifierRewriteAliasRemoval, OldRef: loser, NewRef: loser, OldIdentifier: "OLD-ALIAS", NewIdentifier: "SPEC-0042"},
			{Mode: IdentifierRewriteAliasRemoval, OldRef: loser, NewRef: loser, OldIdentifier: "old-alias", NewIdentifier: "SPEC-0043"},
		},
		Resolutions: []StructuredLinkResolution{resolution},
	})

	require.Empty(t, plan.Edits)
	require.Equal(t, LinkRewriteDiagnosticConflictingEdit, plan.Diagnostics[0].Kind)
	require.True(t, plan.Diagnostics[0].Blocking)
}

func TestPlanStructuredLinkRewritesBlocksAmbiguityWithoutOpaqueProvenanceDecision(t *testing.T) {
	content := "[[SPEC-0075]]"
	links := obsidian.ScanStructuredLinks(content)
	oldRef := noteRef("docs/specs/a.md", "Spec")
	otherRef := noteRef("docs/specs/b.md", "Spec")
	newRef := noteRef("docs/specs/a-new.md", "Spec")

	plan := PlanStructuredLinkRewrites(StructuredLinkRewriteInput{
		NotePath: "docs/consumer.md", Content: content, Links: links,
		Rewrites:    []IdentifierRewrite{{OldRef: oldRef, NewRef: newRef, OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0081"}},
		Resolutions: []StructuredLinkResolution{{LinkIndex: 0, Candidates: []ontology.NodeRef{otherRef, oldRef}}},
	})

	require.Empty(t, plan.Edits)
	require.Equal(t, LinkRewriteDiagnosticAmbiguousTarget, plan.Diagnostics[0].Kind)
	require.True(t, plan.Diagnostics[0].Blocking)
}

func TestPlanStructuredLinkRewritesPreservesPhysicallyDistinctAmbiguousCandidates(t *testing.T) {
	content := "[[SPEC-0075]]"
	links := obsidian.ScanStructuredLinks(content)
	target := noteRef("docs/specs/a.md", "Spec")
	first := target
	first.NodeID = "physical-a"
	first.Structural = "structural-a"
	second := target
	second.NodeID = "physical-b"
	second.Structural = "structural-b"

	plan := PlanStructuredLinkRewrites(StructuredLinkRewriteInput{
		NotePath: "docs/consumer.md", Content: content, Links: links,
		Rewrites: []IdentifierRewrite{{
			OldRef: target, NewRef: noteRef("docs/specs/a-new.md", "Spec"),
			OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0081",
		}},
		Resolutions: []StructuredLinkResolution{{LinkIndex: 0, Candidates: []ontology.NodeRef{first, second}}},
	})

	require.Empty(t, plan.Edits)
	require.Len(t, plan.Diagnostics, 1)
	require.Equal(t, LinkRewriteDiagnosticAmbiguousTarget, plan.Diagnostics[0].Kind)
	require.Len(t, plan.Diagnostics[0].Candidates, 2)
}

func TestPlanStructuredLinkRewritesReportsUnresolvedRelevantTarget(t *testing.T) {
	content := "[[SPEC-0075]]"
	links := obsidian.ScanStructuredLinks(content)
	oldRef := noteRef("docs/specs/a.md", "Spec")

	plan := PlanStructuredLinkRewrites(StructuredLinkRewriteInput{
		NotePath: "docs/consumer.md", Content: content, Links: links,
		Rewrites:    []IdentifierRewrite{{OldRef: oldRef, NewRef: noteRef("docs/specs/a-new.md", "Spec"), OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0081"}},
		Resolutions: []StructuredLinkResolution{{LinkIndex: 0}},
	})

	require.Empty(t, plan.Edits)
	require.Equal(t, LinkRewriteDiagnosticUnresolvedTarget, plan.Diagnostics[0].Kind)
	require.True(t, plan.Diagnostics[0].Blocking)
}

func TestPlanStructuredLinkRewritesRejectsStaleComponentSpan(t *testing.T) {
	content := "[[SPEC-0075]]"
	links := obsidian.ScanStructuredLinks(content)
	links[0].PathSpan.End--
	oldRef := noteRef("docs/specs/a.md", "Spec")

	plan := PlanStructuredLinkRewrites(StructuredLinkRewriteInput{
		NotePath: "docs/consumer.md", Content: content, Links: links,
		Rewrites:    []IdentifierRewrite{{OldRef: oldRef, NewRef: noteRef("docs/specs/SPEC-0081-a.md", "Spec"), OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0081"}},
		Resolutions: []StructuredLinkResolution{{LinkIndex: 0, Candidates: []ontology.NodeRef{oldRef}}},
	})

	require.Empty(t, plan.Edits)
	require.Equal(t, LinkRewriteDiagnosticUnsafeSpan, plan.Diagnostics[0].Kind)
	require.True(t, plan.Diagnostics[0].Blocking)
}

func TestPlanStructuredLinkRewritesRejectsEscapingSourceAndCanonicalRefPaths(t *testing.T) {
	content := "[[SPEC-0075]]"
	links := obsidian.ScanStructuredLinks(content)
	validRef := noteRef("docs/specs/a.md", "Spec")

	for _, input := range []StructuredLinkRewriteInput{
		{
			NotePath: "../consumer.md", Content: content, Links: links,
			Rewrites: []IdentifierRewrite{{OldRef: validRef, NewRef: validRef, OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0081"}},
		},
		{
			NotePath: "docs/consumer.md", Content: content, Links: links,
			Rewrites: []IdentifierRewrite{{OldRef: noteRef("../a.md", "Spec"), NewRef: validRef, OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0081"}},
		},
	} {
		plan := PlanStructuredLinkRewrites(input)
		require.Empty(t, plan.Edits)
		require.Equal(t, LinkRewriteDiagnosticInvalidInput, plan.Diagnostics[0].Kind)
		require.True(t, plan.Diagnostics[0].Blocking)
	}
}

func TestPlanStructuredLinkRewritesBlocksDuplicateResolutionEvidenceRegardlessOfOrder(t *testing.T) {
	content := "[[SPEC-0075]]"
	links := obsidian.ScanStructuredLinks(content)
	oldRef := noteRef("docs/specs/a.md", "Spec")
	otherRef := noteRef("docs/specs/b.md", "Spec")
	oldResolution := StructuredLinkResolution{LinkIndex: 0, Candidates: []ontology.NodeRef{oldRef}}
	otherResolution := StructuredLinkResolution{LinkIndex: 0, Candidates: []ontology.NodeRef{otherRef}}
	rewrite := IdentifierRewrite{OldRef: oldRef, NewRef: noteRef("docs/specs/SPEC-0081-a.md", "Spec"), OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0081"}

	forward := PlanStructuredLinkRewrites(StructuredLinkRewriteInput{
		NotePath: "docs/consumer.md", Content: content, Links: links,
		Rewrites: []IdentifierRewrite{rewrite}, Resolutions: []StructuredLinkResolution{oldResolution, otherResolution, oldResolution},
	})
	reverse := PlanStructuredLinkRewrites(StructuredLinkRewriteInput{
		NotePath: "docs/consumer.md", Content: content, Links: links,
		Rewrites: []IdentifierRewrite{rewrite}, Resolutions: []StructuredLinkResolution{otherResolution, oldResolution, otherResolution},
	})

	require.Equal(t, forward, reverse)
	require.Empty(t, forward.Edits)
	require.Contains(t, linkDiagnosticKinds(forward.Diagnostics), LinkRewriteDiagnosticInvalidInput)
}

func TestPlanStructuredLinkRewritesIsDeterministicAcrossResolutionAndRewriteOrder(t *testing.T) {
	content := "[[A-1|A-1]] [[B-1|B-1]]"
	links := obsidian.ScanStructuredLinks(content)
	aOld, aNew := noteRef("docs/a.md", "Spec"), noteRef("docs/A-2-a.md", "Spec")
	bOld, bNew := noteRef("docs/b.md", "Spec"), noteRef("docs/B-2-b.md", "Spec")
	aRewrite := IdentifierRewrite{OldRef: aOld, NewRef: aNew, OldIdentifier: "A-1", NewIdentifier: "A-2"}
	bRewrite := IdentifierRewrite{OldRef: bOld, NewRef: bNew, OldIdentifier: "B-1", NewIdentifier: "B-2"}
	aResolution := StructuredLinkResolution{LinkIndex: 0, Candidates: []ontology.NodeRef{aOld}}
	bResolution := StructuredLinkResolution{LinkIndex: 1, Candidates: []ontology.NodeRef{bOld}}

	forward := PlanStructuredLinkRewrites(StructuredLinkRewriteInput{
		NotePath: "docs/consumer.md", Content: content, Links: links,
		Rewrites: []IdentifierRewrite{aRewrite, bRewrite}, Resolutions: []StructuredLinkResolution{aResolution, bResolution},
	})
	reverse := PlanStructuredLinkRewrites(StructuredLinkRewriteInput{
		NotePath: "docs/consumer.md", Content: content, Links: links,
		Rewrites: []IdentifierRewrite{bRewrite, aRewrite}, Resolutions: []StructuredLinkResolution{bResolution, aResolution},
	})

	require.Equal(t, forward, reverse)
}

func TestPlanStructuredLinkRewritesRejectsForgedProseOccurrence(t *testing.T) {
	content := "Prose SPEC-0075 only."
	start := len("Prose ")
	span := obsidian.StructuredLinkSpan{Start: start, End: start + len("SPEC-0075"), Present: true}
	forged := obsidian.StructuredLink{
		Kind: obsidian.StructuredLinkWikilink, Target: "SPEC-0075", Path: "SPEC-0075",
		RawSpan: span, TargetSpan: span, PathSpan: span,
	}
	oldRef := noteRef("docs/specs/old.md", "Spec")

	plan := PlanStructuredLinkRewrites(StructuredLinkRewriteInput{
		NotePath: "docs/consumer.md", Content: content, Links: []obsidian.StructuredLink{forged},
		Rewrites:    []IdentifierRewrite{{OldRef: oldRef, NewRef: oldRef, OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0081"}},
		Resolutions: []StructuredLinkResolution{{LinkIndex: 0, Candidates: []ontology.NodeRef{oldRef}}},
	})

	require.Empty(t, plan.Edits)
	require.Equal(t, LinkRewriteDiagnosticUnsafeSpan, plan.Diagnostics[0].Kind)
}

func TestPlanStructuredLinkRewritesChangesOnlyCaseFoldedGovernedBasename(t *testing.T) {
	content := "[[SPEC-0075/archive/spec-0075-note|SPEC-0075]] [SPEC-0075](SPEC-0075/archive/spec-0075-note.md)"
	links := obsidian.ScanStructuredLinks(content)
	oldRef := noteRef("SPEC-0075/archive/SPEC-0075-note.md", "Spec")
	newRef := noteRef("SPEC-0075/archive/SPEC-0081-note.md", "Spec")

	plan := PlanStructuredLinkRewrites(StructuredLinkRewriteInput{
		NotePath: "docs/consumer.md", Content: content, Links: links,
		Rewrites: []IdentifierRewrite{{OldRef: oldRef, NewRef: newRef, OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0081"}},
		Resolutions: []StructuredLinkResolution{
			{LinkIndex: 0, Candidates: []ontology.NodeRef{oldRef}},
			{LinkIndex: 1, Candidates: []ontology.NodeRef{oldRef}},
		},
	})

	require.Empty(t, plan.Diagnostics)
	require.Len(t, plan.Edits, 4)
	require.Equal(t, "SPEC-0075/archive/SPEC-0081-note", plan.Edits[0].Replacement)
	require.Equal(t, "SPEC-0075/archive/SPEC-0081-note.md", plan.Edits[3].Replacement)
}

func TestPlanStructuredLinkRewritesInvalidCandidateEvidenceIsDeterministic(t *testing.T) {
	content := "[[SPEC-0075]]"
	links := obsidian.ScanStructuredLinks(content)
	rewrite := IdentifierRewrite{OldRef: noteRef("docs/old.md", "Spec"), NewRef: noteRef("docs/new.md", "Spec"), OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0081"}
	a := noteRef("../escape-a.md", "Spec")
	b := noteRef("/absolute-b.md", "Spec")

	forward := PlanStructuredLinkRewrites(StructuredLinkRewriteInput{
		NotePath: "docs/consumer.md", Content: content, Links: links, Rewrites: []IdentifierRewrite{rewrite},
		Resolutions: []StructuredLinkResolution{{LinkIndex: 0, Candidates: []ontology.NodeRef{a, b}}},
	})
	reverse := PlanStructuredLinkRewrites(StructuredLinkRewriteInput{
		NotePath: "docs/consumer.md", Content: content, Links: links, Rewrites: []IdentifierRewrite{rewrite},
		Resolutions: []StructuredLinkResolution{{LinkIndex: 0, Candidates: []ontology.NodeRef{b, a}}},
	})

	require.Equal(t, forward, reverse)
	require.Len(t, forward.Diagnostics[0].Candidates, 2)
}

func TestStructuredLinkRewritePlanValidatedSnapshotRejectsMutation(t *testing.T) {
	content := "[[SPEC-0075]]"
	links := obsidian.ScanStructuredLinks(content)
	oldRef := noteRef("docs/old.md", "Spec")
	plan := PlanStructuredLinkRewrites(StructuredLinkRewriteInput{
		NotePath: "docs/consumer.md", Content: content, Links: links,
		Rewrites:    []IdentifierRewrite{{OldRef: oldRef, NewRef: noteRef("docs/new.md", "Spec"), OldIdentifier: "SPEC-0075", NewIdentifier: "SPEC-0081"}},
		Resolutions: []StructuredLinkResolution{{LinkIndex: 0, Candidates: []ontology.NodeRef{oldRef}}},
	})

	snapshot, err := plan.ValidatedSnapshot()
	require.NoError(t, err)
	require.Equal(t, plan.Fingerprint, snapshot.Fingerprint)
	snapshot.Edits[0].Replacement = "MUTATED-SNAPSHOT"
	require.NotEqual(t, snapshot.Edits[0], plan.Edits[0])

	plan.Edits[0].Replacement = "MUTATED-PLAN"
	_, err = plan.ValidatedSnapshot()
	require.Error(t, err)
}

func TestPlanStructuredLinkRewritesCarriesSealedSourceFingerprintForEmptyScan(t *testing.T) {
	content := "plain prose"
	scan := obsidian.ScanStructuredLinkSnapshot(content)

	plan := PlanStructuredLinkRewrites(StructuredLinkRewriteInput{
		NotePath: "docs/consumer.md", Content: content, LinkSnapshot: &scan,
	})

	require.Empty(t, plan.Diagnostics)
	require.Equal(t, "docs/consumer.md", plan.NotePath)
	require.NotEmpty(t, plan.SourceFingerprint)
	snapshot, err := plan.ValidatedSnapshot()
	require.NoError(t, err)
	require.Equal(t, plan.SourceFingerprint, snapshot.SourceFingerprint)

	plan.SourceFingerprint = "forged"
	_, err = plan.ValidatedSnapshot()
	require.Error(t, err)
}

func linkDiagnosticKinds(diagnostics []LinkRewriteDiagnostic) []LinkRewriteDiagnosticKind {
	out := make([]LinkRewriteDiagnosticKind, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		out = append(out, diagnostic.Kind)
	}
	return out
}

func linkEdit(notePath string, linkIndex int, component StructuredLinkComponent, span obsidian.StructuredLinkSpan, expected, replacement string, oldRef, newRef ontology.NodeRef) StructuredLinkEdit {
	return StructuredLinkEdit{
		NotePath: notePath, LinkIndex: linkIndex, Component: component,
		Range:    ontology.ByteRange{Start: span.Start, End: span.End},
		Expected: expected, Replacement: replacement,
		TargetOldRef: oldRef, TargetNewRef: newRef,
	}
}
