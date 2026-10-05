package reference

import (
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestPlanIdentifierReviewCandidatesProducesReviewOnlyPluralEvidence(t *testing.T) {
	first := IdentifierRewrite{Mode: IdentifierRewriteAliasRemoval, OldRef: noteRef("docs/a.md", "Spec"), NewRef: noteRef("docs/a.md", "Spec"), OldIdentifier: "SHARED-1", NewIdentifier: "SPEC-1", PreferredField: "id", AliasesField: "aliases"}
	second := IdentifierRewrite{Mode: IdentifierRewriteAliasRemoval, OldRef: noteRef("docs/b.md", "Spec"), NewRef: noteRef("docs/b.md", "Spec"), OldIdentifier: "shared-1", NewIdentifier: "SPEC-2", PreferredField: "id", AliasesField: "aliases"}
	content := "Mention SHARED-1. Code `shared-1`. Link [[a|SHARED-1]]. id:: SHARED-1"
	scan := obsidian.ScanStructuredLinkSnapshot(content)
	fieldStart := strings.LastIndex(content, "SHARED-1")

	plan := PlanIdentifierReviewCandidates(IdentifierReviewCandidateInput{
		NotePath: "docs/consumer.md", Content: content, LinkSnapshot: &scan,
		Rewrites:           []IdentifierRewrite{second, first},
		HandledFieldRanges: []ontology.ByteRange{{Start: fieldStart, End: fieldStart + len("SHARED-1")}},
	})

	require.Empty(t, plan.BlockingDiagnostics)
	require.Len(t, plan.Candidates, 2)
	require.Len(t, plan.Diagnostics, 2)
	for index, candidate := range plan.Candidates {
		require.Len(t, candidate.Rewrites, 2)
		require.Equal(t, IdentifierRewriteDiagnosticReviewOnly, plan.Diagnostics[index].Kind)
		require.Len(t, plan.Diagnostics[index].Candidates, 2)
	}
	require.Equal(t, obsidian.IdentifierReviewProse, plan.Candidates[0].Region)
	require.Equal(t, obsidian.IdentifierReviewCode, plan.Candidates[1].Region)
	require.Equal(t, []string{"docs/a.md", "docs/b.md"}, candidateRewritePaths(plan.Candidates[0].Rewrites))
}

func TestIdentifierReviewCandidatePlanIsDeterministicSourceBoundAndMutationSafe(t *testing.T) {
	rewrite := IdentifierRewrite{OldRef: noteRef("docs/a.md", "Spec"), NewRef: noteRef("docs/new-a.md", "Spec"), OldIdentifier: "SPEC-1", NewIdentifier: "SPEC-2", PreferredField: "id", AliasesField: "aliases"}
	content := "SPEC-1"
	scan := obsidian.ScanStructuredLinkSnapshot(content)
	forward := PlanIdentifierReviewCandidates(IdentifierReviewCandidateInput{NotePath: "docs/note.md", Content: content, LinkSnapshot: &scan, Rewrites: []IdentifierRewrite{rewrite}})
	reverse := PlanIdentifierReviewCandidates(IdentifierReviewCandidateInput{NotePath: "docs/note.md", Content: content, LinkSnapshot: &scan, Rewrites: []IdentifierRewrite{rewrite}})
	require.Equal(t, forward.Fingerprint, reverse.Fingerprint)
	require.Equal(t, obsidian.StructuredLinkSourceFingerprint(content), forward.SourceFingerprint)

	snapshot, err := forward.ValidatedSnapshot()
	require.NoError(t, err)
	snapshot.Candidates[0].Value = "mutated"
	again, err := forward.ValidatedSnapshot()
	require.NoError(t, err)
	require.Equal(t, "SPEC-1", again.Candidates[0].Value)

	forward.Candidates[0].Value = "mutated"
	_, err = forward.ValidatedSnapshot()
	require.ErrorContains(t, err, "changed after planning")
}

func TestPlanIdentifierReviewCandidatesRejectsStaleScanAndInvalidHandledRange(t *testing.T) {
	rewrite := IdentifierRewrite{OldRef: noteRef("docs/a.md", "Spec"), NewRef: noteRef("docs/new-a.md", "Spec"), OldIdentifier: "SPEC-1", NewIdentifier: "SPEC-2", PreferredField: "id", AliasesField: "aliases"}
	content := "SPEC-1"
	scan := obsidian.ScanStructuredLinkSnapshot(content)

	stale := PlanIdentifierReviewCandidates(IdentifierReviewCandidateInput{NotePath: "docs/note.md", Content: content + " drift", LinkSnapshot: &scan, Rewrites: []IdentifierRewrite{rewrite}})
	require.Empty(t, stale.Candidates)
	require.NotEmpty(t, stale.BlockingDiagnostics)

	invalidRange := PlanIdentifierReviewCandidates(IdentifierReviewCandidateInput{
		NotePath: "docs/note.md", Content: content, LinkSnapshot: &scan, Rewrites: []IdentifierRewrite{rewrite},
		HandledFieldRanges: []ontology.ByteRange{{Start: -1, End: 2}},
	})
	require.Empty(t, invalidRange.Candidates)
	require.NotEmpty(t, invalidRange.BlockingDiagnostics)
}

func candidateRewritePaths(input []IdentifierRewrite) []string {
	out := make([]string, len(input))
	for index := range input {
		out[index] = input[index].OldRef.NotePath
	}
	return out
}
