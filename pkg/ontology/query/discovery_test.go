package query

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiscoverSchemaSelectedLiveContract(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
 enum ProjectStatus { active frozen }
 type Project @node(paths: ["projects/*.md"]) { status: ProjectStatus! owner: Person @link }
 type Person @node(paths: ["people/*.md"]) { name: String! }
 `, nil)
	full, err := DiscoverSchema(env.schema, env.execSchema, "")
	require.NoError(t, err)
	payload, err := json.Marshal(full)
	require.NoError(t, err)
	expected, _ := json.Marshal(map[string]string{"schema": env.execSchema.SDL})
	require.JSONEq(t, string(expected), string(payload))
	selected, err := DiscoverSchema(env.schema, env.execSchema, "Project")
	require.NoError(t, err)
	require.Equal(t, "type-fragment", selected.Scope)
	require.Equal(t, env.schema.Hash, selected.SchemaHash)
	for _, fragment := range []string{"type Project", "status: ProjectStatus!", "owner: Person", "enum ProjectStatus", "active", "frozen", "input FieldFilterInput", "first: Int = 20", "project(path: String"} {
		require.Contains(t, selected.Schema, fragment)
	}
	require.NotContains(t, selected.Schema, "type Person")
	require.Contains(t, selected.ReferencedTypes, "Person")
	require.Len(t, selected.Roots, 1)
	require.Equal(t, "project", selected.Roots[0].Name)
	require.Equal(t, publicRootFirstMax, selected.Roots[0].MaxFirst)
	require.Equal(t, []string{"path", "find", "property", "semantic"}, selected.Roots[0].ExclusiveSelectors)
	require.Less(t, len(selected.Schema), len(full.Schema))
	again, err := DiscoverSchema(env.schema, env.execSchema, "Project")
	require.NoError(t, err)
	require.Equal(t, selected, again)
	person, err := DiscoverSchema(env.schema, env.execSchema, "Person")
	require.NoError(t, err)
	require.Contains(t, person.Schema, "name: String!")
	require.False(t, strings.Contains(person.Schema, "type Project"))
	for _, name := range []string{"Missing", "__Schema", "_FallbackNote", "_FallbackSection"} {
		_, err := DiscoverSchema(env.schema, env.execSchema, name)
		require.Error(t, err, name)
	}
}
