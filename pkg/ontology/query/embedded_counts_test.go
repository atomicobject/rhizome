package query

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEmbeddedRelationCountsMatchLists(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Assertion @node(paths: ["notes/assertions/*.md"]) {
  sourcesSection: AssertionSources @contains(level: H2, heading: "Sources")
}
type AssertionSources implements Section @node(locator: EMBEDDED) {
  citations: [SourceCitation!] @contains(shape: LIST_ITEM)
  sources: [Source!] @neighbors(direction: OUTBOUND, type: "Source", scope: SUBTREE)
}
type SourceCitation implements Section @node(locator: EMBEDDED) {
  sources: [Source!] @neighbors(direction: OUTBOUND, type: "Source", scope: SUBTREE)
}
type Source @node(paths: ["notes/sources/*.md"]) { name: String }
`, map[string]string{
		"notes/assertions/claim.md": "# Claim\n\n## Sources\n\n- [[one]]\n- [[two]]\n",
		"notes/sources/one.md":      "# One\n",
		"notes/sources/two.md":      "# Two\n",
	})
	for _, query := range []string{
		`{ assertion(first: 1) { sourcesSection { sourcesCount sources { path } citations { sourcesCount sources(first: 1) { path } } } } }`,
		`{ assertionSources(first: 1) { sourcesCount sources { path } citations { sourcesCount sources(first: 1) { path } } } }`,
	} {
		prepared, errs := Prepare(env.execSchema, query)
		require.Empty(t, errs)
		result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
		require.Empty(t, result.Errors)
		var section map[string]any
		if rows, ok := result.Data["assertion"]; ok {
			section = rows.([]any)[0].(map[string]any)["sourcesSection"].(map[string]any)
		} else {
			section = result.Data["assertionSources"].([]any)[0].(map[string]any)
		}
		require.Equal(t, 2, section["sourcesCount"], query)
		require.Len(t, section["sources"], 2)
		for _, row := range section["citations"].([]any) {
			citation := row.(map[string]any)
			require.Equal(t, 1, citation["sourcesCount"], query)
			require.Len(t, citation["sources"], 1)
		}
	}
}
