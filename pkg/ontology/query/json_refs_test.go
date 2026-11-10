package query

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNodeRootsResolveSerializedRefsBeforeExplicitPaths(t *testing.T) {
	env := newCustomQueryTestEnv(t, `type Focus implements Section {} type Source @node(paths: ["notes/source.md"]) { focus: Focus @contains(level: H2, heading: "Focus") }`, map[string]string{
		"notes/source.md": "# Source\n\n## Focus\n\nFocused body.\n",
	})
	prepared, errs := Prepare(env.execSchema, `{ node(ref: "notes/source.md") { ... on Source { focus { ref { notePath kind fragment nodeId structural typeName } } } } }`)
	require.Empty(t, errs)
	initial := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, initial.Errors)
	focus := initial.Data["node"].(map[string]any)["focus"].(map[string]any)["ref"].(map[string]any)
	require.NotEmpty(t, focus["structural"], "structural fixture must carry a selector")
	focus["structuralFingerprint"] = focus["structural"]
	for _, tc := range []struct {
		name  string
		ref   map[string]any
		title string
	}{
		{"note", map[string]any{"notePath": "notes/source.md", "kind": "NOTE", "typeName": "Source"}, "Source"},
		{"section", focus, "Focus"},
		{"structural", map[string]any{"notePath": "notes/source.md", "kind": "SECTION", "structuralFingerprint": focus["structural"], "typeName": "Focus"}, "Focus"},
		{"stale structural", map[string]any{"notePath": "notes/source.md", "kind": "SECTION", "structuralFingerprint": "stale", "typeName": "Focus"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serialized, err := json.Marshal(tc.ref)
			require.NoError(t, err)
			prepared, errs := PrepareWithVariables(env.execSchema, `query($ref: String!) {
  single: node(ref: $ref) { title }
  batch: nodes(refs: [$ref]) { items { node { title } error { message } } }
}`, map[string]any{"ref": string(serialized)})
			require.Empty(t, errs)
			result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
			item := result.Data["batch"].(map[string]any)["items"].([]any)[0].(map[string]any)
			if tc.title == "" {
				require.NotEmpty(t, result.Errors, "stale selectors remain read errors")
				require.Nil(t, result.Data["single"])
				require.Nil(t, item["node"], "a stale selector must never widen to its whole note")
				require.NotNil(t, item["error"])
				return
			}
			require.Empty(t, result.Errors)
			require.NotNil(t, result.Data["single"], "serialized ref must not become a literal filename")
			require.Equal(t, tc.title, result.Data["single"].(map[string]any)["title"])
			require.Nil(t, item["error"])
			require.Equal(t, tc.title, item["node"].(map[string]any)["title"])
		})
	}
}
