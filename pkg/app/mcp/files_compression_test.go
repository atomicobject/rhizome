package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func filesCompressionFixture(t testing.TB, count int) (Config, mcp.CallToolRequest) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "fixture")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src"), 0o755))
	registry := filepath.Join(t.TempDir(), "obsidian.json")
	data, err := json.Marshal(map[string]any{"vaults": map[string]any{"fixture": map[string]string{"path": root}}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(registry, data, 0o644))
	previous := obsidian.ObsidianConfigFile
	obsidian.ObsidianConfigFile = func() (string, error) { return registry, nil }
	t.Cleanup(func() { obsidian.ObsidianConfigFile = previous })
	store := newIntelStore(t)
	inputs := make([]any, 0, count)
	for i := range count {
		path := fmt.Sprintf("src/file%02d.go", i)
		inputs = append(inputs, path)
		content := "package fixture\n" + strings.Repeat("// representative source content\n", 128)
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(content), 0o644))
		require.NoError(t, store.ReplaceFileSummary(context.Background(), codeanchor.FileSummary{FilePath: path, Lang: codeanchor.LangGo, Hash: "fixture"}))
	}
	cfg := Config{
		Vault:      &obsidian.Vault{Name: "fixture"},
		VaultPath:  root,
		VaultDef:   obsidian.VaultDefinition{Name: "fixture", Path: root},
		IntelStore: store,
	}
	request := mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "files",
		Arguments: map[string]any{
			"inputs": inputs, "includeContent": "compress",
			"budgetChars": 500000, "limit": 100, "dedupe": false,
		},
	}}
	return cfg, request
}

func TestFilesCompressionWithoutProviderPreservesContents(t *testing.T) {
	cfg, request := filesCompressionFixture(t, 3)
	response, err := FilesTool(cfg)(context.Background(), request)
	require.NoError(t, err)
	require.False(t, response.IsError, "%+v", response)
	var parsed FilesResponse
	require.NoError(t, json.Unmarshal([]byte(response.Content[0].(mcp.TextContent).Text), &parsed))
	require.Len(t, parsed.Files, 3)
	request.GetArguments()["includeContent"] = true
	uncompressed, err := FilesTool(cfg)(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, uncompressed, response)
	require.False(t, parsed.Compressed)
	require.Empty(t, parsed.Text)
	for _, file := range parsed.Files {
		require.Contains(t, file.Content, "package fixture")
	}
}

type filesTestCompressor struct{}

func (filesTestCompressor) MaxInputChars() int { return 500000 }
func (filesTestCompressor) Compress(_ context.Context, request contextpack.CompressRequest) (contextpack.CompressResult, error) {
	return contextpack.CompressResult{Text: fmt.Sprintf("compressed %d files", len(request.Pieces)), Compressed: true}, nil
}

func TestFilesCompressionUsesInjectedProvider(t *testing.T) {
	cfg, request := filesCompressionFixture(t, 3)
	cfg.Compressor = filesTestCompressor{}
	response, err := FilesTool(cfg)(context.Background(), request)
	require.NoError(t, err)
	require.False(t, response.IsError, "%+v", response)
	var parsed FilesResponse
	require.NoError(t, json.Unmarshal([]byte(response.Content[0].(mcp.TextContent).Text), &parsed))
	require.True(t, parsed.Compressed)
	require.Equal(t, "compressed 3 files", parsed.Text)
	require.Len(t, parsed.Files, 3)
	for _, file := range parsed.Files {
		require.Empty(t, file.Content)
	}
}

func BenchmarkFilesCompressionWithoutProvider(b *testing.B) {
	cfg, request := filesCompressionFixture(b, 40)
	tool := FilesTool(cfg)
	ctx := context.Background()
	response, err := tool(ctx, request)
	require.NoError(b, err)
	require.False(b, response.IsError, "%+v", response)
	var parsed FilesResponse
	require.NoError(b, json.Unmarshal([]byte(response.Content[0].(mcp.TextContent).Text), &parsed))
	require.Len(b, parsed.Files, 40)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		response, err := tool(ctx, request)
		if err != nil || response.IsError {
			b.Fatal("file request failed", err)
		}
	}
}
