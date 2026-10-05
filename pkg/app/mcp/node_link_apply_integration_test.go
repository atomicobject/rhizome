package mcp_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/indexing"
	mcpapi "github.com/atomicobject/rhizome/pkg/app/mcp"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestWritableNodeLinkPublishesThroughSuppliedIndexingCapability(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "specs"), 0o755))
	schemaText := `
type ProductSpec @node(paths: ["specs/product.md"]) { stories: StoriesSection @contains(level: H2, heading: "Stories") }
type StoriesSection implements Section { stories: [UserStory!] @contains(level: H3) }
type UserStory implements Section @node(locator: EMBEDDED) { related: ProductSpec @link }
`
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(schemaText), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "specs", "product.md"), []byte("# Product\n\n## Stories\n\n### Story A\nrelated:: [[specs/product]]\n"), 0o644))
	formats, err := builtin.NewRuntime()
	require.NoError(t, err)
	metadata, err := notemeta.NewIndexer(formats)
	require.NoError(t, err)
	config := mcpapi.Config{VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, ReadWrite: true, NoteMetadata: metadata}
	calls := 0
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	config.ApplyLinkTargets = func(ctx context.Context, schemaHash string, request ontology.LinkTargetRequest) (ontology.LinkTargetResult, error) {
		calls++
		require.Equal(t, schema.Hash, schemaHash, "apply must run against the loaded schema")
		return indexing.ApplyNodeLinkTargets(ctx, indexing.NodeLinkApplyRequest{VaultDef: config.VaultDef, NoteMetadata: metadata, NoteReader: &obsidian.Note{}, SchemaHash: schemaHash, LinkTarget: request})
	}
	response, err := mcpapi.NodeLinkTool(config)(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "node_link", Arguments: map[string]any{"targets": []any{"specs/product.md#Story A"}, "ensure": "apply"}}})
	require.NoError(t, err)
	require.False(t, response.IsError, "%+v", response.Content)
	require.Equal(t, 1, calls)
	var payload struct {
		Ensure string `json:"ensure"`
	}
	require.NoError(t, json.Unmarshal([]byte(response.Content[0].(mcp.TextContent).Text), &payload))
	require.Equal(t, "apply", payload.Ensure)
	store, cleanup, err := ontology.OpenStoreForWrite(root)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	edges, err := store.OntologyEdgesForPath(ctx, "specs/product.md", false, "related", 0)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	require.Contains(t, edges[0].SrcNodeID, "specs/product.md#^")
	blockID := edges[0].SrcNodeID[strings.Index(edges[0].SrcNodeID, "#^")+1:]
	content, err := os.ReadFile(filepath.Join(root, "specs", "product.md"))
	require.NoError(t, err)
	require.Contains(t, string(content), blockID, "the persisted block marker backs the published edge")
	rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, []string{"specs/product.md"})
	require.NoError(t, err)
	require.Contains(t, rows, "specs/product.md")
}
