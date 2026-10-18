package identifierreconcile

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAmbiguousLinkGitProvenanceRejectsSiblingSourceModifications(t *testing.T) {
	for _, laterEdit := range []bool{false, true} {
		name := "at merge"
		if laterEdit {
			name = "owned edit after merge"
		}
		t.Run(name, func(t *testing.T) {
			repo := newGitFixture(t)
			repo.write(t, "consumer.md", "# No link\n")
			base := repo.commit(t, "2026-07-15T09:00:00Z", "base source")
			branch := repo.git(t, nil, "symbolic-ref", "--short", "HEAD")
			repo.write(t, "a.md", "---\nid: SPEC-0001\n---\n# Distinct a\n")
			repo.commit(t, "2026-07-15T10:00:00Z", "left claimant")
			content := "[[SPEC-0001]]\n"
			repo.write(t, "consumer.md", content)
			repo.commit(t, "2026-07-15T11:00:00Z", "left link modification")
			repo.git(t, nil, "checkout", "-qb", "other", base)
			repo.write(t, "b.md", "---\nid: SPEC-0001\n---\n# Distinct b\n")
			repo.commit(t, "2026-07-15T12:00:00Z", "right claimant")
			repo.write(t, "consumer.md", content)
			repo.commit(t, "2026-07-15T13:00:00Z", "right link modification")
			repo.git(t, nil, "checkout", "-q", branch)
			repo.git(t, nil, "merge", "-q", "--no-ff", "-m", "merge same source modifications", "other")
			if laterEdit {
				content += "# Later edit\n"
				repo.write(t, "consumer.md", content)
				repo.commit(t, "2026-07-15T15:00:00Z", "owned edit after merge")
			}
			pool := mustPool(t, "SPEC")
			a := claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md")
			b := claim(t, pool, "SPEC-0001", ClaimPreferred, "b.md")
			request := ambiguousLinkRequest(t, "consumer.md", content, 0, []Claim{a, b})
			result, err := (AmbiguousLinkGitResolver{}).Resolve(t.Context(), repo.dir, []AmbiguousLinkProvenanceRequest{request})
			require.NoError(t, err)
			key := mustAmbiguousLinkRequestKey(t, request)
			require.Nil(t, result.Decisions[key], "sibling modifications do not prove a unique link introduction")
			require.NotEmpty(t, result.Unresolved[key])
		})
	}
}

func TestGitProvenanceRejectsSiblingCandidateModifications(t *testing.T) {
	for _, laterEdit := range []bool{false, true} {
		name := "at merge"
		if laterEdit {
			name = "owned edit after merge"
		}
		t.Run(name, func(t *testing.T) {
			repo := newGitFixture(t)
			repo.write(t, "a.md", "---\nid: SPEC-0000\n---\n")
			base := repo.commit(t, "2026-07-15T09:00:00Z", "base claimant")
			branch := repo.git(t, nil, "symbolic-ref", "--short", "HEAD")
			repo.write(t, "a.md", "---\nid: SPEC-0001\n---\n")
			repo.commit(t, "2026-07-15T11:00:00Z", "left claim modification")
			repo.git(t, nil, "checkout", "-qb", "other", base)
			repo.write(t, "a.md", "---\nid: SPEC-0001\n---\n")
			repo.commit(t, "2026-07-15T08:00:00Z", "right claim modification")
			repo.git(t, nil, "checkout", "-q", branch)
			repo.git(t, nil, "merge", "-q", "--no-ff", "-m", "merge same candidate modifications", "other")
			if laterEdit {
				repo.write(t, "a.md", "---\nid: SPEC-0001\n---\n# Later edit\n")
				repo.commit(t, "2026-07-15T15:00:00Z", "owned edit after merge")
			}
			a := claim(t, mustPool(t, "SPEC"), "SPEC-0001", ClaimPreferred, "a.md")
			result, err := (GitProvenanceResolver{}).Resolve(t.Context(), repo.dir, []Claim{a})
			require.NoError(t, err)
			require.False(t, result.Evidence[a.ID()].Complete, "sibling modifications do not prove a unique claim introduction")
			require.Empty(t, result.Evidence[a.ID()].FullOID)
		})
	}
}

func TestGitProvenanceLinearHistoryProvesGapsOncePerPath(t *testing.T) {
	for _, gap := range []bool{false, true} {
		name, wantQueries := "direct parents", 0
		if gap {
			name, wantQueries = "intervening unrelated commits", 1
		}
		t.Run(name, func(t *testing.T) {
			fixture := linearCandidateHistory(t, gap)
			runner := &recordingGitRunner{delegate: execGitCommandRunner{}}
			result, err := (GitProvenanceResolver{runner: runner}).Resolve(t.Context(), fixture.repo.dir, fixture.claims)
			require.NoError(t, err)
			for _, claim := range fixture.claims {
				require.True(t, result.Evidence[claim.ID()].Complete, "%+v", result.Evidence[claim.ID()])
				require.Equal(t, fixture.introduced, result.Evidence[claim.ID()].FullOID)
			}
			require.Equal(t, wantQueries, graphQueryCount(runner), "two claims on one path share its lineage proof")
			require.Equal(t, len(runner.calls), result.Stats.Commands)
			for index, args := range runner.calls {
				require.NotContains(t, args, "fetch")
				require.Contains(t, runner.envs[index], "GIT_NO_LAZY_FETCH=1")
			}
		})
	}
}

func TestGitProvenanceLineageRejectsIncompleteGraph(t *testing.T) {
	fixture := linearCandidateHistory(t, true)
	valid := fixture.repo.git(t, nil, "rev-list", "--parents", "--topo-order", fixture.tip) + "\n"
	for _, tc := range []struct {
		name, output string
		limit        int
		failure      error
	}{
		{name: "empty graph"},
		{name: "raw graph sentinel", output: valid, limit: 4},
		{name: "bad header", output: "invalid\n"},
		{name: "bad parent", output: fixture.tip + " invalid\n"},
		{name: "missing parent", output: fixture.tip + " " + fixture.introduced + "\n"},
		{name: "duplicate record", output: valid + fixture.introduced + "\n"},
		{name: "parent before child", output: fixture.tip + " " + fixture.introduced + "\n" + fixture.introduced + "\n" + strings.Repeat("f", 40) + " " + fixture.introduced + "\n"},
		{name: "cyclic graph", output: fixture.tip + " " + fixture.introduced + "\n" + fixture.introduced + " " + fixture.tip + "\n"},
		{name: "disconnected record", output: valid + strings.Repeat("f", 40) + "\n"},
		{name: "oversized parent record", output: fixture.tip + strings.Repeat(" "+fixture.introduced, 5) + "\n" + fixture.introduced + "\n", limit: 4},
		{name: "command failure", failure: errors.New("injected graph failure")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &recordingGitRunner{delegate: lineageGraphFaultRunner{output: []byte(tc.output), failure: tc.failure}}
			result, err := (GitProvenanceResolver{runner: runner, maxCommitsPerPath: tc.limit}).Resolve(t.Context(), fixture.repo.dir, fixture.claims)
			require.NoError(t, err)
			for _, claim := range fixture.claims {
				require.False(t, result.Evidence[claim.ID()].Complete)
				require.Empty(t, result.Evidence[claim.ID()].FullOID)
			}
			require.Equal(t, 1, graphQueryCount(runner))
		})
	}
}

func TestGitProvenanceLineageCancellationIsReturned(t *testing.T) {
	fixture := linearCandidateHistory(t, true)
	for _, returnedGraph := range []bool{false, true} {
		name := "during command"
		if returnedGraph {
			name = "after graph response"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			runner := lineageGraphFaultRunner{cancel: cancel}
			if returnedGraph {
				runner.output = []byte(fixture.repo.git(t, nil, "rev-list", "--parents", "--topo-order", fixture.tip) + "\n")
			}
			result, err := (GitProvenanceResolver{runner: runner}).Resolve(ctx, fixture.repo.dir, fixture.claims)
			require.ErrorIs(t, err, context.Canceled)
			for _, claim := range fixture.claims {
				require.False(t, result.Evidence[claim.ID()].Complete)
			}
		})
	}
}

type linearProvenanceFixture struct {
	repo            gitFixture
	introduced, tip string
	claims          []Claim
	requests        []AmbiguousLinkProvenanceRequest
}

func linearCandidateHistory(t *testing.T, gap bool) linearProvenanceFixture {
	t.Helper()
	fixture := linearProvenanceFixture{repo: newGitFixture(t)}
	repo := fixture.repo
	const header = "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n"
	repo.write(t, "a.md", header+"# Original\n")
	fixture.introduced = repo.commit(t, "2026-07-15T09:00:00Z", "introduce claim")
	if gap {
		repo.write(t, "unrelated.md", "# Unrelated\n")
		repo.commit(t, "2026-07-15T10:00:00Z", "unrelated change")
	}
	repo.write(t, "a.md", header+"# First body edit\n")
	repo.commit(t, "2026-07-15T11:00:00Z", "edit claim body")
	if gap {
		repo.write(t, "unrelated.md", "# Another unrelated change\n")
		repo.commit(t, "2026-07-15T12:00:00Z", "another unrelated change")
	}
	repo.write(t, "a.md", header+"# Final body edit\n")
	fixture.tip = repo.commit(t, "2026-07-15T13:00:00Z", "final claim body")
	pool := mustPool(t, "SPEC")
	fixture.claims = []Claim{claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md"), claim(t, pool, "SPEC-0001", ClaimAlias, "a.md")}
	return fixture
}

type lineageGraphFaultRunner struct {
	output  []byte
	failure error
	cancel  context.CancelFunc
}

func (r lineageGraphFaultRunner) Run(ctx context.Context, dir string, stdin []byte, env []string, args ...string) ([]byte, []byte, error) {
	if len(args) > 0 && args[0] == "rev-list" {
		if r.cancel != nil {
			r.cancel()
			if r.output != nil {
				return r.output, nil, r.failure
			}
			return nil, nil, ctx.Err()
		}
		return r.output, nil, r.failure
	}
	return (execGitCommandRunner{}).Run(ctx, dir, stdin, env, args...)
}

func graphQueryCount(runner *recordingGitRunner) int {
	count := 0
	for _, args := range runner.calls {
		if len(args) > 0 && args[0] == "rev-list" {
			count++
		}
	}
	return count
}
