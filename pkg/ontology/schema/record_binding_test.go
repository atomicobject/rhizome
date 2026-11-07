package schema

import (
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const recordTestSource = `@source(shape: RECORD, paths: ["model/*.native"], select: "rows", key: {field: "id"}, title: ["name", "$key"], space: "test", logicalKind: "row")`

func recordSchema(t *testing.T, sdl string) (*Schema, error) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome/ontology")
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(sdl), 0644))
	return LoadSchema(root)
}
func TestRecordBindingsCompileWithoutMarkdownInheritance(t *testing.T) {
	schema, err := recordSchema(t, `type Row @node(locator: EMBEDDED) `+recordTestSource+` {
  id: ID @field(path: "id")
  group: String @field(path: "group", fallback: MAPPING_KEY)
  refs: [Row!] @field(path: "refs") @references(to: ["Row"], mode: STRICT, values: BY_SHAPE, edge: {name: "uses"})
 }`)
	require.NoError(t, err)
	row := schema.Types["Row"]
	require.Equal(t, TypeRoleEmbeddedNode, row.Role)
	require.Equal(t, EmbeddedSourceShapeRecord, row.SourceShape)
	require.Equal(t, "rows", row.Record.Select.Text)
	require.Equal(t, "id", row.Record.Key.Field)
	require.True(t, row.ByName["group"].Record.MappingKeyFallback)
	require.Equal(t, "BY_SHAPE", row.ByName["refs"].Record.References.Values)
	require.Equal(t, "uses", row.ByName["refs"].Record.References.Edge.Name)
	require.Nil(t, row.ByName["level"])
	positional, err := recordSchema(t, `type Row @node(locator: EMBEDDED) `+strings.Replace(recordTestSource, `key: {field: "id"}`, `key: null`, 1)+` { id: ID @field(path: "id") }`)
	require.NoError(t, err)
	require.True(t, positional.Types["Row"].Record.Key.Positional)
}
func TestRecordBindingCompileErrorMatrix(t *testing.T) {
	source := recordTestSource
	tests := []struct{ name, source, field string }{
		{"omitted key", strings.Replace(source, `key: {field: "id"}, `, "", 1), `id: ID @field(path: "id")`},
		{"empty key", strings.Replace(source, `key: {field: "id"}`, `key: {}`, 1), `id: ID @field(path: "id")`},
		{"mixed key", strings.Replace(source, `key: {field: "id"}`, `key: {field: "id", mappingKey: true}`, 1), `id: ID @field(path: "id")`},
		{"false mapping key", strings.Replace(source, `key: {field: "id"}`, `key: {mappingKey: false}`, 1), `id: ID @field(path: "id")`},
		{"empty key field", strings.Replace(source, `key: {field: "id"}`, `key: {field: ""}`, 1), `id: ID @field(path: "id")`},
		{"template expression", strings.Replace(source, `key: {field: "id"}`, `key: {template: "{obj.id}"}`, 1), `id: ID @field(path: "id")`},
		{"template format", strings.Replace(source, `key: {field: "id"}`, `key: {template: "{id:02}"}`, 1), `id: ID @field(path: "id")`},
		{"missing title", strings.Replace(source, `title: ["name", "$key"], `, "", 1), `id: ID @field(path: "id")`},
		{"empty title", strings.Replace(source, `title: ["name", "$key"]`, `title: []`, 1), `id: ID @field(path: "id")`},
		{"empty paths", strings.Replace(source, `paths: ["model/*.native"]`, `paths: []`, 1), `id: ID @field(path: "id")`},
		{"marker", strings.Replace(source, `shape: RECORD`, `shape: RECORD, marker: "#row"`, 1), `id: ID @field(path: "id")`},
		{"predicate selector", strings.Replace(source, `select: "rows"`, `select: "rows[?x]"`, 1), `id: ID @field(path: "id")`},
		{"empty namespace", strings.Replace(source, `space: "test"`, `space: ""`, 1), `id: ID @field(path: "id")`},
		{"implicit field", source, `id: ID`},
		{"literal source", source, `id: ID @field(source: "id")`},
		{"dual source", source, `id: ID @field(path: "id", source: "id")`},
		{"inline source", source, `id: ID @field(path: "id", sourceKind: INLINE)`},
		{"missing ref binding", source, `refs: [Row!] @field(path: "refs")`},
		{"unknown ref target", source, `refs: [String!] @field(path: "refs") @references(to: ["Missing"], mode: STRICT)`},
		{"empty ref targets", source, `refs: [String!] @field(path: "refs") @references(to: [], mode: STRICT)`},
		{"missing typed targets", source, `refs: [String!] @field(path: "refs") @references(to: ["Row"], mode: STRICT, encoding: TYPED)`},
		{"targets on ID", source, `refs: [String!] @field(path: "refs") @references(to: ["Row"], mode: STRICT, targets: [{tag: "row", type: "Row"}])`},
		{"empty edge", source, `refs: [String!] @field(path: "refs") @references(to: ["Row"], mode: STRICT, edge: {name: ""})`},
		{"contains heading", source, `refs: [Row!] @contains(shape: RECORD, select: "rows", heading: "Rows")`},
		{"container self cycle", strings.Replace(source, `shape: RECORD`, `shape: RECORD, container: "Row"`, 1), `id: ID @field(path: "id")`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := recordSchema(t, `type Row @node(locator: EMBEDDED) `+tt.source+` { `+tt.field+` }`)
			require.Error(t, err)
		})
	}
	_, err := recordSchema(t, `type Row implements Section @node(locator: EMBEDDED) `+source+` { value: String @field(path: "id") }`)
	require.ErrorContains(t, err, "RECORD cannot implement Section")
}
func TestRecordSelectorAndTemplateGrammar(t *testing.T) {
	for _, text := range []string{"", "rows", "rows[]", "rows{}", "groups{}[]", "a{}[].b", "resources[].endpoints"} {
		_, err := CompileRecordSelector(text)
		require.NoError(t, err, text)
	}
	for _, text := range []string{"a..b", "a[0]", "a.*", "a..", "rows[]{}", "$.rows"} {
		_, err := CompileRecordSelector(text)
		require.Error(t, err, text)
	}
	parts, err := compileRecordTemplate(`{{{from}}}>{to}`)
	require.NoError(t, err)
	require.Equal(t, []RecordTemplatePart{{Literal: "{"}, {Field: "from"}, {Literal: "}>"}, {Field: "to"}}, parts)
}
