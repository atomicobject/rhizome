package identifierreconcile

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAmbiguousLinkGitProvenanceLineageProofIsSharedBySourcePath(t *testing.T) {
	for _, gap := range []bool{false, true} {
		name, wantQueries := "direct parents", 1
		if gap {
			name, wantQueries = "intervening unrelated commits", 2
		}
		t.Run(name, func(t *testing.T) {
			fixture := linearSourceHistory(t, gap)
			runner := &recordingGitRunner{delegate: execGitCommandRunner{}}
			result, err := (AmbiguousLinkGitResolver{runner: runner}).Resolve(t.Context(), fixture.repo.dir, fixture.requests)
			require.NoError(t, err)
			require.Empty(t, result.Unresolved)
			require.Len(t, result.Decisions, 2)
			for index, request := range fixture.requests {
				decision := result.Decisions[mustAmbiguousLinkRequestKey(t, request)]
				require.NotNil(t, decision)
				snapshot, err := decision.ValidatedSnapshot()
				require.NoError(t, err)
				require.Equal(t, fixture.introduced, snapshot.Evidence.SourceIntroductionOID)
				require.Equal(t, nodeRefForCanonicalKey(fixture.claims[index*2].Node), snapshot.Winner)
			}
			require.Equal(t, 3, result.Stats.StructuredBlobsScanned, "two links share the source's three historical versions")
			require.Equal(t, wantQueries, graphQueryCount(runner), "one gap proof and one introduction graph are shared by both requests")
			require.Equal(t, len(runner.calls), result.Stats.Commands)
		})
	}
}

func TestAmbiguousLinkGitProvenanceLineageRejectsIncompleteGraph(t *testing.T) {
	fixture := linearSourceHistory(t, true)
	for _, tc := range []struct {
		name    string
		limit   int
		output  []byte
		failure error
		cancel  bool
	}{
		{name: "raw graph sentinel", limit: 4},
		{name: "malformed graph", output: []byte("invalid\n")},
		{name: "graph command failed", failure: errors.New("injected graph failure")},
		{name: "cancelled graph", cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var delegate gitCommandRunner = lineageGraphFaultRunner{output: tc.output, failure: tc.failure}
			if tc.limit != 0 {
				delegate = execGitCommandRunner{}
			}
			if tc.cancel {
				delegate = lineageGraphFaultRunner{cancel: cancel}
			}
			runner := &recordingGitRunner{delegate: delegate}
			result, err := (AmbiguousLinkGitResolver{runner: runner, maxCommitsPerPath: tc.limit}).Resolve(ctx, fixture.repo.dir, fixture.requests)
			if tc.cancel {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.NoError(t, err)
				require.Len(t, result.Unresolved, 2)
			}
			require.Empty(t, result.Decisions)
			require.Equal(t, 1, graphQueryCount(runner), "failed lineage proof must not reach winner certification")
		})
	}
}

func linearSourceHistory(t *testing.T, gap bool) linearProvenanceFixture {
	t.Helper()
	fixture := linearProvenanceFixture{repo: newGitFixture(t)}
	repo := fixture.repo
	repo.write(t, "a.md", "---\nid: SPEC-0001\n---\n# First A\n")
	repo.write(t, "c.md", "---\nid: SPEC-0002\n---\n# First C\n")
	repo.commit(t, "2026-07-15T08:00:00Z", "original claimants")
	const links = "[[SPEC-0001]] [[SPEC-0002]]\n"
	repo.write(t, "consumer.md", links)
	fixture.introduced = repo.commit(t, "2026-07-15T09:00:00Z", "introduce links")
	if gap {
		repo.write(t, "unrelated.md", "# Unrelated\n")
		repo.commit(t, "2026-07-15T10:00:00Z", "unrelated change")
	}
	repo.write(t, "consumer.md", links+"# First body edit\n")
	repo.commit(t, "2026-07-15T11:00:00Z", "edit consumer body")
	if gap {
		repo.write(t, "unrelated.md", "# Another unrelated change\n")
		repo.commit(t, "2026-07-15T12:00:00Z", "another unrelated change")
	}
	content := links + "# Final body edit\n"
	repo.write(t, "consumer.md", content)
	fixture.tip = repo.commit(t, "2026-07-15T13:00:00Z", "final consumer body")
	repo.write(t, "b.md", "---\nid: SPEC-0001\n---\n# Late B\n")
	repo.write(t, "d.md", "---\nid: SPEC-0002\n---\n# Late D\n")
	repo.commit(t, "2026-07-15T14:00:00Z", "later claimants")
	pool := mustPool(t, "SPEC")
	fixture.claims = []Claim{
		claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md"), claim(t, pool, "SPEC-0001", ClaimPreferred, "b.md"),
		claim(t, pool, "SPEC-0002", ClaimPreferred, "c.md"), claim(t, pool, "SPEC-0002", ClaimPreferred, "d.md"),
	}
	fixture.requests = []AmbiguousLinkProvenanceRequest{
		ambiguousLinkRequest(t, "consumer.md", content, 0, fixture.claims[:2]),
		ambiguousLinkRequest(t, "consumer.md", content, 1, fixture.claims[2:]),
	}
	return fixture
}
