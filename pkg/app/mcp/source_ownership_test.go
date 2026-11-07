package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestRenameHeadingToolRefusesOwnedInboundRewrite(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0755))
	files := map[string]string{"target.md": "# Old\n\nBody.\n", "owned.md": "See [[target#Old]].\n"}
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome/config.yml"), []byte("notes:\n  includes: ['**/*.md']\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome/source-ownership.yml"), []byte("sources:\n  - owner: external\n    reason: Use external authoring.\n    paths: [owned.md]\n"), 0644))
	result, err := RenameHeadingTool(Config{Vault: &repoconfig.Vault{Env: testkit.Env(t), Name: root}, NoteMetadata: testkit.NoteMetadataIndexer(t), ReadWrite: true})(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"path": "target.md", "oldHeading": "Old", "newHeading": "New", "apply": true}}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Contains(t, result.Content[0].(mcp.TextContent).Text, "Use external authoring.")
	for name, content := range files {
		data, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err)
		require.Equal(t, content, string(data))
	}
}
