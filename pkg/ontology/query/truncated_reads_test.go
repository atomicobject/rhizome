package query

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// SPEC-0136 R8: capped reads report what they dropped beside typedRoots, and
// declared relation lists without first stay uncapped.
func TestCappedReadsReportTruncation(t *testing.T) {
	env := newQueryTestEnv(t)
	searcher := &fakeNoteSearcher{hits: []NoteSearchHit{
		{Path: "notes/decisions/structured-ontology-decision.md", Score: 0.9},
		{Path: "notes/decisions/ambient-linked-decision.md", Score: 0.8},
		{Path: "notes/decisions/backlink-decision.md", Score: 0.7},
	}}
	for _, tc := range []struct {
		name, query string
		want        []truncatedRead
	}{
		{"nodes", `{ batch: nodes(refs: ["notes/people/alice.md", "notes/teams/platform-team.md", "notes/projects/roadmap-refresh.md"], first: 2) { items { requestedRef } } }`,
			[]truncatedRead{{Path: []string{"batch"}, Returned: 2, Limit: 2}}},
		{"search", `{ search(query: ["decision"], first: 2) { nodes { path } } }`,
			[]truncatedRead{{Path: []string{"search"}, Returned: 2, Limit: 2}}},
		{"neighborhood", `{ project(first: 5) { neighborhood(direction: BOTH, first: 1) { edges { relation } } } person(first: 5) { neighborhood(first: 1) { edges { relation } } } }`,
			[]truncatedRead{{Path: []string{"project", "neighborhood"}, Returned: 1, Limit: 1}, {Path: []string{"person", "neighborhood"}, Returned: 1, Limit: 1}}},
		{"under the cap", `{ nodes(refs: ["notes/people/alice.md"]) { items { requestedRef } } search(query: ["decision"], first: 5) { nodes { path } } }`, nil},
		{"declared relations stay uncapped", `{ team(first: 5) { members { name projects { name } } } }`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepared, errs := Prepare(env.execSchema, tc.query)
			require.Empty(t, errs)
			result := Execute(t.Context(), env.depsWithSearch(searcher), env.schema, prepared)
			require.Empty(t, result.Errors)
			if tc.want == nil {
				require.NotContains(t, result.Extensions, "truncated")
				return
			}
			require.Equal(t, tc.want, result.Extensions["truncated"])
		})
	}
}
