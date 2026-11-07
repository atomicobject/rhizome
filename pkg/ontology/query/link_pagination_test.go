package query

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuthoredLinkListsAcceptFirst(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Project @node(paths: ["notes/projects/*.md"]) {
  decisions: [Decision!] @link
  connections: Connections @contains(level: H2, heading: "Connections")
}
type Connections implements Section @node(locator: EMBEDDED) {
  decisions: [Decision!] @link(includeBodyLinks: false, includeBacklinks: false)
}
type Decision @node(paths: ["notes/decisions/*.md"]) { name: String }
`, map[string]string{
		"notes/projects/one.md": "---\ndecisions: [notes/decisions/a.md, notes/decisions/b.md]\n---\n# Project\n\n## Connections\n\ndecisions:: [[a]]\ndecisions:: [[b]]\n",
		"notes/decisions/a.md":  "# A\n",
		"notes/decisions/b.md":  "# B\n",
	})
	prepared, errs := Prepare(env.execSchema, `{ project(first: 1) { limited: decisions(first: 1) { path } all: decisions { path } connections { limited: decisions(first: 1) { path } all: decisions { path } } } }`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	row := result.Data["project"].([]any)[0].(map[string]any)
	for _, owner := range []map[string]any{row, row["connections"].(map[string]any)} {
		require.Len(t, owner["limited"], 1)
		require.Len(t, owner["all"], 2)
		require.Equal(t, owner["all"].([]any)[:1], owner["limited"])
	}
}
