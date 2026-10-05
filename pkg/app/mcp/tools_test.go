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
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/app/runtimeview"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCapabilitiesToolRequiresAuthoritativeInventory(t *testing.T) {
	resp, err := CapabilitiesTool(Config{VaultPath: t.TempDir()})(context.Background(), mcp.CallToolRequest{})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Equal(t, "tool inventory unavailable", resp.Content[0].(mcp.TextContent).Text)
}

func TestManagedIndexedOperationsReturnStableUnavailableEnvelope(t *testing.T) {
	missing := &actions.IndexedContextFreshness{
		State:       actions.IndexedContextMissing,
		WarningCode: "indexed-context-missing",
		Remediation: "rzm index",
	}
	cfg := Config{IntelStorePolicy: IntelStoreManagedReadOnly, IndexedContextUnavailable: missing}

	tests := []struct {
		name string
		call func() (*mcp.CallToolResult, error)
	}{
		{
			name: "report",
			call: func() (*mcp.CallToolResult, error) {
				return ReportTool(cfg)(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"op": "doc-coverage"}}})
			},
		},
		{
			name: "find connections",
			call: func() (*mcp.CallToolResult, error) {
				return FindConnectionsTool(cfg)(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"note": "Alpha.md"}}})
			},
		},
		{
			name: "code symbol",
			call: func() (*mcp.CallToolResult, error) {
				return CodeSymbolTool(cfg)(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"symbol": "Target"}}})
			},
		},
		{
			name: "code references",
			call: func() (*mcp.CallToolResult, error) {
				return CodeReferencesTool(cfg)(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"symbol": "Target"}}})
			},
		},
		{
			name: "graph path",
			call: func() (*mcp.CallToolResult, error) {
				return GraphPathTool(cfg)(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"from": "a", "to": "b"}}})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := tt.call()
			require.NoError(t, err)
			require.True(t, resp.IsError)
			text := resp.Content[0].(mcp.TextContent).Text
			var envelope map[string]string
			require.NoError(t, json.Unmarshal([]byte(text), &envelope))
			require.Equal(t, "indexed-context-missing", envelope["code"])
			require.Equal(t, "rzm index", envelope["remediation"])
		})
	}
}

func TestCapabilitiesToolReportsSemanticReadyWhileCodePending(t *testing.T) {
	view := &mutableRuntimeView{}
	view.set(runtimeview.Snapshot{
		Semantic: runtimeview.CapabilityState{Done: true, Ready: true},
	})
	cfg := Config{
		VaultPath:     t.TempDir(),
		VaultDef:      obsidian.VaultDefinition{Name: "vault", Path: t.TempDir()},
		Runtime:       view,
		ToolInventory: staticToolInventory{available: []string{"capabilities"}},
	}
	resp, err := CapabilitiesTool(cfg)(context.Background(), mcp.CallToolRequest{})
	require.NoError(t, err)
	require.False(t, resp.IsError)

	var payload CapabilitiesResponse
	require.NoError(t, json.Unmarshal([]byte(resp.Content[0].(mcp.TextContent).Text), &payload))
	require.True(t, payload.Server.Ready)
	require.False(t, payload.Features.CodeAnchors)
	require.False(t, payload.Features.CodeEmbeddings)
}

func TestCapabilitiesToolRespondsDuringColdStart(t *testing.T) {
	view := &mutableRuntimeView{}
	cfg := Config{
		VaultPath:     t.TempDir(),
		VaultDef:      obsidian.VaultDefinition{Name: "vault", Path: t.TempDir()},
		Runtime:       view,
		ToolInventory: staticToolInventory{available: []string{"capabilities"}},
	}
	resp, err := CapabilitiesTool(cfg)(context.Background(), mcp.CallToolRequest{})
	require.NoError(t, err)
	require.False(t, resp.IsError)

	var payload CapabilitiesResponse
	require.NoError(t, json.Unmarshal([]byte(resp.Content[0].(mcp.TextContent).Text), &payload))
	require.False(t, payload.Server.Ready)
	require.False(t, payload.Features.NoteEmbeddings)
	require.False(t, payload.Features.CodeAnchors)
	require.Zero(t, view.searchWaits.Load(), "cold capabilities must not wait for search")
	require.Zero(t, view.semanticWaits.Load(), "cold capabilities must not wait for semantic readiness")
	require.Zero(t, view.codeWaits.Load(), "cold capabilities must not wait for code indexing")
}

type staticToolInventory struct {
	available []string
	mutating  []string
}

func (inventory staticToolInventory) AvailableToolNames(bool) []string {
	return inventory.available
}

func (inventory staticToolInventory) MutatingToolNames(bool) []string {
	return inventory.mutating
}

func TestListPropertiesTool(t *testing.T) {
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "vault")
	if err := os.MkdirAll(vaultPath, 0o755); err != nil {
		t.Fatalf("failed to create vault dir: %v", err)
	}

	content := `---
office: AOGR
count: 2
---`
	if err := os.WriteFile(filepath.Join(vaultPath, "note.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write note.md: %v", err)
	}

	obsidianConfig := filepath.Join(tempDir, "obsidian.json")
	configBody, _ := json.Marshal(map[string]any{
		"vaults": map[string]any{
			"vault": map[string]any{"path": vaultPath},
		},
	})
	if err := os.WriteFile(obsidianConfig, configBody, 0o644); err != nil {
		t.Fatalf("failed to write obsidian config: %v", err)
	}

	origConfig := obsidian.ObsidianConfigFile
	obsidian.ObsidianConfigFile = func() (string, error) { return obsidianConfig, nil }
	defer func() { obsidian.ObsidianConfigFile = origConfig }()

	cfg := Config{
		Vault:     &obsidian.Vault{Name: "vault"},
		VaultPath: vaultPath,
		VaultDef:  obsidian.VaultDefinition{Name: "vault", Path: vaultPath},
		Debug:     false,
	}
	store := newIntelStore(t)
	cfg.NoteMetadata = configuredMCPNoteMetadata(t)
	_, err := cfg.NoteMetadata.EnsureIndexed(context.Background(), cfg.VaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	cfg.IntelStore = store

	tool := ListPropertiesTool(cfg)
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "list_properties",
			Arguments: map[string]interface{}{
				"valueLimit": float64(5),
			},
		},
	}

	resp, err := tool(context.Background(), req)
	assert.NoError(t, err)
	if !assert.Len(t, resp.Content, 1) {
		return
	}

	text, ok := resp.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", resp.Content[0])
	}

	var parsed PropertyListResponse
	err = json.Unmarshal([]byte(text.Text), &parsed)
	assert.NoError(t, err)
	assert.Len(t, parsed.Properties, 2)

	var officeFound bool
	for _, p := range parsed.Properties {
		if p.Name == "office" {
			officeFound = true
			assert.Equal(t, 1, p.NoteCount)
			assert.Equal(t, []string{"AOGR"}, p.EnumValues)
		}
	}
	assert.True(t, officeFound, "expected office property in response")
}

func TestCommunityListToolUsesProvidedIntelStore(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "vault")
	require.NoError(t, os.MkdirAll(vaultPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "Alpha.md"), []byte("# Alpha\n\nSee [[Beta]].\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "Beta.md"), []byte("# Beta\n\nBack to [[Alpha]].\n"), 0o644))

	obsidianConfig := filepath.Join(tempDir, "obsidian.json")
	cfgBody, err := json.Marshal(map[string]any{
		"vaults": map[string]any{
			"vault": map[string]string{"path": vaultPath},
		},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(obsidianConfig, cfgBody, 0o644))

	origConfig := obsidian.ObsidianConfigFile
	obsidian.ObsidianConfigFile = func() (string, error) { return obsidianConfig, nil }
	defer func() { obsidian.ObsidianConfigFile = origConfig }()

	store := newIntelStore(t)
	cfg := Config{
		Vault:      &obsidian.Vault{Name: "vault"},
		VaultPath:  vaultPath,
		VaultDef:   obsidian.VaultDefinition{Name: "vault", Path: vaultPath},
		IntelStore: store,
	}
	cfg.NoteMetadata = configuredMCPNoteMetadata(t)
	_, err = cfg.NoteMetadata.EnsureIndexed(ctx, cfg.VaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)

	tool := CommunityListTool(cfg)
	resp, err := tool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "community_list",
			Arguments: map[string]any{"maxCommunities": 5, "maxTopNotes": 5},
		},
	})
	require.NoError(t, err)
	require.False(t, resp.IsError)
	require.Len(t, resp.Content, 1)

	text, ok := resp.Content[0].(mcp.TextContent)
	require.True(t, ok)

	var parsed CommunityListResponse
	require.NoError(t, json.Unmarshal([]byte(text.Text), &parsed))
	require.NotEmpty(t, parsed.Communities)
	require.GreaterOrEqual(t, parsed.Stats.NodeCount, 2)
}

func TestReportToolAcceptsCodeSimilarity(t *testing.T) {
	ctx := context.Background()
	vaultPath := t.TempDir()
	store := newIntelStore(t)
	cfg := Config{
		VaultPath:  vaultPath,
		VaultDef:   obsidian.VaultDefinition{Name: "vault", Path: vaultPath},
		IntelStore: store,
	}

	tool := ReportTool(cfg)
	resp, err := tool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "report",
			Arguments: map[string]any{"op": "code_similarity", "paths": []any{"pkg"}},
		},
	})
	require.NoError(t, err)
	require.False(t, resp.IsError)
	require.Len(t, resp.Content, 1)

	text, ok := resp.Content[0].(mcp.TextContent)
	require.True(t, ok)
	var parsed ReportResponse
	require.NoError(t, json.Unmarshal([]byte(text.Text), &parsed))
	require.Equal(t, "code_similarity", parsed.Op)
	require.NotEmpty(t, parsed.Warnings)
	encoded, err := json.Marshal(parsed.Data)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "similarFunctions")
	require.Contains(t, string(encoded), "overlappingTypes")
}

func TestListPropertiesToolWithOnlyRaisesValueLimit(t *testing.T) {
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "vault")
	if err := os.MkdirAll(vaultPath, 0o755); err != nil {
		t.Fatalf("failed to create vault dir: %v", err)
	}

	var contentBuilder strings.Builder
	contentBuilder.WriteString("---\nwho:\n")
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&contentBuilder, "  - Person %02d\n", i)
	}
	contentBuilder.WriteString("omit: skip\n---")

	if err := os.WriteFile(filepath.Join(vaultPath, "note.md"), []byte(contentBuilder.String()), 0o644); err != nil {
		t.Fatalf("failed to write note.md: %v", err)
	}

	obsidianConfig := filepath.Join(tempDir, "obsidian.json")
	configBody, _ := json.Marshal(map[string]any{
		"vaults": map[string]any{
			"vault": map[string]any{"path": vaultPath},
		},
	})
	if err := os.WriteFile(obsidianConfig, configBody, 0o644); err != nil {
		t.Fatalf("failed to write obsidian config: %v", err)
	}

	origConfig := obsidian.ObsidianConfigFile
	obsidian.ObsidianConfigFile = func() (string, error) { return obsidianConfig, nil }
	defer func() { obsidian.ObsidianConfigFile = origConfig }()

	cfg := Config{
		Vault:     &obsidian.Vault{Name: "vault"},
		VaultPath: vaultPath,
		VaultDef:  obsidian.VaultDefinition{Name: "vault", Path: vaultPath},
		Debug:     false,
	}
	store := newIntelStore(t)
	cfg.NoteMetadata = configuredMCPNoteMetadata(t)
	_, err := cfg.NoteMetadata.EnsureIndexed(context.Background(), cfg.VaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	cfg.IntelStore = store

	tool := ListPropertiesTool(cfg)
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "list_properties",
			Arguments: map[string]interface{}{
				"only": []interface{}{"who"},
			},
		},
	}

	resp, err := tool(context.Background(), req)
	assert.NoError(t, err)
	if !assert.Len(t, resp.Content, 1) {
		return
	}

	text, ok := resp.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", resp.Content[0])
	}

	var parsed PropertyListResponse
	err = json.Unmarshal([]byte(text.Text), &parsed)
	assert.NoError(t, err)
	if !assert.Len(t, parsed.Properties, 1) {
		return
	}

	assert.Equal(t, "who", parsed.Properties[0].Name)
	assert.Equal(t, 30, parsed.Properties[0].DistinctValueCount)
	assert.False(t, parsed.Properties[0].TruncatedValueSet)
	assert.Len(t, parsed.Properties[0].EnumValues, 30, "valueLimit should be raised to enumerate all values")
}

func TestFileContextTool(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	vaultPath := filepath.Join(root, "vault")
	require.NoError(t, os.MkdirAll(vaultPath, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "A.md"), []byte("Link to [[B]]"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "B.md"), []byte("Link to [[A]]"), 0o644))

	configFile := filepath.Join(root, "obsidian.json")
	configBody, _ := json.Marshal(map[string]any{
		"vaults": map[string]any{
			"random": map[string]any{"path": vaultPath},
		},
	})
	require.NoError(t, os.WriteFile(configFile, configBody, 0o644))
	origConfig := obsidian.ObsidianConfigFile
	obsidian.ObsidianConfigFile = func() (string, error) { return configFile, nil }
	defer func() { obsidian.ObsidianConfigFile = origConfig }()

	cfg := Config{
		Vault:          &obsidian.Vault{Name: "vault"},
		VaultPath:      vaultPath,
		VaultDef:       obsidian.VaultDefinition{Name: "vault", Path: vaultPath},
		Debug:          false,
		SuppressedTags: []string{},
		ReadWrite:      true,
		NoteMetadata:   configuredMCPNoteMetadata(t),
	}
	store := newIntelStore(t)
	_, err := cfg.NoteMetadata.EnsureIndexed(context.Background(), cfg.VaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	cfg.IntelStore = store

	tool := FileContextTool(cfg)
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "file_context",
			Arguments: map[string]interface{}{
				"files": []interface{}{"A.md", "B.md"},
			},
		},
	}

	resp, err := tool(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, resp.Content, 1)
	text, ok := resp.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, text.Text, "tool: file_context")
	assert.Contains(t, text.Text, "path: A.md")
	assert.Contains(t, text.Text, "path: B.md")
	assert.LessOrEqual(t, len(text.Text), 60000)

	// Test partial error handling: one valid file, one missing file
	reqPartial := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "file_context",
			Arguments: map[string]interface{}{
				"files": []interface{}{"A.md", "NonExistent.md"},
			},
		},
	}
	respPartial, err := tool(context.Background(), reqPartial)
	require.NoError(t, err)
	require.Len(t, respPartial.Content, 1)
	textPartial, ok := respPartial.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, textPartial.Text, "tool: file_context")
	assert.Contains(t, textPartial.Text, "path: A.md")
	assert.Contains(t, textPartial.Text, "NonExistent.md")
	assert.Contains(t, textPartial.Text, "not found")

	reqApply := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "file_context",
			Arguments: map[string]interface{}{
				"files":             []interface{}{"A.md"},
				"ensureLinkTargets": "apply",
			},
		},
	}
	respApply, err := tool(context.Background(), reqApply)
	require.NoError(t, err)
	require.False(t, respApply.IsError)
}

func TestFileContextToolRefusesEnsureApplyWhenReadOnly(t *testing.T) {
	tool := FileContextTool(Config{ReadWrite: false})
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "file_context",
			Arguments: map[string]interface{}{
				"files":             []interface{}{"A.md"},
				"ensureLinkTargets": "apply",
			},
		},
	}

	resp, err := tool(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Len(t, resp.Content, 1)
	text, ok := resp.Content[0].(mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, "ensureLinkTargets=apply requires MCP read-write mode")
}

func TestFileContextToolReturnsIndexedEnrichmentOnlyForIndexedReadOnlyPath(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "worker.go"), []byte("package worker\n"), 0o644))
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "file_context",
			Arguments: map[string]interface{}{
				"files": []interface{}{"worker.go"},
			},
		},
	}
	base := Config{
		Vault:        &obsidian.Vault{Name: root},
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Name: "vault", Path: root},
		IntelStore:   store,
		NoteMetadata: configuredMCPNoteMetadata(t),
	}

	liveResponse, err := FileContextTool(base)(context.Background(), request)
	require.NoError(t, err)
	require.False(t, liveResponse.IsError, "%#v", liveResponse.Content)
	var livePayload ContextTextResponse
	require.NoError(t, json.Unmarshal([]byte(liveResponse.Content[0].(mcp.TextContent).Text), &livePayload))
	require.Nil(t, livePayload.IndexedEnrichment)

	missing := actions.IndexedContextFreshness{
		State:       actions.IndexedContextMissing,
		WarningCode: "indexed-context-missing",
		Remediation: "rzm index",
	}
	indexed := base
	indexed.IndexedReadOnlyFileContext = true
	indexed.IndexedContextUnavailable = &missing
	indexedResponse, err := FileContextTool(indexed)(context.Background(), request)
	require.NoError(t, err)
	require.False(t, indexedResponse.IsError, "%#v", indexedResponse.Content)
	var indexedPayload ContextTextResponse
	require.NoError(t, json.Unmarshal([]byte(indexedResponse.Content[0].(mcp.TextContent).Text), &indexedPayload))
	require.NotNil(t, indexedPayload.IndexedEnrichment)
	require.Equal(t, actions.IndexedContextMissing, indexedPayload.IndexedEnrichment.Status)
	require.Zero(t, indexedPayload.IndexedEnrichment.Reads)
	require.Zero(t, indexedPayload.IndexedEnrichment.Results)
	require.Len(t, indexedPayload.IndexedEnrichment.Warnings, 1)
	require.Equal(t, "indexed-context-missing", indexedPayload.IndexedEnrichment.Warnings[0].Code)
}

func TestFileContextToolIndexedReadOnlyUsesDedicatedSessionStore(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "worker.go"), []byte("package worker\n"), 0o644))

	indexPath := filepath.Join(root, ".rhizome", "index.db")
	sessionStore, err := sqlitefixture.Open(indexPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sessionStore.Close() })
	intelStore, err := semdb.OpenReadOnlyExisting(indexPath, context.Background(), sqliteutil.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	missing := actions.IndexedContextFreshness{
		State:       actions.IndexedContextMissing,
		WarningCode: "indexed-context-missing",
		Remediation: "rzm index",
	}
	tool := FileContextTool(Config{
		Vault:                      &obsidian.Vault{Name: root},
		VaultPath:                  root,
		VaultDef:                   obsidian.VaultDefinition{Name: "vault", Path: root},
		IntelStore:                 intelStore,
		SessionStore:               sessionStore,
		NoteMetadata:               configuredMCPNoteMetadata(t),
		IndexedReadOnlyFileContext: true,
		IndexedContextUnavailable:  &missing,
	})
	request := mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "file_context",
		Arguments: map[string]interface{}{
			"files":     []interface{}{"worker.go"},
			"sessionId": "stable-session",
		},
	}}

	first, err := tool(context.Background(), request)
	require.NoError(t, err)
	require.False(t, first.IsError, "%#v", first.Content)
	var firstPayload ContextTextResponse
	require.NoError(t, json.Unmarshal([]byte(first.Content[0].(mcp.TextContent).Text), &firstPayload))
	require.Zero(t, firstPayload.DedupeHits)

	second, err := tool(context.Background(), request)
	require.NoError(t, err)
	require.False(t, second.IsError, "%#v", second.Content)
	var secondPayload ContextTextResponse
	require.NoError(t, json.Unmarshal([]byte(second.Content[0].(mcp.TextContent).Text), &secondPayload))
	require.Positive(t, secondPayload.DedupeHits)
	require.Error(t, intelStore.EnsureSession(context.Background(), "must-remain-read-only"))
}

func TestFileContextTool_ExcludesDocs(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	vaultPath := filepath.Join(root, "vault")
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "src"), 0o755))

	docPath := filepath.Join(vaultPath, "CONTEXT.md")
	require.NoError(t, os.WriteFile(docPath, []byte("doc"), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "Note.md"), []byte("content"), 0o644))

	codePath := filepath.Join(vaultPath, "src", "main.go")
	require.NoError(t, os.WriteFile(codePath, []byte("package main"), 0o644))

	configFile := filepath.Join(root, "obsidian.json")
	configBody, _ := json.Marshal(map[string]any{
		"vaults": map[string]any{
			"random": map[string]any{"path": vaultPath},
		},
	})
	require.NoError(t, os.WriteFile(configFile, configBody, 0o644))
	origConfig := obsidian.ObsidianConfigFile
	obsidian.ObsidianConfigFile = func() (string, error) { return configFile, nil }
	defer func() { obsidian.ObsidianConfigFile = origConfig }()

	cfg := Config{
		Vault:          &obsidian.Vault{Name: "vault"},
		VaultPath:      vaultPath,
		VaultDef:       obsidian.VaultDefinition{Name: "vault", Path: vaultPath},
		Debug:          false,
		SuppressedTags: []string{},
		ReadWrite:      true,
		NoteMetadata:   configuredMCPNoteMetadata(t),
	}

	tool := FileContextTool(cfg)
	// Positive control: without exclusions the ancestor doc is part of context.
	defaultResp, err := tool(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "file_context", Arguments: map[string]interface{}{"files": []interface{}{"src/main.go"}},
	}})
	require.NoError(t, err)
	require.False(t, defaultResp.IsError)
	require.Contains(t, defaultResp.Content[0].(mcp.TextContent).Text, "CONTEXT.md")

	resolvedDocPath := paths.ResolveSymlinks(docPath)
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "file_context",
			Arguments: map[string]interface{}{
				"files":             []interface{}{"src/main.go"},
				"exclude_doc_paths": []interface{}{docPath, resolvedDocPath.String()},
			},
		},
	}

	resp, err := tool(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, resp.Content, 1)
	text, ok := resp.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, text.Text, "tool: file_context")
	assert.NotContains(t, text.Text, "CONTEXT.md")
}

func TestVaultContextTool(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	vaultPath := filepath.Join(root, "vault")
	require.NoError(t, os.MkdirAll(vaultPath, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "A.md"), []byte("Link to [[B]]"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "B.md"), []byte(`---
Summary: Hello
Tags: [foo, bar]
---
Content`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "MOC.md"), []byte("# MOC"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "Orphan.md"), []byte(""), 0o644))

	configFile := filepath.Join(root, "obsidian.json")
	configBody, _ := json.Marshal(map[string]any{
		"vaults": map[string]any{
			"random": map[string]any{"path": vaultPath},
		},
	})
	require.NoError(t, os.WriteFile(configFile, configBody, 0o644))
	origConfig := obsidian.ObsidianConfigFile
	obsidian.ObsidianConfigFile = func() (string, error) { return configFile, nil }
	defer func() { obsidian.ObsidianConfigFile = origConfig }()

	cfg := Config{
		Vault:          &obsidian.Vault{Name: "vault"},
		VaultPath:      vaultPath,
		VaultDef:       obsidian.VaultDefinition{Name: "vault", Path: vaultPath},
		Debug:          false,
		SuppressedTags: []string{},
		ReadWrite:      true,
		NoteMetadata:   configuredMCPNoteMetadata(t),
	}
	store := newIntelStore(t)
	_, err := cfg.NoteMetadata.EnsureIndexed(context.Background(), cfg.VaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	cfg.IntelStore = store

	tool := VaultContextTool(cfg)
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "vault_context",
			Arguments: map[string]interface{}{
				"keyPatterns":  []interface{}{"find:MOC"},
				"contextFiles": []interface{}{"A.md"},
			},
		},
	}

	resp, err := tool(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, resp.Content, 1)

	text, ok := resp.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, text.Text, "tool: vault_context")
	assert.Contains(t, text.Text, "find:MOC")
	assert.Contains(t, text.Text, "MOC.md")
	assert.Contains(t, text.Text, "B.md")
	assert.Contains(t, text.Text, "Hello")
	assert.LessOrEqual(t, len(text.Text), 60000)
}

func TestVaultContextRequestScopeDefaultsRichAndAcceptsMinimalBootstrap(t *testing.T) {
	require.Equal(t, actions.VaultContextRequestScopeRich, vaultContextRequestScope(nil))
	require.Equal(t, actions.VaultContextRequestScopeRich, vaultContextRequestScope(map[string]any{}))
	require.Equal(t, actions.VaultContextRequestScopeMinimalBootstrap, vaultContextRequestScope(map[string]any{
		"requestScope": string(actions.VaultContextRequestScopeMinimalBootstrap),
	}))
	require.Equal(t, actions.VaultContextRequestScopeIndexedBootstrap, vaultContextRequestScope(map[string]any{
		"requestScope": string(actions.VaultContextRequestScopeIndexedBootstrap),
	}))
}

func TestFindConnections_RejectsBothNoteAndText(t *testing.T) {
	result, err := FindConnectionsTool(Config{})(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "find_connections",
			Arguments: map[string]any{"note": "Notes/A.md", "text": "related notes"},
		},
	})
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	content, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, content.Text, "provide only one of note or text")
}

func TestFindConnections_AbsolutePathNormalized(t *testing.T) {
	root := t.TempDir()
	vaultPath := filepath.Join(root, "vault")
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "Notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "Notes", "A.md"), []byte("# A\n\nalpha\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "Notes", "B.md"), []byte("# B\n\nalpha\n"), 0o644))
	noteAbs := filepath.Join(vaultPath, "Notes", "A.md")

	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store := newIntelStore(t)
	addDocSection(t, store, prov, "Notes/A.md", "A", "alpha", "section-a", "chunk-a")
	addDocSection(t, store, prov, "Notes/B.md", "B", "alpha", "section-b", "chunk-b")

	cfg := Config{
		VaultPath:     vaultPath,
		VaultDef:      obsidian.VaultDefinition{Path: vaultPath},
		IntelStore:    store,
		SessionStore:  store,
		EmbedProvider: prov,
		EmbeddingsOn:  true,
	}
	tool := FindConnectionsTool(cfg)
	res, err := tool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "find_connections",
			Arguments: map[string]any{
				"note":      noteAbs,
				"limit":     1,
				"sessionId": "find-connections-session",
			},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Len(t, res.Content, 1)

	tc, ok := res.Content[0].(mcp.TextContent)
	require.True(t, ok)

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(tc.Text), &payload))
	require.Equal(t, "note", payload["inputType"])
	require.Equal(t, "Notes/A.md", payload["note"])

	matches, ok := payload["matches"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, matches)

	second, err := tool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "find_connections",
			Arguments: map[string]any{
				"note":      noteAbs,
				"limit":     1,
				"sessionId": "find-connections-session",
			},
		},
	})
	require.NoError(t, err)
	require.False(t, second.IsError)
	var deduped map[string]any
	require.NoError(t, json.Unmarshal([]byte(second.Content[0].(mcp.TextContent).Text), &deduped))
	require.Positive(t, int(deduped["dedupeHits"].(float64)))
}

func TestResolveSemanticConnectionNotePathPreservesTypedExtensions(t *testing.T) {
	root := t.TempDir()
	vaultPath := filepath.Join(root, "vault")
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "Notes"), 0o755))

	resolved, err := resolveSemanticConnectionNotePath(vaultPath, filepath.Join(vaultPath, "Notes", "Decision.html"))
	require.NoError(t, err)
	require.Equal(t, "Notes/Decision.html", resolved)

	resolved, err = resolveSemanticConnectionNotePath(vaultPath, "Notes/Decision.MD")
	require.NoError(t, err)
	require.Equal(t, "Notes/Decision.MD", resolved)
}

func TestFindConnections_NoteInputUsesOntologyNodeEmbeddings(t *testing.T) {
	root := t.TempDir()
	vaultPath := filepath.Join(root, "vault")
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "Notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "Notes", "A.md"), []byte("# A\n\nalpha\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "Notes", "B.md"), []byte("# B\n\nalpha\n"), 0o644))

	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store := newIntelStore(t)
	ctx := context.Background()
	node := codeanchor.IntelOntologyNode{
		NodeID:      "node:a",
		NotePath:    "Notes/A.md",
		NodeRefJSON: `{"notePath":"Notes/A.md","nodeId":"a","typeName":"ReferenceDoc"}`,
		NodeKind:    "ROOT",
		TypeName:    "ReferenceDoc",
		Title:       "A",
	}
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{node.NotePath}, []codeanchor.IntelOntologyNode{node}))
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{node.NodeID}, []codeanchor.IntelChunk{{
		ChunkID:     "node-a-chunk",
		OwnerID:     node.NodeID,
		OwnerType:   "ontology_node",
		Ord:         0,
		Granularity: "node_body",
		Breadcrumb:  "A",
		Heading:     "A",
		ContentHash: "node-a-hash",
	}}))
	vecs, err := prov.EmbedTexts(ctx, []string{"alpha"})
	require.NoError(t, err)
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{"node-a-chunk": vecs[0]}))
	addDocSection(t, store, prov, "Notes/B.md", "B", "alpha", "section-b", "chunk-b")

	cfg := Config{
		VaultPath:     vaultPath,
		VaultDef:      obsidian.VaultDefinition{Path: vaultPath},
		IntelStore:    store,
		EmbedProvider: prov,
		EmbeddingsOn:  true,
	}
	tool := FindConnectionsTool(cfg)
	res, err := tool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "find_connections",
			Arguments: map[string]any{
				"note":  "Notes/A.md",
				"limit": 1,
			},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Len(t, res.Content, 1)

	tc, ok := res.Content[0].(mcp.TextContent)
	require.True(t, ok)
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(tc.Text), &payload))
	matches, ok := payload["matches"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, matches)
}

func TestFindConnections_TextInputSkipsNoteChunks(t *testing.T) {
	root := t.TempDir()
	vaultPath := filepath.Join(root, "vault")
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "Notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "Notes", "B.md"), []byte("# B\n\nhello world\n"), 0o644))

	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store := newIntelStore(t)
	addDocSection(t, store, prov, "Notes/B.md", "B", "hello world", "section-b", "chunk-b")

	cfg := Config{
		VaultPath:     vaultPath,
		VaultDef:      obsidian.VaultDefinition{Path: vaultPath},
		IntelStore:    store,
		EmbedProvider: prov,
		EmbeddingsOn:  true,
	}

	tool := FindConnectionsTool(cfg)
	res, err := tool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "find_connections",
			Arguments: map[string]any{
				"text":  "hello world",
				"limit": 1,
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.False(t, res.IsError)
	require.Len(t, res.Content, 1)

	tc, ok := res.Content[0].(mcp.TextContent)
	require.True(t, ok)

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(tc.Text), &payload))
	require.Equal(t, "text", payload["inputType"])
	_, hasNote := payload["note"]
	require.False(t, hasNote)

	matches, ok := payload["matches"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, matches)
}

func TestFindConnections_TextInputReportsProviderFailureAfterManagedStoreIsAvailable(t *testing.T) {
	store := newIntelStore(t)
	tool := FindConnectionsTool(Config{
		IntelStore:       store,
		IntelStorePolicy: IntelStoreManagedReadOnly,
	})
	res, err := tool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "find_connections",
			Arguments: map[string]any{"text": "hello world"},
		},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Len(t, res.Content, 1)
	text, ok := res.Content[0].(mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, "embedding provider is not configured")
	require.NotContains(t, text.Text, "indexed-context-incompatible")
}

func TestSemanticQuery_DiversifiesWithinNote(t *testing.T) {
	vaultPath := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "Notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "Notes", "A.md"), []byte("# A\n\nalpha\n\n## Details\n\nalpha\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "Notes", "B.md"), []byte("# B\n\nalpha\n"), 0o644))

	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store := newIntelStore(t)
	addDocSections(t, store, prov, "Notes/A.md", []docSectionSpec{
		{SectionID: "a1", Title: "A", Content: "alpha", ChunkID: "chunk-a1"},
		{SectionID: "a2", Title: "Details", Content: "alpha", ChunkID: "chunk-a2"},
	})
	addDocSection(t, store, prov, "Notes/B.md", "B", "alpha", "section-b", "chunk-b")

	cfg := Config{
		VaultPath:     vaultPath,
		VaultDef:      obsidian.VaultDefinition{Path: vaultPath},
		IntelStore:    store,
		EmbeddingsOn:  true,
		EmbedProvider: prov,
	}

	tool := SemanticQueryTool(cfg)
	res, err := tool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "semantic_query",
			Arguments: map[string]any{"queries": []any{"alpha"}, "limit": 2},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Len(t, res.Content, 1)

	tc, ok := res.Content[0].(mcp.TextContent)
	require.True(t, ok)

	var payload struct {
		Matches []SemanticMatchPayload `json:"matches"`
		Text    string                 `json:"text"`
	}
	require.NoError(t, json.Unmarshal([]byte(tc.Text), &payload))

	require.Len(t, payload.Matches, 2)
	require.NotEqual(t, payload.Matches[0].Path, payload.Matches[1].Path)
	paths := []string{payload.Matches[0].Path, payload.Matches[1].Path}
	require.Contains(t, paths, "Notes/A.md")
	require.Contains(t, paths, "Notes/B.md")
	for _, match := range payload.Matches {
		require.Equal(t, "note", match.Type)
		require.Equal(t, -1, match.ChunkIndex)
	}
	require.NotEmpty(t, payload.Text)

	// Equivalent bodies (differing only in case and punctuation) within one
	// displayed group collapse to a variant stub; a distinct body keeps its own.
	vaultPath = t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "docs", "area"), 0o755))
	store = newIntelStore(t)
	bodies := map[string]string{
		"docs/area/one.md":   "Reset domain drops and recreates the search index for every vault.",
		"docs/area/two.md":   "RESET DOMAIN -- drops AND recreates the search index, for every vault!!",
		"docs/area/three.md": "Compaction rewrites older segments to reclaim search index storage space.",
	}
	for notePath, body := range bodies {
		require.NoError(t, os.WriteFile(filepath.Join(vaultPath, filepath.FromSlash(notePath)), []byte(body+"\n"), 0o644))
		addDocSection(t, store, prov, notePath, filepath.Base(notePath), body, "section-"+notePath, "chunk-"+notePath)
	}
	cfg = Config{VaultPath: vaultPath, VaultDef: obsidian.VaultDefinition{Path: vaultPath}, IntelStore: store, EmbeddingsOn: true, EmbedProvider: prov}
	res, err = SemanticQueryTool(cfg)(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "semantic_query", Arguments: map[string]any{"queries": []any{"search index"}, "limit": 5},
	}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	payload.Matches = nil
	require.NoError(t, json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &payload))
	byPath := map[string]SemanticMatchPayload{}
	for _, match := range payload.Matches {
		byPath[match.Path] = match
	}
	require.Len(t, byPath, 3)
	one, two := byPath["docs/area/one.md"], byPath["docs/area/two.md"]
	variants := 0
	for _, pair := range [][2]SemanticMatchPayload{{one, two}, {two, one}} {
		if pair[0].StubContent == "variant of "+pair[1].Path+"; read if needed" {
			variants++
			require.Equal(t, bodies[pair[1].Path], pair[1].FullFileContent)
		}
	}
	require.Equal(t, 1, variants, "exactly one equivalent body collapses into a variant of the other")
	require.Equal(t, bodies["docs/area/three.md"], byPath["docs/area/three.md"].FullFileContent)
}

func TestSemanticQuery_SupportsQueriesAndPathsAndContinuation(t *testing.T) {
	vaultPath := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "Notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "Notes", "A.md"), []byte("# A\n\nalpha\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "Notes", "B.md"), []byte("# B\n\nalpha\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "Notes", "C.md"), []byte("# C\n\nalpha\n"), 0o644))

	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store := newIntelStore(t)
	addDocSection(t, store, prov, "Notes/A.md", "A", "alpha", "section-a", "chunk-a")
	addDocSection(t, store, prov, "Notes/B.md", "B", "alpha", "section-b", "chunk-b")
	addDocSection(t, store, prov, "Notes/C.md", "C", "alpha", "section-c", "chunk-c")
	addCurrentNoteOwnership(t, store, "Notes/A.md", "Notes/B.md", "Notes/C.md")

	cfg := Config{
		VaultPath:     vaultPath,
		VaultDef:      obsidian.VaultDefinition{Path: vaultPath},
		IntelStore:    store,
		EmbeddingsOn:  true,
		EmbedProvider: prov,
	}

	tool := SemanticQueryTool(cfg)
	res, err := tool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "semantic_query",
			Arguments: map[string]any{
				"queries": []any{"primary", "extra"},
				"paths":   []any{"Notes/A.md"},
				"limit":   1,
			},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	tc, ok := res.Content[0].(mcp.TextContent)
	require.True(t, ok)

	var payload struct {
		Query             string                 `json:"query"`
		Matches           []SemanticMatchPayload `json:"matches"`
		ContinuationToken string                 `json:"continuationToken"`
	}
	require.NoError(t, json.Unmarshal([]byte(tc.Text), &payload))
	require.Equal(t, "primary\n\nextra", payload.Query)
	require.Len(t, payload.Matches, 1)
	require.NotEmpty(t, payload.ContinuationToken)

	// Continuation requests repeat the same request alongside the token.
	res2, err := tool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "semantic_query",
			Arguments: map[string]any{
				"queries":           []any{"primary", "extra"},
				"paths":             []any{"Notes/A.md"},
				"limit":             1,
				"continuationToken": payload.ContinuationToken,
			},
		},
	})
	require.NoError(t, err)
	require.False(t, res2.IsError)
	tc2, ok := res2.Content[0].(mcp.TextContent)
	require.True(t, ok)
	var next struct {
		Matches []SemanticMatchPayload `json:"matches"`
	}
	require.NoError(t, json.Unmarshal([]byte(tc2.Text), &next))
	require.Len(t, next.Matches, 1)
	require.NotEqual(t, payload.Matches[0].Path, next.Matches[0].Path)

	// The handler owns query-input normalization: blank-line facets split,
	// whitespace and empty fragments drop, CRLF matches LF, and query objects
	// keep their per-query modes.
	type inputPayload struct {
		Query       string                 `json:"query"`
		Queries     []string               `json:"queries"`
		QueryInputs []SemanticQueryInput   `json:"queryInputs"`
		ModeApplied string                 `json:"modeApplied"`
		Matches     []SemanticMatchPayload `json:"matches"`
	}
	call := func(t *testing.T, args map[string]any) (*mcp.CallToolResult, inputPayload) {
		t.Helper()
		res, err := tool(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "semantic_query", Arguments: args}})
		require.NoError(t, err)
		var out inputPayload
		if !res.IsError {
			require.NoError(t, json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &out))
		}
		return res, out
	}
	for _, tt := range []struct {
		name    string
		queries []any
		want    []string
	}{
		{"splits blank-line facets", []any{"query1\n\nquery2", "query3"}, []string{"query1", "query2", "query3"}},
		{"trims outer whitespace", []any{"  query1  ", "\n\nquery2\n\n"}, []string{"query1", "query2"}},
		{"skips empty fragments", []any{"", "  ", "query1\n\n\n\nquery2", "\n\n", "query3"}, []string{"query1", "query2", "query3"}},
		{"CRLF matches LF", []any{"query1\r\n\r\nquery2"}, []string{"query1", "query2"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res, out := call(t, map[string]any{"queries": tt.queries, "limit": 5})
			require.False(t, res.IsError)
			require.Equal(t, tt.want, out.Queries)
			require.Equal(t, strings.Join(tt.want, "\n\n"), out.Query)
			require.Len(t, out.QueryInputs, len(tt.want))
			for i, input := range out.QueryInputs {
				require.Equal(t, tt.want[i], input.Text)
			}
		})
	}
	t.Run("all-empty input needs seeds", func(t *testing.T) {
		res, _ := call(t, map[string]any{"queries": []any{"", "  "}})
		require.True(t, res.IsError)
		require.Contains(t, res.Content[0].(mcp.TextContent).Text, "provide queries or seed paths")
		res, out := call(t, map[string]any{"queries": []any{"", "  "}, "paths": []any{"Notes/A.md"}, "limit": 5})
		require.False(t, res.IsError)
		require.Empty(t, out.Query)
		require.NotEmpty(t, out.Matches)
	})
	t.Run("typed entry skips blank inputs", func(t *testing.T) {
		resp, err := SemanticQueryUnifiedWithOptions(context.Background(), cfg, SemanticQueryOptions{
			Queries: []SemanticQueryInput{{Text: "  "}, {Text: "query1"}, {Text: "\r\n"}},
			Limit:   5,
		})
		require.NoError(t, err)
		require.Equal(t, "query1", resp.Query)
	})
	t.Run("query objects keep per-query modes", func(t *testing.T) {
		res, out := call(t, map[string]any{"queries": []any{
			map[string]any{"text": "docs for pkg/search/service.go", "mode": "docs_for_code"},
			map[string]any{"text": "tests for pkg/search/service.go", "mode": "tests_for_code"},
		}, "limit": 5})
		require.False(t, res.IsError)
		require.Equal(t, "docs for pkg/search/service.go\n\ntests for pkg/search/service.go", out.Query)
		require.Equal(t, []string{"docs for pkg/search/service.go", "tests for pkg/search/service.go"}, out.Queries)
		require.Equal(t, []SemanticQueryInput{
			{Text: "docs for pkg/search/service.go", Mode: "docs_for_code"},
			{Text: "tests for pkg/search/service.go", Mode: "tests_for_code"},
		}, out.QueryInputs)
		require.Empty(t, out.ModeApplied)
		require.NotEmpty(t, out.Matches)
	})
	t.Run("query objects reject legacy nested intent", func(t *testing.T) {
		res, _ := call(t, map[string]any{"queries": []any{
			map[string]any{"text": "docs for pkg/search/service.go", "intent": "docs_for_code"},
		}})
		require.True(t, res.IsError)
		require.Contains(t, res.Content[0].(mcp.TextContent).Text, "query objects use mode (not intent)")
	})
}

func TestSemanticQuery_ContinuationTokenPreservesRawDirectorySeeds(t *testing.T) {
	vaultPath := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "pkg", "search"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "pkg", "search", "CONTEXT.md"), []byte("# Context\n\nsearch module context\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "pkg", "search", "Guide.md"), []byte("# Guide\n\nsearch guide details\n"), 0o644))

	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store := newIntelStore(t)
	addDocSection(t, store, prov, "pkg/search/CONTEXT.md", "Context", "search module context", "context-section", "context-chunk")
	addDocSection(t, store, prov, "pkg/search/Guide.md", "Guide", "search guide details", "guide-section", "guide-chunk")
	addCurrentNoteOwnership(t, store, "pkg/search/CONTEXT.md", "pkg/search/Guide.md")

	cfg := Config{
		VaultPath:     vaultPath,
		VaultDef:      obsidian.VaultDefinition{Path: vaultPath},
		IntelStore:    store,
		EmbeddingsOn:  true,
		EmbedProvider: prov,
	}

	request := SemanticQueryOptions{
		Query:       "search",
		SeedTokens:  []string{"pkg/search"},
		Mode:        "subsystem_overview",
		Limit:       1,
		BudgetChars: 4000,
	}
	resp, err := SemanticQueryUnifiedWithOptions(context.Background(), cfg, request)
	require.NoError(t, err)
	require.NotEmpty(t, resp.ContinuationToken)

	// The raw directory seed is part of the request identity, so the next page
	// must repeat it rather than rely on cursor-carried resolution.
	request.ContinuationToken = resp.ContinuationToken
	nextResp, err := SemanticQueryUnifiedWithOptions(context.Background(), cfg, request)
	require.NoError(t, err)
	require.NotEmpty(t, nextResp.Matches)
}

func TestSemanticQuery_NoLimitPacksUntilBudget(t *testing.T) {
	vaultPath := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, ".rhizome"), 0o755))
	// Small budget to force early stop. LoadLocalConfig only needs budgetChars and a vault block.
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, ".rhizome", "config.yml"), []byte("notes: {}\nbudgetChars: 3800\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "Notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "Notes", "A.md"), []byte("# A\n\nalpha\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "Notes", "B.md"), []byte("# B\n\nalpha\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "Notes", "C.md"), []byte("# C\n\nalpha\n"), 0o644))

	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store := newIntelStore(t)
	addDocSection(t, store, prov, "Notes/A.md", "A", "alpha", "section-a", "chunk-a")
	addDocSection(t, store, prov, "Notes/B.md", "B", "alpha", "section-b", "chunk-b")
	addDocSection(t, store, prov, "Notes/C.md", "C", "alpha", "section-c", "chunk-c")

	cfg := Config{
		VaultPath:     vaultPath,
		VaultDef:      obsidian.VaultDefinition{Path: vaultPath},
		IntelStore:    store,
		EmbeddingsOn:  true,
		EmbedProvider: prov,
	}

	tool := SemanticQueryTool(cfg)
	res, err := tool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "semantic_query",
			Arguments: map[string]any{"queries": []any{"q"}}, // no limit
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Len(t, res.Content, 1)

	tc, ok := res.Content[0].(mcp.TextContent)
	require.True(t, ok)

	var payload struct {
		Matches           []SemanticMatchPayload `json:"matches"`
		Text              string                 `json:"text"`
		ContinuationToken string                 `json:"continuationToken"`
	}
	require.NoError(t, json.Unmarshal([]byte(tc.Text), &payload))
	require.LessOrEqual(t, len(tc.Text), 3800, "the complete encoded response honors the configured budget")
	require.NotEmpty(t, payload.Text)
	require.ElementsMatch(t, []string{"Notes/A.md", "Notes/B.md", "Notes/C.md"}, semanticMatchPaths(payload.Matches),
		"an omitted limit packs every fitting source rather than applying a small default")
	require.Empty(t, payload.ContinuationToken)
}
