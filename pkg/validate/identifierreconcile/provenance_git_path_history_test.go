package identifierreconcile

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAmbiguousLinkGitProvenanceFiltersInterleavedPathHistories(t *testing.T) {
	history := forkedProvenanceHistory(t, "b.md", "2026-07-15T08:00:00Z")
	pool := mustPool(t, "SPEC")
	a := claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md")
	b := claim(t, pool, "SPEC-0001", ClaimPreferred, "b.md")
	request := ambiguousLinkRequest(t, "consumer.md", "[[SPEC-0001]]\n", 0, []Claim{b, a})
	runner := &recordingGitRunner{delegate: pathHistoryGitRunner{output: append(
		gitPathHistoryRecord(history.rightOID, "1784102400", history.baseOID, "A", "b.md"),
		gitPathHistoryRecord(history.leftOID, "1784116800", history.baseOID, "A", "a.md")...,
	)}}
	result, err := (AmbiguousLinkGitResolver{runner: runner}).Resolve(t.Context(), history.repo.dir, []AmbiguousLinkProvenanceRequest{request})
	require.NoError(t, err)
	require.Empty(t, result.Unresolved)
	decision := result.Decisions[mustAmbiguousLinkRequestKey(t, request)]
	require.NotNil(t, decision)
	snapshot, err := decision.ValidatedSnapshot()
	require.NoError(t, err)
	require.Equal(t, nodeRefForCanonicalKey(a.Node), snapshot.Winner)
	require.Equal(t, history.linkOID, snapshot.Evidence.SourceIntroductionOID)
	require.Len(t, snapshot.Evidence.Candidates, 2)
	require.Equal(t, history.leftOID, snapshot.Evidence.Candidates[0].IntroductionOID)
	require.True(t, snapshot.Evidence.Candidates[0].Complete)
	require.True(t, snapshot.Evidence.Candidates[0].Ancestor)
	require.Equal(t, history.rightOID, snapshot.Evidence.Candidates[1].IntroductionOID)
	require.True(t, snapshot.Evidence.Candidates[1].Complete)
	require.False(t, snapshot.Evidence.Candidates[1].Ancestor)
	require.Equal(t, 1, graphQueryCount(runner), "one owned addition per path adds no lineage graph queries")
}

func TestGitProvenanceRejectsIndependentSamePathIntroductions(t *testing.T) {
	for _, date := range []string{"2026-07-15T08:00:00Z", "2026-07-15T14:00:00Z"} {
		t.Run(date, func(t *testing.T) {
			history := forkedProvenanceHistory(t, "a.md", date)
			a := claim(t, mustPool(t, "SPEC"), "SPEC-0001", ClaimPreferred, "a.md")
			result, err := (GitProvenanceResolver{}).Resolve(t.Context(), history.repo.dir, []Claim{a})
			require.NoError(t, err)
			require.False(t, result.Evidence[a.ID()].Complete, "independent births at one path cannot prove unique introduction")
			require.Empty(t, result.Evidence[a.ID()].FullOID)
		})
	}
}

func TestGitProvenanceRejectsDeletedAndReaddedPath(t *testing.T) {
	repo := newGitFixture(t)
	const content = "---\nid: SPEC-0001\n---\n"
	repo.write(t, "a.md", content)
	repo.commit(t, "2026-07-15T09:00:00Z", "original path")
	require.NoError(t, os.Rename(filepath.Join(repo.dir, "a.md"), filepath.Join(t.TempDir(), "held.md")))
	repo.commit(t, "2026-07-15T10:00:00Z", "remove original path")
	repo.write(t, "a.md", content)
	repo.commit(t, "2026-07-15T11:00:00Z", "new path with same identifier")
	a := claim(t, mustPool(t, "SPEC"), "SPEC-0001", ClaimPreferred, "a.md")
	result, err := (GitProvenanceResolver{}).Resolve(t.Context(), repo.dir, []Claim{a})
	require.NoError(t, err)
	require.False(t, result.Evidence[a.ID()].Complete)
	require.Empty(t, result.Evidence[a.ID()].FullOID)
}

func TestGitProvenanceRejectsMergedDeletedAndReaddedPath(t *testing.T) {
	repo := newGitFixture(t)
	const content = "---\nid: SPEC-0001\n---\n"
	repo.write(t, "a.md", content)
	base := repo.commit(t, "2026-07-15T09:00:00Z", "original path")
	branch := repo.git(t, nil, "symbolic-ref", "--short", "HEAD")
	repo.git(t, nil, "branch", "other", base)
	require.NoError(t, os.Rename(filepath.Join(repo.dir, "a.md"), filepath.Join(t.TempDir(), "held.md")))
	repo.commit(t, "2026-07-15T10:00:00Z", "remove original path")
	repo.write(t, "a.md", content)
	repo.commit(t, "2026-07-15T11:00:00Z", "new path with same identifier")
	repo.git(t, nil, "checkout", "-q", "other")
	repo.write(t, "other.md", "# Other branch\n")
	repo.commit(t, "2026-07-15T12:00:00Z", "independent branch update")
	repo.git(t, nil, "checkout", "-q", branch)
	repo.git(t, nil, "merge", "-q", "--no-ff", "-m", "merge after path recreation", "other")
	a := claim(t, mustPool(t, "SPEC"), "SPEC-0001", ClaimPreferred, "a.md")
	result, err := (GitProvenanceResolver{}).Resolve(t.Context(), repo.dir, []Claim{a})
	require.NoError(t, err)
	require.False(t, result.Evidence[a.ID()].Complete)
	require.Empty(t, result.Evidence[a.ID()].FullOID)
}

func TestGitProvenancePathFilteringPreservesHistoryGuards(t *testing.T) {
	history := forkedProvenanceHistory(t, "b.md", "2026-07-15T08:00:00Z")
	a := claim(t, mustPool(t, "SPEC"), "SPEC-0001", ClaimPreferred, "a.md")
	own := gitPathHistoryRecord(history.leftOID, "1784116800", history.baseOID, "A", "a.md")
	foreign := gitPathHistoryRecord(history.rightOID, "1784102400", history.baseOID, "A", "b.md")
	for _, tc := range []struct {
		name     string
		output   []byte
		limit    int
		complete bool
	}{
		{"foreign addition before owned birth", append(append([]byte(nil), foreign...), own...), 10, true},
		{"foreign modification after owned birth", append(append([]byte(nil), own...), gitPathHistoryRecord(history.rightOID, "1784102400", history.baseOID, "M", "b.md")...), 10, true},
		{"foreign only", foreign, 10, false},
		{"raw bound before filtering", append(append([]byte(nil), own...), foreign...), 1, false},
		{"malformed foreign record", append(gitPathHistoryRecord(history.rightOID, "1784102400", history.baseOID, "A", "b.md", "extra.md"), own...), 10, false},
		{"foreign rename", append(gitPathHistoryRecord(history.rightOID, "1784102400", history.baseOID, "R100", "old.md", "b.md"), own...), 10, false},
		{"foreign copy", append(gitPathHistoryRecord(history.rightOID, "1784102400", history.baseOID, "C100", "old.md", "b.md"), own...), 10, false},
		{"foreign merge", append(gitPathHistoryRecord(history.rightOID, "1784102400", history.baseOID+" "+history.leftOID, "A", "b.md"), own...), 10, false},
		{"foreign malformed parent", append(gitPathHistoryRecord(history.rightOID, "1784102400", "invalid", "A", "b.md"), own...), 10, false},
		{"foreign malformed oid", append(gitPathHistoryRecord("invalid", "1784102400", history.baseOID, "A", "b.md"), own...), 10, false},
		{"foreign malformed status", append(gitPathHistoryRecord(history.rightOID, "1784102400", history.baseOID, "Aanything", "b.md"), own...), 10, false},
		{"malformed suffix after owned birth", append(append([]byte(nil), own...), []byte("\x1ebroken")...), 10, false},
		{"merge suffix after owned birth", append(append([]byte(nil), own...), gitPathHistoryRecord(history.rightOID, "1784102400", history.baseOID+" "+history.leftOID, "M", "a.md")...), 10, false},
		{"owned copy boundary", append(gitPathHistoryRecord(history.leftOID, "1784116800", history.baseOID, "C100", "old.md", "a.md"), foreign...), 10, true},
		{"invalid suffix after owned copy", append(gitPathHistoryRecord(history.leftOID, "1784116800", history.baseOID, "C100", "old.md", "a.md"), []byte("\x1ebroken")...), 10, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := (GitProvenanceResolver{runner: pathHistoryGitRunner{output: tc.output}, maxCommitsPerPath: tc.limit}).Resolve(t.Context(), history.repo.dir, []Claim{a})
			require.NoError(t, err)
			require.Equal(t, tc.complete, result.Evidence[a.ID()].Complete)
			if tc.complete {
				require.Equal(t, history.leftOID, result.Evidence[a.ID()].FullOID)
			} else {
				require.Empty(t, result.Evidence[a.ID()].FullOID)
			}
		})
	}
}

type forkedGitPathHistory struct {
	repo                                gitFixture
	baseOID, leftOID, rightOID, linkOID string
}

func forkedProvenanceHistory(t *testing.T, otherPath, otherDate string) forkedGitPathHistory {
	t.Helper()
	history := forkedGitPathHistory{repo: newGitFixture(t)}
	repo := history.repo
	repo.write(t, "base.md", "# Base\n")
	history.baseOID = repo.commit(t, "2026-07-15T09:00:00Z", "base")
	sourceBranch := repo.git(t, nil, "symbolic-ref", "--short", "HEAD")
	repo.write(t, "a.md", "---\nid: SPEC-0001\n---\n")
	history.leftOID = repo.commit(t, "2026-07-15T12:00:00Z", "add claimant on source branch")
	repo.write(t, "consumer.md", "[[SPEC-0001]]\n")
	history.linkOID = repo.commit(t, "2026-07-15T13:00:00Z", "add branch-local link")
	repo.git(t, nil, "branch", "other", history.baseOID)
	repo.git(t, nil, "checkout", "-q", "other")
	repo.write(t, otherPath, "---\nid: SPEC-0001\n---\n")
	history.rightOID = repo.commit(t, otherDate, "add claimant with earlier author date on other branch")
	repo.git(t, nil, "checkout", "-q", sourceBranch)
	repo.git(t, nil, "merge", "-q", "--no-ff", "-m", "merge other claimant", "other")
	return history
}

func gitPathHistoryRecord(oid, author, parents, status string, paths ...string) []byte {
	return []byte("\x1e" + oid + "\x00" + author + "\x00" + parents + "\x00\n" + status + "\x00" + strings.Join(paths, "\x00") + "\x00")
}

type pathHistoryGitRunner struct{ output []byte }

func (r pathHistoryGitRunner) Run(ctx context.Context, dir string, stdin []byte, env []string, args ...string) ([]byte, []byte, error) {
	if len(args) > 0 && (args[len(args)-1] == "a.md" || args[len(args)-1] == "b.md") {
		for _, arg := range args {
			if arg == "--follow" {
				return r.output, nil, nil
			}
		}
	}
	return (execGitCommandRunner{}).Run(ctx, dir, stdin, env, args...)
}
