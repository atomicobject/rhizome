package query

import (
	"encoding/json"
	"fmt"
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
	full, err := DiscoverSchema(env.schema, env.execSchema, SchemaDiscoveryOptions{Type: ""})
	require.NoError(t, err)
	payload, err := json.Marshal(full)
	require.NoError(t, err)
	expected, _ := json.Marshal(map[string]string{"schema": env.execSchema.SDL})
	require.JSONEq(t, string(expected), string(payload))
	selected, err := DiscoverSchema(env.schema, env.execSchema, SchemaDiscoveryOptions{Type: "Project"})
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
	again, err := DiscoverSchema(env.schema, env.execSchema, SchemaDiscoveryOptions{Type: "Project"})
	require.NoError(t, err)
	require.Equal(t, selected, again)
	person, err := DiscoverSchema(env.schema, env.execSchema, SchemaDiscoveryOptions{Type: "Person"})
	require.NoError(t, err)
	require.Contains(t, person.Schema, "name: String!")
	require.False(t, strings.Contains(person.Schema, "type Project"))
	for _, name := range []string{"Missing", "__Schema", "_FallbackNote", "_FallbackSection"} {
		_, err := DiscoverSchema(env.schema, env.execSchema, SchemaDiscoveryOptions{Type: name})
		require.Error(t, err, name)
	}
}

func TestDiscoverSchemaRelatedTypesAreBoundedAndDeduplicated(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
 enum State { active frozen }
 type Project @node(paths: ["projects/*.md"]) { status: State! owner: Person @link }
 type Person @node(paths: ["people/*.md"]) { name: String! project: Project @link organization: Organization @link }
 type Organization @node(paths: ["organizations/*.md"]) { name: String! }
 `, nil)
	selected, err := DiscoverSchema(env.schema, env.execSchema, SchemaDiscoveryOptions{Type: "Project", Depth: 1})
	require.NoError(t, err)
	require.Contains(t, selected.IncludedTypes, "Person")
	require.Contains(t, selected.Schema, "type Person ")
	require.Contains(t, selected.ReferencedTypes, "Organization")
	require.NotContains(t, selected.Schema, "type Organization ")
	require.Equal(t, 1, strings.Count(selected.Schema, "type Project "))
	require.Equal(t, 1, strings.Count(selected.Schema, "enum State "))
	require.False(t, selected.Truncated)

	deeper, err := DiscoverSchema(env.schema, env.execSchema, SchemaDiscoveryOptions{Types: []string{"Person", "Project", "Person"}, Depth: 2})
	require.NoError(t, err)
	require.Contains(t, deeper.Schema, "type Organization ")
	require.Equal(t, 1, strings.Count(deeper.Schema, "type Person "))
	require.Equal(t, []string{"Person", "Project"}, deeper.Types)
	again, err := DiscoverSchema(env.schema, env.execSchema, SchemaDiscoveryOptions{Types: []string{"Project", "Person"}, Depth: 2})
	require.NoError(t, err)
	require.Equal(t, deeper, again)
}

func TestDiscoverSchemaBudgetPreservesWholeDefinitions(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
 type Project @node(paths: ["projects/*.md"]) { owner: Person @link }
 type Person @node(paths: ["people/*.md"]) { name: String! }
 `, nil)
	base, err := DiscoverSchema(env.schema, env.execSchema, SchemaDiscoveryOptions{Type: "Project"})
	require.NoError(t, err)
	limited, err := DiscoverSchema(env.schema, env.execSchema, SchemaDiscoveryOptions{Type: "Project", Depth: 1, MaxBytes: len(base.Schema)})
	require.NoError(t, err)
	require.True(t, limited.Truncated)
	require.Contains(t, limited.LimitReasons, "maxBytes")
	require.Contains(t, limited.ReferencedTypes, "Person")
	require.Equal(t, base.Schema, limited.Schema)
	require.LessOrEqual(t, len(limited.Schema), limited.Limits.MaxBytes)
	_, err = DiscoverSchema(env.schema, env.execSchema, SchemaDiscoveryOptions{Type: "Project", MaxBytes: 1})
	require.ErrorContains(t, err, "selected types require")
}

func TestDiscoverSchemaRejectsInvalidExpansion(t *testing.T) {
	env := newCustomQueryTestEnv(t, `type Project @node(paths: ["projects/*.md"]) { name: String! }`, nil)
	for _, options := range []SchemaDiscoveryOptions{
		{Type: "Project", Types: []string{"Project"}},
		{Type: "Project", Depth: -1}, {Type: "Project", Depth: 3},
		{Type: "Project", MaxBytes: -1}, {Type: "Project", MaxBytes: SchemaDiscoveryMaxBytes + 1},
		{Depth: 1}, {MaxBytes: 1}, {Types: []string{""}},
	} {
		_, err := DiscoverSchema(env.schema, env.execSchema, options)
		require.Error(t, err, "%+v", options)
	}
}

func TestDiscoverSchemaTypeLimitAndAbstractMembers(t *testing.T) {
	var sdl strings.Builder
	sdl.WriteString("interface Item { name: String! }\n")
	sdl.WriteString("type Root @node(paths: [\"roots/*.md\"]) { item: Item @link ")
	for i := 0; i < 35; i++ {
		fmt.Fprintf(&sdl, "child%d: Child%d @link ", i, i)
	}
	sdl.WriteString("}\n")
	for i := 0; i < 35; i++ {
		fmt.Fprintf(&sdl, "type Child%d implements Item @node(paths: [\"child%d/*.md\"]) { name: String! }\n", i, i)
	}
	env := newCustomQueryTestEnv(t, sdl.String(), nil)
	limited, err := DiscoverSchema(env.schema, env.execSchema, SchemaDiscoveryOptions{Type: "Root", Depth: 1, MaxBytes: SchemaDiscoveryMaxBytes})
	require.NoError(t, err)
	require.True(t, limited.Truncated)
	require.Contains(t, limited.LimitReasons, "maxTypes")
	require.Len(t, limited.IncludedTypes, SchemaDiscoveryMaxTypes)
	require.NotEmpty(t, limited.ReferencedTypes)
	for _, name := range limited.IncludedTypes {
		require.NotContains(t, limited.ReferencedTypes, name)
	}
	abstract, err := DiscoverSchema(env.schema, env.execSchema, SchemaDiscoveryOptions{Type: "Item"})
	require.NoError(t, err)
	require.Contains(t, abstract.ReferencedTypes, "Child0")
	require.Contains(t, abstract.ReferencedTypes, "Child34")
}

func TestDecodeSchemaDiscoveryOptionsRejectsWrongShapes(t *testing.T) {
	for _, input := range []map[string]any{
		{"type": 3}, {"types": "Project"}, {"types": []any{3}},
		{"depth": 1.5}, {"depth": "1"}, {"maxBytes": true},
	} {
		_, err := DecodeSchemaDiscoveryOptions(input)
		require.Error(t, err, "%v", input)
	}
	opts, err := DecodeSchemaDiscoveryOptions(map[string]any{"types": []any{"Project", "Person"}, "depth": float64(1), "maxBytes": float64(8000), "sessionId": "session"})
	require.NoError(t, err)
	require.Equal(t, SchemaDiscoveryOptions{Types: []string{"Project", "Person"}, Depth: 1, MaxBytes: 8000}, opts)
}
