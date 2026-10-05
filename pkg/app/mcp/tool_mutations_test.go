package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestRenameHeadingToolCancelsWhileWaitingForWriter(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, ".rhizome", "index.lock")
	release, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	defer func() { _ = release() }()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan *mcp.CallToolResult, 1)
	go func() {
		result, _ := RenameHeadingTool(Config{Vault: &obsidian.Vault{Name: root}, ReadWrite: true})(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{Arguments: map[string]any{
				"path": "note.md", "oldHeading": "Old", "newHeading": "New", "apply": true,
			}},
		})
		done <- result
	}()
	require.Eventually(t, func() bool { return indexlock.CheckPriority(lockPath) }, time.Second, 10*time.Millisecond)
	cancel()
	select {
	case result := <-done:
		require.NotNil(t, result)
		require.True(t, result.IsError)
		require.Contains(t, result.Content[0].(mcp.TextContent).Text, "context canceled")
	case <-time.After(time.Second):
		t.Fatal("MCP cancellation did not release the writer wait")
	}
	require.False(t, indexlock.CheckPriority(lockPath))
}
