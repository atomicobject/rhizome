package identifierreconcile

import (
	"context"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestAmbiguousLinkGitProvenanceChoosesOnlyClaimantAncestralAtLinkIntroduction(t *testing.T) {
	repo := newGitFixture(t)
	pool := mustPool(t, "SPEC")
	a := claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md")
	b := claim(t, pool, "SPEC-0001", ClaimPreferred, "b.md")
	repo.write(t, "a.md", "---\nid: SPEC-0001\n---\n")
	repo.commit(t, "2026-07-15T09:00:00Z", "add ancestral claimant")
	content := "# Consumer\n\n[[SPEC-0001]]\n"
	repo.write(t, "consumer.md", content)
	repo.commit(t, "2026-07-15T10:00:00Z", "add ambiguous link")
	repo.write(t, "b.md", "---\nid: SPEC-0001\n---\n")
	repo.commit(t, "2026-07-15T08:00:00Z", "add later claimant with misleading author date")

	request := ambiguousLinkRequest(t, "consumer.md", content, 0, []Claim{b, a})
	runner := &recordingGitRunner{delegate: execGitCommandRunner{}}
	result, err := (AmbiguousLinkGitResolver{runner: runner}).Resolve(context.Background(), repo.dir, []AmbiguousLinkProvenanceRequest{request})
	require.NoError(t, err)
	key := mustAmbiguousLinkRequestKey(t, request)
	require.Empty(t, result.Unresolved)
	decision := result.Decisions[key]
	require.NotNil(t, decision)
	snapshot, err := decision.ValidatedSnapshot()
	require.NoError(t, err)
	require.Equal(t, nodeRefForCanonicalKey(a.Node), snapshot.Winner)
	require.Len(t, snapshot.Evidence.Candidates, 2)
	require.True(t, snapshot.Evidence.Candidates[0].Ancestor)
	require.False(t, snapshot.Evidence.Candidates[1].Ancestor)

	decision.Winner = nodeRefForCanonicalKey(b.Node)
	_, err = decision.ValidatedSnapshot()
	require.Error(t, err)
	for index, env := range runner.envs {
		require.Contains(t, env, "GIT_NO_LAZY_FETCH=1", "command %d", index)
		require.Contains(t, env, "LC_ALL=C", "command %d", index)
	}
	for _, args := range runner.calls {
		require.NotContains(t, args, "fetch")
	}
}

func TestAmbiguousLinkGitProvenanceLeavesMultipleAncestralClaimantsBlocking(t *testing.T) {
	repo := newGitFixture(t)
	pool := mustPool(t, "SPEC")
	a := claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md")
	b := claim(t, pool, "SPEC-0001", ClaimPreferred, "b.md")
	repo.write(t, "a.md", "---\nid: SPEC-0001\n---\n")
	repo.write(t, "b.md", "---\nid: SPEC-0001\n---\n")
	repo.commit(t, "2026-07-15T09:00:00Z", "add both claimants")
	content := "[[SPEC-0001]]\n"
	repo.write(t, "consumer.md", content)
	repo.commit(t, "2026-07-15T10:00:00Z", "add ambiguous link")

	request := ambiguousLinkRequest(t, "consumer.md", content, 0, []Claim{a, b})
	result, err := AmbiguousLinkGitResolver{}.Resolve(context.Background(), repo.dir, []AmbiguousLinkProvenanceRequest{request})
	require.NoError(t, err)
	key := mustAmbiguousLinkRequestKey(t, request)
	require.Nil(t, result.Decisions[key])
	require.Contains(t, result.Unresolved[key], "non-unique")
}

func TestAmbiguousLinkGitProvenanceUsesSourceBranchAncestryNotAuthorDates(t *testing.T) {
	repo := newGitFixture(t)
	pool := mustPool(t, "SPEC")
	a := claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md")
	b := claim(t, pool, "SPEC-0001", ClaimPreferred, "b.md")
	repo.write(t, "base.md", "# Base\n")
	baseOID := repo.commit(t, "2026-07-15T09:00:00Z", "base")
	sourceBranch := strings.TrimSpace(repo.git(t, nil, "symbolic-ref", "--short", "HEAD"))
	repo.write(t, "a.md", "---\nid: SPEC-0001\n---\n")
	repo.commit(t, "2026-07-15T12:00:00Z", "add claimant on source branch")
	content := "[[SPEC-0001]]\n"
	repo.write(t, "consumer.md", content)
	repo.commit(t, "2026-07-15T13:00:00Z", "add branch-local link")
	repo.git(t, nil, "branch", "other", baseOID)
	repo.git(t, nil, "checkout", "-q", "other")
	repo.write(t, "b.md", "---\nid: SPEC-0001\n---\n")
	repo.commit(t, "2026-07-15T08:00:00Z", "add claimant with earlier author date on other branch")
	repo.git(t, nil, "checkout", "-q", sourceBranch)
	repo.git(t, nil, "merge", "-q", "--no-ff", "-m", "merge other claimant", "other")

	request := ambiguousLinkRequest(t, "consumer.md", content, 0, []Claim{b, a})
	result, err := AmbiguousLinkGitResolver{}.Resolve(context.Background(), repo.dir, []AmbiguousLinkProvenanceRequest{request})
	require.NoError(t, err)
	decision := result.Decisions[mustAmbiguousLinkRequestKey(t, request)]
	require.NotNil(t, decision, "%+v", result.Unresolved)
	require.Equal(t, nodeRefForCanonicalKey(a.Node), decision.Winner)
}

func TestAmbiguousLinkGitProvenanceBatchesSharedSourceHistory(t *testing.T) {
	repo := newGitFixture(t)
	pool := mustPool(t, "SPEC")
	claims := []Claim{
		claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md"), claim(t, pool, "SPEC-0001", ClaimPreferred, "a-late.md"),
		claim(t, pool, "SPEC-0002", ClaimPreferred, "b.md"), claim(t, pool, "SPEC-0002", ClaimPreferred, "b-late.md"),
	}
	repo.write(t, "a.md", "---\nid: SPEC-0001\n---\n")
	repo.write(t, "b.md", "---\nid: SPEC-0002\n---\n")
	repo.commit(t, "2026-07-15T09:00:00Z", "add ancestral claimants")
	content := "[[SPEC-0001]] [[SPEC-0002]]\n"
	repo.write(t, "consumer.md", content)
	repo.commit(t, "2026-07-15T10:00:00Z", "add links")
	repo.write(t, "a-late.md", "---\nid: SPEC-0001\n---\n")
	repo.write(t, "b-late.md", "---\nid: SPEC-0002\n---\n")
	repo.commit(t, "2026-07-15T11:00:00Z", "add late claimants")

	requests := []AmbiguousLinkProvenanceRequest{
		ambiguousLinkRequest(t, "consumer.md", content, 0, claims[:2]),
		ambiguousLinkRequest(t, "consumer.md", content, 1, claims[2:]),
	}
	runner := &recordingGitRunner{delegate: execGitCommandRunner{}}
	result, err := (AmbiguousLinkGitResolver{runner: runner}).Resolve(context.Background(), repo.dir, requests)
	require.NoError(t, err)
	require.Len(t, result.Decisions, 2)
	require.Equal(t, 1, result.Stats.StructuredBlobsScanned)
	sourceLogs := 0
	ancestryWalks := 0
	for _, args := range runner.calls {
		if strings.Contains(strings.Join(args, " "), "log --follow") && args[len(args)-1] == "consumer.md" {
			sourceLogs++
		}
		if len(args) > 0 && args[0] == "rev-list" {
			ancestryWalks++
		}
	}
	require.Equal(t, 1, sourceLogs)
	require.Equal(t, 1, ancestryWalks)
}

func TestAmbiguousLinkGitProvenanceIsolatesDirtySourceFallback(t *testing.T) {
	repo := newGitFixture(t)
	pool := mustPool(t, "SPEC")
	a := claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md")
	b := claim(t, pool, "SPEC-0001", ClaimPreferred, "b.md")
	repo.write(t, "a.md", "---\nid: SPEC-0001\n---\n")
	repo.commit(t, "2026-07-15T09:00:00Z", "add ancestral claimant")
	goodContent := "[[SPEC-0001]]\n"
	badCommitted := "# Tracked\n[[SPEC-0001]]\n"
	repo.write(t, "good.md", goodContent)
	repo.write(t, "bad.md", badCommitted)
	repo.commit(t, "2026-07-15T10:00:00Z", "add links")
	repo.write(t, "b.md", "---\nid: SPEC-0001\n---\n")
	repo.commit(t, "2026-07-15T11:00:00Z", "add later claimant")
	badDirty := "# Dirty\n[[SPEC-0001]]\n"
	repo.write(t, "bad.md", badDirty)

	good := ambiguousLinkRequest(t, "good.md", goodContent, 0, []Claim{a, b})
	bad := ambiguousLinkRequest(t, "bad.md", badDirty, 0, []Claim{a, b})
	result, err := AmbiguousLinkGitResolver{}.Resolve(context.Background(), repo.dir, []AmbiguousLinkProvenanceRequest{bad, good})
	require.NoError(t, err)
	require.NotNil(t, result.Decisions[mustAmbiguousLinkRequestKey(t, good)])
	require.Contains(t, result.Unresolved[mustAmbiguousLinkRequestKey(t, bad)], "introduction")
}

func TestAmbiguousLinkGitProvenanceFallsBackWhenAncestryWalkHitsBound(t *testing.T) {
	repo := newGitFixture(t)
	pool := mustPool(t, "SPEC")
	a := claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md")
	b := claim(t, pool, "SPEC-0001", ClaimPreferred, "b.md")
	repo.write(t, "a.md", "---\nid: SPEC-0001\n---\n")
	repo.commit(t, "2026-07-15T09:00:00Z", "add ancestral claimant")
	content := "[[SPEC-0001]]\n"
	repo.write(t, "consumer.md", content)
	repo.commit(t, "2026-07-15T10:00:00Z", "add link")
	repo.write(t, "b.md", "---\nid: SPEC-0001\n---\n")
	repo.commit(t, "2026-07-15T11:00:00Z", "add later claimant")

	request := ambiguousLinkRequest(t, "consumer.md", content, 0, []Claim{a, b})
	result, err := (AmbiguousLinkGitResolver{maxCommitsPerPath: 1}).Resolve(context.Background(), repo.dir, []AmbiguousLinkProvenanceRequest{request})
	require.NoError(t, err)
	key := mustAmbiguousLinkRequestKey(t, request)
	require.Nil(t, result.Decisions[key])
	require.Contains(t, result.Unresolved[key], "ancestry")
}

func ambiguousLinkRequest(t *testing.T, notePath, content string, linkIndex int, candidates []Claim) AmbiguousLinkProvenanceRequest {
	t.Helper()
	scan := obsidian.ScanStructuredLinkSnapshot(content)
	snapshot, err := scan.ValidatedSnapshot()
	require.NoError(t, err)
	require.Greater(t, len(snapshot.Links), linkIndex)
	return AmbiguousLinkProvenanceRequest{
		NotePath: notePath, SourceHash: snapshot.SourceFingerprint, Content: content, LinkSnapshot: &scan,
		LinkIndex: linkIndex, RawTarget: snapshot.Links[linkIndex].Target, Candidates: candidates,
	}
}

func mustAmbiguousLinkRequestKey(t *testing.T, request AmbiguousLinkProvenanceRequest) string {
	t.Helper()
	normalized, err := normalizeAmbiguousLinkRequest(request)
	require.NoError(t, err)
	return normalized.key
}

func TestAmbiguousLinkDecisionRejectsUnsortedOrIncompleteEvidence(t *testing.T) {
	refA := ontology.NodeRef{NotePath: "a.md", TypeName: "Spec", Kind: ontology.NodeKindNote}
	refB := ontology.NodeRef{NotePath: "b.md", TypeName: "Spec", Kind: ontology.NodeKindNote}
	valid := func() ambiguousLinkDecisionPayload {
		return ambiguousLinkDecisionPayload{
			NotePath: "consumer.md", SourceHash: strings.Repeat("a", 64), LinkIndex: 0, RawTarget: "SHARED-ID",
			CandidateRefs: []ontology.NodeRef{refA, refB}, Winner: refA,
			Evidence: AmbiguousLinkGitEvidence{SourceIntroductionOID: strings.Repeat("1", 40), Candidates: []LinkCandidateAncestry{
				{Ref: refA, ClaimID: "claim-a", IntroductionOID: strings.Repeat("2", 40), Complete: true, Ancestor: true},
				{Ref: refB, ClaimID: "claim-b", IntroductionOID: strings.Repeat("3", 40), Complete: true},
			}},
		}
	}
	accepted, err := sealAmbiguousLinkDecision(valid()).ValidatedSnapshot()
	require.NoError(t, err, "a complete, sorted decision with one ancestral winner is the positive control")
	require.True(t, sameRepairRef(refA, accepted.Winner))

	tests := []struct {
		name   string
		mutate func(*ambiguousLinkDecisionPayload)
	}{
		{name: "incomplete candidate history", mutate: func(payload *ambiguousLinkDecisionPayload) {
			payload.Evidence.Candidates[1].Complete = false
		}},
		{name: "missing claim identity", mutate: func(payload *ambiguousLinkDecisionPayload) {
			payload.Evidence.Candidates[1].ClaimID = ""
		}},
		{name: "unsorted candidates", mutate: func(payload *ambiguousLinkDecisionPayload) {
			payload.CandidateRefs = []ontology.NodeRef{refB, refA}
			payload.Evidence.Candidates[0], payload.Evidence.Candidates[1] = payload.Evidence.Candidates[1], payload.Evidence.Candidates[0]
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := valid()
			tt.mutate(&payload)
			// Resealing reaches the structural validator rather than the
			// fingerprint check.
			_, err := sealAmbiguousLinkDecision(payload).ValidatedSnapshot()
			require.EqualError(t, err, "ambiguous link candidate evidence is incomplete or unsorted")
		})
	}
}
