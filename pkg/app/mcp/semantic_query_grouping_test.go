package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPackGroupsPreservesRankedOrder(t *testing.T) {
	docs := docMatcher{patterns: []string{"**/CONTEXT.md"}}
	intent := semanticQueryIntent{Overview: true, PreferDocs: true}

	grouped := packGroups([]semanticQueryGroup{
		{match: SemanticMatchPayload{Type: "code", Path: "pkg/search/semantic/search.go"}, score: 0.90},
		{match: SemanticMatchPayload{Type: "code", Path: "pkg/search/service.go"}, score: 0.88},
		{match: SemanticMatchPayload{Type: "note", Path: "pkg/app/mcp/CONTEXT.md"}, score: 0.80},
		{match: SemanticMatchPayload{Type: "note", Path: "docs/reference/guides/Embeddings.md"}, score: 0.82},
		{match: SemanticMatchPayload{Type: "code", Path: "pkg/search/planner.go"}, score: 0.70},
	}, intent, docs)

	type shown struct {
		label   string
		path    string
		indices []int
	}
	got := make([]shown, 0, len(grouped))
	for _, group := range grouped {
		got = append(got, shown{label: group.label, path: group.path, indices: group.indices})
	}
	// Adjacent sources share a displayed group; a later source with the same
	// label starts a new group rather than moving ahead of intervening results.
	require.Equal(t, []shown{
		{label: "Code: pkg/search", path: "pkg/search/semantic/search.go", indices: []int{0, 1}},
		{label: "Notes: pkg/app", path: "pkg/app/mcp/CONTEXT.md", indices: []int{2}},
		{label: "Notes: docs/reference", path: "docs/reference/guides/Embeddings.md", indices: []int{3}},
		{label: "Code: pkg/search", path: "pkg/search/planner.go", indices: []int{4}},
	}, got)
}
