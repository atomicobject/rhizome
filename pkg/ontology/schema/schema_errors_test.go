package schema

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

func requireCompileError(t *testing.T, err error, code string) *CompileError {
	t.Helper()
	var rule *CompileError
	require.ErrorAs(t, err, &rule)
	require.Equal(t, code, rule.Code)
	return rule
}

func requireGraphQLCompileError(t *testing.T, err error) {
	t.Helper()
	var syntax *gqlerror.Error
	require.ErrorAs(t, err, &syntax)
	require.NotEmpty(t, syntax.Locations)
}

func TestCompileErrorRetainsAuthoredFieldAndDiagnostic(t *testing.T) {
	root := t.TempDir()
	writeOntologySchema(t, root, "type Record @node(paths: [\"notes/*.md\"]) {\n  key: [String] @identifier\n}\n")
	_, err := LoadSchema(root)
	rule := requireCompileError(t, err, "identifier_list")
	require.Equal(t, "Record", rule.TypeName)
	require.Equal(t, "key", rule.FieldName)
	require.Equal(t, "schema.graphql", rule.Position.Src.Name)
	require.Equal(t, 2, rule.Position.Line)
	require.Equal(t, 3, rule.Position.Column)
	require.EqualError(t, err, "schema.graphql:2:3: @identifier on Record.key cannot be a list field")
}
