package query

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNestedFrontmatterTypedQueryFilterAndSort(t *testing.T) {
	env := newCustomQueryTestEnv(t, `type Assertion @node(paths: ["notes/*.md"]) {
 speaker: String @field(path: "provenance.speaker")
 literal: String @field(source: "provenance.speaker")
 source: Note @link(path: "provenance.source", includeBodyLinks: false, includeBacklinks: false)
 }`, map[string]string{
		"notes/a.md": "---\nprovenance.speaker: Literal\nprovenance: {speaker: Zoe, source: notes/b.md}\n---\n# A\n",
		"notes/b.md": "---\nprovenance: {speaker: Alice}\n---\n# B\n",
	})
	for _, selector := range []string{`filters: [{field: "speaker", op: eq, value: "Zoe"}]`, `filters: [{field: "speaker", op: contains, value: "Zo"}]`} {
		p, errs := Prepare(env.execSchema, `{ assertion(first: 10, `+selector+`) { speaker literal } }`)
		require.Empty(t, errs)
		result := Execute(context.Background(), env.deps(nil), env.schema, p)
		require.Empty(t, result.Errors)
		require.Equal(t, []any{map[string]any{"speaker": "Zoe", "literal": "Literal"}}, result.Data["assertion"], selector)
	}
	linkQuery, linkErrors := Prepare(env.execSchema, `{ assertion(path: "notes/a.md") { source { path } } }`)
	require.Empty(t, linkErrors)
	linkResult := Execute(context.Background(), env.deps(nil), env.schema, linkQuery)
	require.Empty(t, linkResult.Errors)
	require.Equal(t, []any{map[string]any{"source": map[string]any{"path": "notes/b.md"}}}, linkResult.Data["assertion"])
	p, errs := Prepare(env.execSchema, `{ assertion(first: 1, sort: [{field: "speaker", direction: asc}]) { speaker } }`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, p)
	require.Empty(t, result.Errors)
	require.Equal(t, []any{map[string]any{"speaker": "Alice"}}, result.Data["assertion"])
	deps := env.deps(nil)
	deps.ReadOverlay = &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{"notes/a.md": "---\nprovenance: {speaker: Aaron}\n---\n# A\n"}, Status: "dirty"}
	result = Execute(context.Background(), deps, env.schema, p)
	require.Empty(t, result.Errors)
	require.Equal(t, []any{map[string]any{"speaker": "Aaron"}}, result.Data["assertion"])
}
