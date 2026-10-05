package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestCodeSymbolToolResolvesDefinitionSnippet(t *testing.T) {
	ctx := context.Background()
	vaultPath := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "src", "app.go"), []byte("package src\n\nfunc Run() {\n\tTarget()\n}\n\nfunc Target() {}\n"), 0o644))

	store := newIntelStore(t)
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/app.go", []codeanchor.IntelAnchor{
		{AnchorID: "target", Lang: codeanchor.LangGo, Kind: "function", Path: "src/app.go", Symbol: "Target", FQN: "src.Target", StartLine: 7, EndLine: 7, Fingerprint: "target"},
	}, nil, nil))

	res, err := CodeSymbolTool(Config{VaultPath: vaultPath, IntelStore: store})(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "code_symbol", Arguments: map[string]any{"symbol": "src.Target", "contextLines": float64(1)}},
	})
	require.NoError(t, err)
	var payload CodeSymbolResponse
	decodeToolResult(t, res, &payload)
	require.Equal(t, "resolved", payload.Status)
	require.NotNil(t, payload.Definition)
	require.Equal(t, "src/app.go", payload.Definition.Path)
	require.Contains(t, payload.Definition.Content, "func Target")
	require.Equal(t, 6, payload.Definition.ContentStart)
}

func TestCodeSymbolToolReturnsAmbiguousCandidates(t *testing.T) {
	ctx := context.Background()
	store := newIntelStore(t)
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/a.go", []codeanchor.IntelAnchor{
		{AnchorID: "a", Lang: codeanchor.LangGo, Kind: "function", Path: "src/a.go", Symbol: "Target", FQN: "pkg/a.Target", Fingerprint: "a"},
	}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/b.go", []codeanchor.IntelAnchor{
		{AnchorID: "b", Lang: codeanchor.LangGo, Kind: "function", Path: "src/b.go", Symbol: "Target", FQN: "pkg/b.Target", Fingerprint: "b"},
	}, nil, nil))

	res, err := CodeSymbolTool(Config{VaultPath: t.TempDir(), IntelStore: store})(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "code_symbol", Arguments: map[string]any{"symbol": "Target"}},
	})
	require.NoError(t, err)
	var payload CodeSymbolResponse
	decodeToolResult(t, res, &payload)
	require.Equal(t, "ambiguous", payload.Status)
	require.Len(t, payload.Candidates, 2)
}

func TestCodeReferencesToolReturnsCallersAndCallees(t *testing.T) {
	ctx := context.Background()
	store := newIntelStore(t)
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/callee.go", []codeanchor.IntelAnchor{
		{AnchorID: "callee", Lang: codeanchor.LangGo, Kind: "function", Path: "src/callee.go", Symbol: "Target", FQN: "pkg.Target", Fingerprint: "callee"},
		{AnchorID: "helper", Lang: codeanchor.LangGo, Kind: "function", Path: "src/callee.go", Symbol: "Helper", FQN: "pkg.Helper", Fingerprint: "helper"},
	}, []codeanchor.IntelEdge{{SrcID: "callee", DstID: "helper", Kind: "calls"}}, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/caller.go", []codeanchor.IntelAnchor{
		{AnchorID: "caller", Lang: codeanchor.LangGo, Kind: "function", Path: "src/caller.go", Symbol: "Run", FQN: "pkg.Run", Fingerprint: "caller"},
	}, []codeanchor.IntelEdge{{SrcID: "caller", DstID: "callee", Kind: "calls"}}, nil))

	res, err := CodeReferencesTool(Config{VaultPath: t.TempDir(), IntelStore: store})(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "code_references", Arguments: map[string]any{"symbol": "pkg.Target"}},
	})
	require.NoError(t, err)
	var payload CodeReferencesResponse
	decodeToolResult(t, res, &payload)
	require.Equal(t, "resolved", payload.Status)
	require.Len(t, payload.Callers, 1)
	require.Equal(t, "pkg.Run", payload.Callers[0].FQN)
	require.Len(t, payload.Callees, 1)
	require.Equal(t, "pkg.Helper", payload.Callees[0].FQN)
}

func TestFindTestSnippetsStopsAtLimitAndSkipsHeavyDirs(t *testing.T) {
	vaultPath := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "pkg"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "node_modules", "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "pkg", "one_test.go"), []byte("package pkg\n\nfunc TestOne() { Target() }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "pkg", "two_test.go"), []byte("package pkg\n\nfunc TestTwo() { Target() }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "node_modules", "pkg", "ignored_test.go"), []byte("package pkg\n\nfunc TestIgnored() { Target() }\n"), 0o644))

	snippets := findTestSnippets(Config{VaultPath: vaultPath}, "Target", 1, 2000)

	require.Len(t, snippets, 1)
	require.NotContains(t, snippets[0].Path, "node_modules")
}

func TestCodeSymbolToolMissingIndexDiagnostic(t *testing.T) {
	res, err := CodeSymbolTool(Config{VaultPath: t.TempDir()})(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "code_symbol", Arguments: map[string]any{"symbol": "Missing"}},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Equal(t, "code index unavailable; run `rzm index`", res.Content[0].(mcp.TextContent).Text)
}

func TestCodeSymbolToolManagedReadOnlyStoreDoesNotFallbackOpen(t *testing.T) {
	ctx := context.Background()
	vaultPath := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, ".rhizome"), 0o755))
	store, err := sqlitefixture.Open(filepath.Join(vaultPath, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/app.go", []codeanchor.IntelAnchor{
		{AnchorID: "target", Lang: codeanchor.LangGo, Kind: "function", Path: "src/app.go", Symbol: "Target", FQN: "src.Target", Fingerprint: "target"},
	}, nil, nil))
	require.NoError(t, store.Close())

	res, err := CodeSymbolTool(Config{
		VaultPath:        vaultPath,
		IntelStorePolicy: IntelStoreManagedReadOnly,
	})(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "code_symbol", Arguments: map[string]any{"symbol": "src.Target"}},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
	text := res.Content[0].(mcp.TextContent).Text
	var envelope map[string]string
	require.NoError(t, json.Unmarshal([]byte(text), &envelope))
	require.Equal(t, "indexed-context-missing", envelope["code"])
	require.Equal(t, "rzm index", envelope["remediation"])
}

func decodeToolResult(t *testing.T, res *mcp.CallToolResult, target any) {
	t.Helper()
	require.False(t, res.IsError)
	require.NotEmpty(t, res.Content)
	text, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		ptr, ok := res.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		text = *ptr
	}
	require.NoError(t, json.Unmarshal([]byte(text.Text), target))
}
