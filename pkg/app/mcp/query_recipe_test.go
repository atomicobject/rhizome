package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestQueryRecipeToolSharesTargetDiagnosticsAndRefreshesRegistry(t *testing.T) {
	root := t.TempDir()
	ontologyDir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(ontologyDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(ontologyDir, "schema.graphql"), []byte(`type Project @node(paths:["projects/*.md"]) { name:String }`), 0o600))
	recipesDir := filepath.Join(root, ".rhizome", "query-recipes")
	require.NoError(t, os.MkdirAll(recipesDir, 0o700))
	recipe := []byte(`apiVersion: rhizome.query-recipe.v1
id: target
name: Target
problem: Inspect schema.
inputSpec: {mode: none}
query: {graphQL: 'query { __schema { queryType { name } } }'}
outputContract: {empty: No schema.}
adaptationGuidance: {summary: Inspect the schema.}
`)
	require.NoError(t, os.WriteFile(filepath.Join(recipesDir, "target.yaml"), recipe, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(recipesDir, "broken.yaml"), []byte("invalid: ["), 0o600))
	handler := QueryRecipeTool(Config{VaultPath: root})
	call := func() map[string]any {
		result, err := handler(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"op": "run", "id": "target"}}})
		require.NoError(t, err)
		require.False(t, result.IsError)
		var payload map[string]any
		require.NoError(t, json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &payload))
		return payload
	}
	first := call()
	require.Equal(t, "target", first["recipe"].(map[string]any)["id"])
	require.NotEmpty(t, first["result"].(map[string]any)["data"])
	duplicate := filepath.Join(recipesDir, "duplicate.yaml")
	require.NoError(t, os.WriteFile(duplicate, recipe, 0o600))
	blocked := call()
	require.Equal(t, false, blocked["ok"])
	issue := blocked["issues"].([]any)[0].(map[string]any)
	require.Equal(t, "duplicate_recipe_id", issue["code"])
	require.NoError(t, os.Rename(duplicate, filepath.Join(t.TempDir(), "duplicate.yaml")))
	require.Equal(t, "target", call()["recipe"].(map[string]any)["id"])
	// The standalone loader sees the same malformed unrelated source.
	_, issues := queryrecipe.LoadDefaultSources(root)
	require.Len(t, issues, 1)
}

func TestOntologyQueryUsesSharedRuntimeCapabilities(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`type Project @node(paths:["projects/*.md"]) {name:String}`), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "thing.go"), []byte("package pkg\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "thing_test.go"), []byte("package pkg\n"), 0o600))
	store, err := semdb.Open(filepath.Join(root, "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	metadata := configuredMCPNoteMetadata(t)
	vault := obsidian.VaultDefinition{Path: root}
	_, err = ontologytest.Publish(context.Background(), metadata, vault, &obsidian.Note{}, store)
	require.NoError(t, err)
	result, err := runOntologyQuery(context.Background(), Config{VaultPath: root, VaultDef: vault, IntelStore: store, NoteMetadata: metadata}, `{
 ontology {authoringGuide(type:"Project") {available}}
 code {testsForCode(path:"pkg/thing.go") {available tests {path}}}
 }`, nil)
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	require.Equal(t, true, result.Data["ontology"].(map[string]any)["authoringGuide"].(map[string]any)["available"])
	code := result.Data["code"].(map[string]any)["testsForCode"].(map[string]any)
	require.Equal(t, true, code["available"])
	require.Equal(t, []any{map[string]any{"path": "pkg/thing_test.go"}}, code["tests"])
}
