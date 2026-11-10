package query

import (
	"context"
	"errors"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"

	"github.com/stretchr/testify/require"
)

func TestExecuteBatchFieldErrorsKeepRequestedRef(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
 type Children implements Section { titleText: String @field }
 type Item @node(paths: ["notes/*.md"]) {
   children: Children @contains(level: H2, heading: "Children", required: true)
 }`, map[string]string{
		"notes/broken.md":  "---\naliases: [Broken alias]\n---\n# Broken\n",
		"notes/healthy.md": "# Healthy\n\n## Children\n\nHealthy children.\n",
	})
	prepared, errs := Prepare(env.execSchema, `{
   before: item(path: "notes/broken.md") { children { title } }
   batch: nodes(refs: ["Broken alias", "notes/healthy.md", "notes/broken.md", "Broken alias", "missing"]) {
     rows: items { requestedRef value: node { ... on Item { child: children { title } } } error { code } }
   }
   after: item(path: "notes/broken.md") { children { title } }
 }`)
	require.Empty(t, errs)
	for range 2 {
		result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
		require.Len(t, result.Errors, 5)
		require.Nil(t, result.Errors[0].Extensions)
		require.Nil(t, result.Errors[4].Extensions)
		for i, ref := range []string{"Broken alias", "notes/broken.md", "Broken alias"} {
			fieldError := result.Errors[i+1]
			require.Equal(t, "required section Item.children is missing", fieldError.Message)
			require.Equal(t, []string{"batch", "rows", "value", "child"}, fieldError.Path)
			require.Equal(t, map[string]any{"requestedRef": ref}, fieldError.Extensions)
		}
		rows := result.Data["batch"].(map[string]any)["rows"].([]any)
		require.Len(t, rows, 5)
		healthy := rows[1].(map[string]any)
		require.Equal(t, "notes/healthy.md", healthy["requestedRef"])
		require.Equal(t, map[string]any{"child": map[string]any{"title": "Children"}}, healthy["value"])
		require.Nil(t, healthy["error"])
		require.Equal(t, map[string]any{"code": "unresolved_ref"}, rows[4].(map[string]any)["error"])
	}
}

func TestExecuteBatchChildReadFailureKeepsHealthySibling(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
 type Children implements Section { titleText: String @field }
 type Item @node(paths: ["notes/*.md"]) {
   children: Children @contains(level: H2, heading: "Children")
 }`, map[string]string{
		"notes/broken.md":  "# Broken\n\n## Children\n",
		"notes/healthy.md": "# Healthy\n\n## Children\n",
	})
	deps := env.deps(nil)
	deps.NoteReader = &sectionNeighborFailureReader{Note: &obsidian.Note{}, failures: map[string]error{
		"notes/broken.md": errors.New("child source unavailable"),
	}}
	prepared, errs := Prepare(env.execSchema, `{
   batch: nodes(refs: ["notes/broken.md", "notes/healthy.md"]) {
     items { requestedRef node { ... on Item { children { title } } } }
   }
 }`)
	require.Empty(t, errs)
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.NotEmpty(t, result.Errors)
	for _, fieldError := range result.Errors {
		require.Contains(t, fieldError.Message, "child source unavailable")
		require.Equal(t, []string{"batch", "items", "node", "children"}, fieldError.Path)
		require.Equal(t, map[string]any{"requestedRef": "notes/broken.md"}, fieldError.Extensions)
	}
	rows := result.Data["batch"].(map[string]any)["items"].([]any)
	require.Equal(t, map[string]any{"children": map[string]any{"title": "Children"}}, rows[1].(map[string]any)["node"])
}
