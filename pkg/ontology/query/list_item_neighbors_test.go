package query

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListItemNeighborsIncludeFirstLineLinks(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Evidence @node(paths:["evidence/*.md"]) { name: String @field }
type Registry @node(paths:["registry.md"]) { entries: [Entry!] @contains(shape: LIST_ITEM, marker:"#citation") }
type Entry implements Section @node(locator:EMBEDDED) {
 references: [Evidence!] @neighbors(direction:OUTBOUND,type:"Evidence",scope:SUBTREE)
}`, map[string]string{"evidence/visit.md": "---\nname: Visit\n---\n# Visit\n", "registry.md": "# Registry\n\n- [[visit]] #citation\n"})
	prepared, errs := Prepare(env.execSchema, `{ registry(path:"registry.md") { entries { references { name } neighborhood(first:10) { edges { relation target { notePath } } } } } }`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	entry := result.Data["registry"].([]any)[0].(map[string]any)["entries"].([]any)[0].(map[string]any)
	require.Equal(t, []any{map[string]any{"name": "Visit"}}, entry["references"])
	edges := entry["neighborhood"].(map[string]any)["edges"].([]any)
	found := false
	for _, raw := range edges {
		edge := raw.(map[string]any)
		if edge["relation"] == "references" && edge["target"].(map[string]any)["notePath"] == "evidence/visit.md" {
			found = true
		}
	}
	require.True(t, found, "the native neighborhood must contain the same edge as the typed relation")
}
