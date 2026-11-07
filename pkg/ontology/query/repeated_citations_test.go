package query

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRepeatedCitationBulletsRetainResolvedTargets(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Assertion @node(paths: ["notes/assertions/*.md"]) { sources: Sources @contains(level: H2, heading: "Sources") }
type Sources implements Section @node(locator: EMBEDDED) { citations: [Citation!] @contains(shape: LIST_ITEM) }
type Citation implements Section @node(locator: EMBEDDED) {
  sources: [Source!] @neighbors(direction: OUTBOUND, type: "Source", scope: SUBTREE)
}
type Source @node(paths: ["notes/sources/*.md"]) { name: String }
`, map[string]string{
		"notes/assertions/claim.md": "# Claim\n\n## Sources\n\n- [[one]]\n- [[two]]\n- [[one]]\n",
		"notes/sources/one.md":      "# One\n",
		"notes/sources/two.md":      "# Two\n",
		"notes/raw/one.md":          "# Raw One\n",
	})
	prepared, errs := Prepare(env.execSchema, `{ assertion(first: 1) { sources { citations { sourcesCount sources { path } } } } }`)
	require.Empty(t, errs)
	for _, staged := range []bool{false, true} {
		deps := env.deps(nil)
		if staged {
			deps.ReadOverlay = &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{"notes/assertions/claim.md": "# Claim\n\n## Sources\n\n- [[one]]\n- [[two]]\n- [[one]]\n"}, Status: "dirty"}
		}
		result := Execute(context.Background(), deps, env.schema, prepared)
		require.Empty(t, result.Errors)
		rows := result.Data["assertion"].([]any)[0].(map[string]any)["sources"].(map[string]any)["citations"].([]any)
		require.Len(t, rows, 3)
		for i, row := range rows {
			target := "notes/sources/one.md"
			if i == 1 {
				target = "notes/sources/two.md"
			}
			require.Equal(t, map[string]any{"sourcesCount": 1, "sources": []any{map[string]any{"path": target}}}, row)
		}
	}
}
