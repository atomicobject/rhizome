package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestOntologyQuerySchemaHandlerSharesSelectedContract(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`type Project @node(paths: ["projects/*.md"]) { status: String! }`), 0644))
	handler := OntologyQuerySchemaTool(Config{VaultPath: root})
	call := func(value any) *mcp.CallToolResult {
		t.Helper()
		result, err := handler(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"type": value}}})
		require.NoError(t, err)
		return result
	}
	type payload struct {
		Schema          string   `json:"schema"`
		Type            string   `json:"type"`
		Scope           string   `json:"scope"`
		ReferencedTypes []string `json:"referencedTypes"`
		Roots           []struct {
			Name string `json:"name"`
		} `json:"roots"`
	}
	decode := func(result *mcp.CallToolResult) payload {
		t.Helper()
		require.False(t, result.IsError)
		var out payload
		require.NoError(t, json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &out))
		return out
	}

	full := decode(call(""))
	require.Empty(t, full.Type)
	require.Contains(t, full.Schema, "type Project implements NoteNode & Node & Note {")
	require.Contains(t, full.Schema, "type CodeSymbol implements Node {")
	require.Contains(t, full.Schema, "type Query {")

	selected := decode(call("Project"))
	require.Equal(t, "Project", selected.Type)
	require.Equal(t, "type-fragment", selected.Scope)
	require.Len(t, selected.Roots, 1)
	require.Equal(t, "project", selected.Roots[0].Name)
	require.Contains(t, selected.ReferencedTypes, "NodeRef")
	require.Contains(t, selected.Schema, "type Project implements NoteNode & Node & Note {")
	require.Contains(t, selected.Schema, "status: String!")
	require.NotContains(t, selected.Schema, "type CodeSymbol")
	require.NotContains(t, selected.Schema, "type NodeRefResolution")

	for value, message := range map[any]string{
		"Missing": `public query type "Missing" not found`,
		3:         "type must be a string",
	} {
		result := call(value)
		require.True(t, result.IsError)
		require.Contains(t, result.Content[0].(mcp.TextContent).Text, message)
	}
}
