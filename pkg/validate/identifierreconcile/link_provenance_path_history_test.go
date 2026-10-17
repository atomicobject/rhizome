package identifierreconcile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAmbiguousLinkGitProvenanceRejectsIndependentSourceBirths(t *testing.T) {
	repo := newGitFixture(t)
	pool := mustPool(t, "SPEC")
	a := claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md")
	b := claim(t, pool, "SPEC-0001", ClaimPreferred, "b.md")
	repo.write(t, "base.md", "# Base\n")
	base := repo.commit(t, "2026-07-15T09:00:00Z", "base")
	branch := repo.git(t, nil, "symbolic-ref", "--short", "HEAD")
	repo.write(t, "a.md", "---\nid: SPEC-0001\n---\n# distinct a\n")
	repo.commit(t, "2026-07-15T10:00:00Z", "left claimant")
	content := "[[SPEC-0001]]\n"
	repo.write(t, "consumer.md", content)
	repo.commit(t, "2026-07-15T11:00:00Z", "left link introduction")
	repo.git(t, nil, "checkout", "-qb", "other", base)
	repo.write(t, "b.md", "---\nid: SPEC-0001\n---\n# distinct b\n")
	repo.commit(t, "2026-07-15T12:00:00Z", "right claimant")
	repo.write(t, "consumer.md", content)
	repo.commit(t, "2026-07-15T13:00:00Z", "right link introduction")
	repo.git(t, nil, "checkout", "-q", branch)
	repo.git(t, nil, "merge", "-q", "--no-ff", "-m", "merge independent identical links", "other")
	request := ambiguousLinkRequest(t, "consumer.md", content, 0, []Claim{a, b})
	result, err := (AmbiguousLinkGitResolver{}).Resolve(t.Context(), repo.dir, []AmbiguousLinkProvenanceRequest{request})
	require.NoError(t, err)
	key := mustAmbiguousLinkRequestKey(t, request)
	require.Nil(t, result.Decisions[key], "neither source birth uniquely proves the intended claimant")
	require.NotEmpty(t, result.Unresolved[key])
}

func TestLinkHistoryRawBoundPrecedesPathFiltering(t *testing.T) {
	history := forkedProvenanceHistory(t, "b.md", "2026-07-15T08:00:00Z")
	output := append(
		gitPathHistoryRecord(history.linkOID, "1784120400", history.leftOID, "A", "consumer.md"),
		gitPathHistoryRecord(history.rightOID, "1784102400", history.baseOID, "A", "b.md")...,
	)
	resolver := AmbiguousLinkGitResolver{maxCommitsPerPath: 1}
	run := func(dir string, stdin []byte, args ...string) ([]byte, []byte, error) {
		for _, arg := range args {
			if arg == "--follow" {
				return output, nil, nil
			}
		}
		return (execGitCommandRunner{}).Run(t.Context(), dir, stdin, []string{"GIT_NO_LAZY_FETCH=1", "LC_ALL=C"}, args...)
	}
	events, reason, err := resolver.loadLinkHistory(t.Context(), history.repo.dir, "consumer.md", run)
	require.NoError(t, err)
	require.Empty(t, events)
	require.NotEmpty(t, reason, "filtering must not hide the max-plus-one sentinel from the source-history owner")
}
