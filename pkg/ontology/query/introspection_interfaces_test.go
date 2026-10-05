package query

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExecuteIntrospection_InterfaceInheritance(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
interface Note {
  summary: String @field
}
type Project @node(paths: ["projects/*.md"]) {
  title: String!
}
`, nil)
	prepared, errs := Prepare(env.execSchema, `{
  inherited: __type(name: "NoteNode") { interfaces { name } }
  base: __type(name: "Node") { interfaces { name } }
  object: __type(name: "Project") { interfaces { name } }
  scalar: __type(name: "String") { interfaces { name } }
 }`)
	require.Empty(t, errs)
	result := ExecuteIntrospection(env.execSchema, prepared)
	require.Empty(t, result.Errors)
	require.Equal(t, map[string]any{
		"inherited": map[string]any{"interfaces": []any{map[string]any{"name": "Node"}, map[string]any{"name": "Note"}}},
		"base":      map[string]any{"interfaces": []any{}},
		"object":    map[string]any{"interfaces": []any{map[string]any{"name": "NoteNode"}, map[string]any{"name": "Node"}, map[string]any{"name": "Note"}}},
		"scalar":    map[string]any{"interfaces": nil},
	}, result.Data)
}
