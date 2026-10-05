package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type suppliedFilesReader struct{ obsidian.Note }

func (suppliedFilesReader) GetContents(obsidian.VaultDefinition, string) (string, error) {
	return "supplied reader body", nil
}

func TestFilesToolUsesSuppliedNoteReader(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "decision.md"), []byte("disk body"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes: {}\n"), 0o644))
	cfg := Config{Vault: &obsidian.Vault{Name: root}, VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, NoteReader: &suppliedFilesReader{}}
	result, err := FilesTool(cfg)(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "files", Arguments: map[string]any{"inputs": []any{"decision.md"}, "includeContent": true},
	}})
	require.NoError(t, err)
	require.False(t, result.IsError)
	var response FilesResponse
	require.NoError(t, json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &response))
	require.Len(t, response.Files, 1)
	require.Contains(t, response.Files[0].Content, "supplied reader body")
	require.NotContains(t, response.Files[0].Content, "disk body")
}

func TestFilesToolReturnsProjectedMetadataForMarkdownNotes(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "vault")
	require.NoError(t, os.MkdirAll(root, 0o755))
	content := "---\ntitle: Decision\ntags: [project]\n---\n#inline\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "decision.md"), []byte(content), 0o644))

	obsidianConfig := filepath.Join(tempDir, "obsidian.json")
	configBody, err := json.Marshal(map[string]any{
		"vaults": map[string]any{
			"vault": map[string]string{"path": root},
		},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(obsidianConfig, configBody, 0o644))
	origConfig := obsidian.ObsidianConfigFile
	obsidian.ObsidianConfigFile = func() (string, error) { return obsidianConfig, nil }
	defer func() { obsidian.ObsidianConfigFile = origConfig }()

	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "index.sqlite"))
	require.NoError(t, err)
	defer store.Close()
	indexer := configuredMCPNoteMetadata(t)
	vaultDef := obsidian.VaultDefinition{Name: "vault", Path: root}
	_, err = indexer.EnsureIndexed(ctx, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)

	cfg := Config{
		Vault:        &obsidian.Vault{Name: "vault"},
		VaultPath:    root,
		VaultDef:     vaultDef,
		IntelStore:   store,
		NoteMetadata: indexer,
	}
	tool := FilesTool(cfg)
	resp, err := tool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "files",
		Arguments: map[string]interface{}{
			"inputs":             []interface{}{"decision.md"},
			"includeContent":     false,
			"includeFrontmatter": true,
		},
	}})
	require.NoError(t, err)
	require.Len(t, resp.Content, 1)
	text, ok := resp.Content[0].(mcp.TextContent)
	require.True(t, ok)
	var parsed FilesResponse
	require.NoError(t, json.Unmarshal([]byte(text.Text), &parsed))
	require.Len(t, parsed.Files, 1)
	assert.ElementsMatch(t, []string{"project", "inline"}, parsed.Files[0].Tags)
	assert.Equal(t, "Decision", parsed.Files[0].Frontmatter["title"])
}

func TestFilesToolContentOmittedReasonAndDedupe(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "vault")
	require.NoError(t, os.MkdirAll(vaultPath, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "note.md"), []byte("hello world"), 0o644))

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

	storePath := filepath.Join(tempDir, "index.sqlite")
	store, err := sqlitefixture.Open(storePath)
	require.NoError(t, err)
	defer store.Close()

	cfg := Config{
		Vault:        &obsidian.Vault{Name: "vault"},
		VaultPath:    vaultPath,
		VaultDef:     obsidian.VaultDefinition{Name: "vault", Path: vaultPath},
		IntelStore:   store,
		SessionStore: store,
	}

	tool := FilesTool(cfg)
	sessionID := "session-dedupe"

	req1 := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "files",
			Arguments: map[string]interface{}{
				"sessionId":      sessionID,
				"inputs":         []interface{}{"note.md"},
				"includeContent": true,
			},
		},
	}
	resp1, err := tool(ctx, req1)
	require.NoError(t, err)
	require.Len(t, resp1.Content, 1)
	var page1 FilesResponse
	require.NoError(t, json.Unmarshal([]byte(resp1.Content[0].(mcp.TextContent).Text), &page1))
	require.Len(t, page1.Files, 1)
	assert.Equal(t, "hello world", page1.Files[0].Content)
	assert.Empty(t, page1.Files[0].ContentOmittedReason)

	req2 := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "files",
			Arguments: map[string]interface{}{
				"sessionId":      sessionID,
				"inputs":         []interface{}{"note.md"},
				"includeContent": true,
			},
		},
	}
	resp2, err := tool(ctx, req2)
	require.NoError(t, err)
	require.Len(t, resp2.Content, 1)
	var page2 FilesResponse
	require.NoError(t, json.Unmarshal([]byte(resp2.Content[0].(mcp.TextContent).Text), &page2))
	require.Len(t, page2.Files, 1)
	assert.Empty(t, page2.Files[0].Content)
	assert.Equal(t, contentOmittedDeduped, page2.Files[0].ContentOmittedReason)

	req3 := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "files",
			Arguments: map[string]interface{}{
				"sessionId":      sessionID,
				"inputs":         []interface{}{"note.md"},
				"includeContent": true,
				"dedupe":         false,
			},
		},
	}
	resp3, err := tool(ctx, req3)
	require.NoError(t, err)
	require.Len(t, resp3.Content, 1)
	var page3 FilesResponse
	require.NoError(t, json.Unmarshal([]byte(resp3.Content[0].(mcp.TextContent).Text), &page3))
	require.Len(t, page3.Files, 1)
	assert.Equal(t, "hello world", page3.Files[0].Content)
	assert.Empty(t, page3.Files[0].ContentOmittedReason)
}

func TestFilesToolBudgetPacksPages(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "vault")
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "src"), 0o755))

	codeBodyA := strings.Repeat("a", 200)
	codeBodyB := strings.Repeat("b", 200)
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "src", "a.go"), []byte(codeBodyA), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "src", "b.go"), []byte(codeBodyB), 0o644))

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

	storePath := filepath.Join(tempDir, "index.sqlite")
	store, err := sqlitefixture.Open(storePath)
	require.NoError(t, err)
	defer store.Close()

	require.NoError(t, store.ReplaceFileSummary(ctx, codeanchor.FileSummary{
		FilePath: "src/a.go",
		Lang:     codeanchor.LangGo,
		Hash:     "a1",
	}))
	require.NoError(t, store.ReplaceFileSummary(ctx, codeanchor.FileSummary{
		FilePath: "src/b.go",
		Lang:     codeanchor.LangGo,
		Hash:     "b1",
	}))

	cfg := Config{
		Vault:      &obsidian.Vault{Name: "vault"},
		VaultPath:  vaultPath,
		VaultDef:   obsidian.VaultDefinition{Name: "vault", Path: vaultPath},
		IntelStore: store,
	}

	tool := FilesTool(cfg)
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "files",
			Arguments: map[string]interface{}{
				"inputs":         []interface{}{"src/a.go", "src/b.go"},
				"includeContent": true,
				"budgetChars":    220,
				"limit":          2,
			},
		},
	}
	resp, err := tool(ctx, req)
	require.NoError(t, err)
	require.Len(t, resp.Content, 1)

	var parsed FilesResponse
	require.NoError(t, json.Unmarshal([]byte(resp.Content[0].(mcp.TextContent).Text), &parsed))
	require.Len(t, parsed.Files, 1)
	require.False(t, parsed.Files[0].ContentTruncated)
	assert.Equal(t, "src/a.go", parsed.Files[0].Path)
	assert.NotEmpty(t, parsed.ContinuationToken)

	req2 := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "files",
			Arguments: map[string]interface{}{
				"continuationToken": parsed.ContinuationToken,
			},
		},
	}
	resp2, err := tool(ctx, req2)
	require.NoError(t, err)
	require.Len(t, resp2.Content, 1)
	var page2 FilesResponse
	require.NoError(t, json.Unmarshal([]byte(resp2.Content[0].(mcp.TextContent).Text), &page2))
	require.Len(t, page2.Files, 1)
	require.False(t, page2.Files[0].ContentTruncated)
	assert.Equal(t, "src/b.go", page2.Files[0].Path)

	assert.Empty(t, page2.ContinuationToken)
}

func TestFilesToolContinuationTokenPagesResults(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "vault")
	require.NoError(t, os.MkdirAll(vaultPath, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "a.md"), []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "b.md"), []byte("b"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "c.md"), []byte("c"), 0o644))

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

	cfg := Config{
		Vault:     &obsidian.Vault{Name: "vault"},
		VaultPath: vaultPath,
		VaultDef:  obsidian.VaultDefinition{Name: "vault", Path: vaultPath},
	}

	tool := FilesTool(cfg)
	req1 := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "files",
			Arguments: map[string]interface{}{
				"inputs":         []interface{}{"find:*.md"},
				"includeContent": false,
				"limit":          2,
			},
		},
	}
	resp1, err := tool(ctx, req1)
	require.NoError(t, err)
	require.NotNil(t, resp1)
	require.Len(t, resp1.Content, 1)

	text1, ok := resp1.Content[0].(mcp.TextContent)
	require.True(t, ok)
	var page1 FilesResponse
	require.NoError(t, json.Unmarshal([]byte(text1.Text), &page1))
	require.Len(t, page1.Files, 2)
	require.NotEmpty(t, page1.ContinuationToken)
	require.Equal(t, 3, page1.Total)
	require.Equal(t, 2, page1.Returned)

	req2 := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "files",
			Arguments: map[string]interface{}{
				"continuationToken": page1.ContinuationToken,
			},
		},
	}
	resp2, err := tool(ctx, req2)
	require.NoError(t, err)
	require.NotNil(t, resp2)
	require.Len(t, resp2.Content, 1)

	text2, ok := resp2.Content[0].(mcp.TextContent)
	require.True(t, ok)
	var page2 FilesResponse
	require.NoError(t, json.Unmarshal([]byte(text2.Text), &page2))
	require.Len(t, page2.Files, 1)
	require.Empty(t, page2.ContinuationToken)
	require.Equal(t, 3, page2.Total)
	require.Equal(t, 1, page2.Returned)
}

func TestFilesToolUnifiedExpansion(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "vault")
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "src"), 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "src", "main.go"), []byte("package main\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "note.md"), []byte("content"), 0o644))

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

	storePath := filepath.Join(tempDir, "index.sqlite")
	store, err := sqlitefixture.Open(storePath)
	require.NoError(t, err)
	defer store.Close()

	require.NoError(t, store.ReplaceFileSummary(ctx, codeanchor.FileSummary{
		FilePath: "src/main.go",
		Lang:     codeanchor.LangGo,
		Hash:     "abc123",
	}))
	require.NoError(t, store.ReplaceDocLinksForPath(ctx, "src/main.go", []codeanchor.DocLink{
		{
			SrcType: "code",
			SrcPath: "src/main.go",
			DstKind: "note",
			DstPath: "note.md",
		},
	}))

	cfg := Config{
		Vault:      &obsidian.Vault{Name: "vault"},
		VaultPath:  vaultPath,
		VaultDef:   obsidian.VaultDefinition{Name: "vault", Path: vaultPath},
		IntelStore: store,
	}

	tool := FilesTool(cfg)
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "files",
			Arguments: map[string]interface{}{
				"inputs":         []interface{}{"note.md"},
				"maxDepth":       float64(1),
				"includeContent": false,
			},
		},
	}

	resp, err := tool(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Content, 1)

	text, ok := resp.Content[0].(mcp.TextContent)
	require.True(t, ok)

	var parsed FilesResponse
	require.NoError(t, json.Unmarshal([]byte(text.Text), &parsed))

	foundCode := false
	for _, entry := range parsed.Files {
		if entry.Path == "src/main.go" && entry.FileType == fileTypeCode {
			foundCode = true
			break
		}
	}
	assert.True(t, foundCode, "expected code file via unified graph expansion")
}

func TestFilesToolGraphExpansionUsesTypedRelations(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "vault")
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "docs"), 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "docs", "effort.md"), []byte("# Effort\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "docs", "spec.md"), []byte("# Spec\n"), 0o644))
	// Misleading suffixes: endpoint kind, not extension, decides the file type.
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "Notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "Notes", "Decision.HTML"), []byte("<p>Decision</p>"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "Notes", "Decision.MD"), []byte("# not a note\n"), 0o644))

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

	storePath := filepath.Join(tempDir, "index.sqlite")
	store, err := sqlitefixture.Open(storePath)
	require.NoError(t, err)
	defer store.Close()

	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "docs/effort.md", RelationName: "frozenSpecs", DstPath: "docs/spec.md", DstType: "TechnicalSpec", Provenance: "field", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "notes", LoadedAt: 1, Ready: true},
	}))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "docs/effort.md", semdb.GraphDocEdgeKindWikilink, []string{"Notes/Decision.HTML"}))
	require.NoError(t, store.ReplaceDocLinksForPath(ctx, "Notes/Decision.MD", []codeanchor.DocLink{{
		SrcType: "code", SrcPath: "Notes/Decision.MD", DstKind: "note", DstPath: "docs/effort.md",
	}}))

	cfg := Config{
		Vault:        &obsidian.Vault{Name: "vault"},
		VaultPath:    vaultPath,
		VaultDef:     obsidian.VaultDefinition{Name: "vault", Path: vaultPath},
		IntelStore:   store,
		NoteMetadata: configuredMCPNoteMetadata(t),
	}

	tool := FilesTool(cfg)
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "files",
			Arguments: map[string]interface{}{
				"inputs":         []interface{}{"docs/effort.md"},
				"maxDepth":       float64(1),
				"includeContent": false,
			},
		},
	}

	resp, err := tool(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Content, 1)

	text, ok := resp.Content[0].(mcp.TextContent)
	require.True(t, ok)

	var parsed FilesResponse
	require.NoError(t, json.Unmarshal([]byte(text.Text), &parsed))
	require.ElementsMatch(t, []string{"docs/effort.md", "docs/spec.md", "Notes/Decision.HTML", "Notes/Decision.MD"}, fileEntryPaths(parsed.Files))
	fileTypes := map[string]string{}
	for _, entry := range parsed.Files {
		fileTypes[entry.Path] = entry.FileType
	}
	require.Equal(t, fileTypeNote, fileTypes["Notes/Decision.HTML"])
	require.Equal(t, fileTypeCode, fileTypes["Notes/Decision.MD"])
	require.Equal(t, fileTypeNote, fileTypes["docs/spec.md"])
}

func TestFilesToolProjectedNoteSurfaceUsesRuntimeCapabilities(t *testing.T) {
	configured := Config{NoteMetadata: configuredMCPNoteMetadata(t)}
	require.True(t, supportsMCPProjectedNoteFileSurface(configured, "Notes/Decision.MD"))
	require.True(t, supportsMCPProjectedNoteFileSurface(configured, "Notes/Decision.HTML"))

	future := fileSurfaceTestProvider{descriptor: noteformat.Descriptor{
		ID:                "future-text",
		Extensions:        []string{".txt"},
		ProviderVersion:   "future-v1",
		ProjectionVersion: "future-projection-v1",
		OwnershipPolicy:   noteformat.OwnershipExplicitInclude,
		Capabilities:      noteformat.MustCapabilities(noteformat.CapabilitySourceReading),
	}}
	registry, err := noteformat.NewRegistry(future)
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, future)
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	require.True(t, supportsMCPProjectedNoteFileSurface(Config{NoteMetadata: indexer}, "Notes/Decision.TXT"))
}

func TestReadMCPNoteFileContentReadsProjectedNonMarkdownSource(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "Notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Notes", "Decision.TXT"), []byte("future content"), 0o644))
	vaultPaths, err := paths.NewVaultPaths(root)
	require.NoError(t, err)

	content, err := readMCPNoteFileContent(vaultPaths, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, "Notes/Decision.TXT", false)
	require.NoError(t, err)
	require.Equal(t, "future content", content)
}

func configuredMCPNoteMetadata(t *testing.T) notemeta.Indexer {
	t.Helper()
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	return indexer
}

type fileSurfaceTestProvider struct {
	descriptor noteformat.Descriptor
}

func (p fileSurfaceTestProvider) Descriptor() noteformat.Descriptor {
	return p.descriptor
}

func (p fileSurfaceTestProvider) Project(noteformat.AuthoredSource) (noteformat.Projection, error) {
	return noteformat.Projection{}, nil
}

func fileEntryPaths(entries []FileEntry) []string {
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.Path)
	}
	return paths
}

// newCodeFilesConfig builds a vault containing the given code files, each
// registered in a real SQLite file-summary index.
func newCodeFilesConfig(t *testing.T, files map[string]string) Config {
	t.Helper()
	ctx := context.Background()
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "vault")
	require.NoError(t, os.MkdirAll(vaultPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "note.md"), []byte("content"), 0o644))

	obsidianConfig := filepath.Join(tempDir, "obsidian.json")
	cfgBody, err := json.Marshal(map[string]any{"vaults": map[string]any{"vault": map[string]string{"path": vaultPath}}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(obsidianConfig, cfgBody, 0o644))
	origConfig := obsidian.ObsidianConfigFile
	obsidian.ObsidianConfigFile = func() (string, error) { return obsidianConfig, nil }
	t.Cleanup(func() { obsidian.ObsidianConfigFile = origConfig })

	store := newIntelStore(t)
	for rel, body := range files {
		full := filepath.Join(vaultPath, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
		require.NoError(t, store.ReplaceFileSummary(ctx, codeanchor.FileSummary{FilePath: rel, Lang: codeanchor.LangGo, Hash: "hash-" + rel}))
	}
	return Config{
		Vault:        &obsidian.Vault{Name: "vault"},
		VaultPath:    vaultPath,
		VaultDef:     obsidian.VaultDefinition{Name: "vault", Path: vaultPath},
		IntelStore:   store,
		NoteMetadata: configuredMCPNoteMetadata(t),
	}
}

func callFilesTool(t *testing.T, cfg Config, args map[string]any) FilesResponse {
	t.Helper()
	resp, err := FilesTool(cfg)(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "files", Arguments: args}})
	require.NoError(t, err)
	require.False(t, resp.IsError)
	require.Len(t, resp.Content, 1)
	var parsed FilesResponse
	require.NoError(t, json.Unmarshal([]byte(resp.Content[0].(mcp.TextContent).Text), &parsed))
	return parsed
}

func TestFilesToolIncludesFullCodeContentWithoutBudget(t *testing.T) {
	codeBody := strings.Repeat("b", 1000)
	cfg := newCodeFilesConfig(t, map[string]string{"src/main.go": codeBody})

	t.Run("metadata only", func(t *testing.T) {
		parsed := callFilesTool(t, cfg, map[string]any{"inputs": []any{"src/main.go"}, "includeContent": false})
		require.Len(t, parsed.Files, 1)
		entry := parsed.Files[0]
		assert.Equal(t, "src/main.go", entry.Path)
		assert.Equal(t, fileTypeCode, entry.FileType)
		assert.Empty(t, entry.Content)
		assert.False(t, entry.ContentTruncated)
	})
	t.Run("full content", func(t *testing.T) {
		parsed := callFilesTool(t, cfg, map[string]any{"inputs": []any{"src/main.go"}, "includeContent": true})
		require.Len(t, parsed.Files, 1)
		entry := parsed.Files[0]
		assert.Equal(t, "src/main.go", entry.Path)
		assert.Equal(t, fileTypeCode, entry.FileType)
		assert.False(t, entry.ContentTruncated)
		assert.Equal(t, codeBody, entry.Content)
	})
}

func TestFilesToolBudgetTruncatesSingleLargeFile(t *testing.T) {
	cfg := newCodeFilesConfig(t, map[string]string{"src/big.go": strings.Repeat("x", 1000)})
	parsed := callFilesTool(t, cfg, map[string]any{
		"inputs": []any{"src/big.go"}, "includeContent": true, "budgetChars": 200, "limit": 1,
	})
	require.Len(t, parsed.Files, 1)
	entry := parsed.Files[0]
	assert.Equal(t, "src/big.go", entry.Path)
	assert.Equal(t, fileTypeCode, entry.FileType)
	assert.True(t, entry.ContentTruncated)
	assert.NotEmpty(t, entry.Content)
	assert.True(t, strings.HasPrefix(strings.Repeat("x", 1000), entry.Content))
	assert.LessOrEqual(t, len(entry.Content), 200)
	assert.Empty(t, parsed.ContinuationToken)
}
