package query

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExecuteWorkspaceMarkdownAnchorsKeepCanonicalSourcePath(t *testing.T) {
	for _, tc := range []struct{ name, source, decoy string }{
		{"literal percent escape", "notes/Budget%20USD.md", "notes/Budget USD.md"},
		{"literal hash", "notes/Budget#USD.md", "notes/Budget.md"},
		{"space", "notes/Budget USD.md", "notes/Budget.md"},
		{"ordinary nested path", "notes/BudgetUSD.md", "notes/Budget.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newCustomQueryTestEnv(t, `
type Focus implements Section {}
type Source @node(paths: ["notes/*.md"]) {
  focus: Focus @contains(level: H2, heading: "Focus")
  block: Focus @contains(level: H2, heading: "Block")
}
`, map[string]string{
				tc.source: "[heading](#Focus) [block](#^source-block)\n\n## Focus\n\nContent\n\n## Block\n\nParagraph\n^source-block\n",
				tc.decoy:  "## Focus\n\nDecoy\n\n## Block\n\nParagraph\n^source-block\n",
			})
			prepared, errs := PrepareWithVariables(env.execSchema, `query($path: String!) {
  note(path: $path) { path workspace { sourceLinks(first: 10) {
    authoredTarget target resolved resolvedRef { notePath }
  } } }
}`, map[string]any{"path": tc.source})
			require.Empty(t, errs)
			result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
			require.Empty(t, result.Errors)
			note := result.Data["note"].(map[string]any)
			require.Equal(t, tc.source, note["path"])
			links := note["workspace"].(map[string]any)["sourceLinks"].([]any)
			require.Len(t, links, 2)
			for i, fragment := range []string{"Focus", "^source-block"} {
				link := links[i].(map[string]any)
				require.Equal(t, "#"+fragment, link["authoredTarget"])
				require.Equal(t, tc.source+"#"+fragment, link["target"])
				require.Equal(t, true, link["resolved"])
				ref := link["resolvedRef"].(map[string]any)
				require.Equal(t, tc.source, ref["notePath"], "a decoy with the same fragments must not replace the canonical source")
			}
		})
	}
}
