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
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestSemanticQueryControlsPersistAcrossContinuationAndCompactEveryRepresentation(t *testing.T) {
	vaultPath := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "pkg", "first.go"), []byte("package pkg\n\nfunc Run() {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "pkg", "second.go"), []byte("package pkg\n\nfunc Run() {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "pkg", "third_test.go"), []byte("package pkg\n\nfunc TestRun() {}\n"), 0o644))

	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store := newIntelStore(t)
	addAnchor(t, store, provider, "pkg/first.go", "Run", "pkg.Service.Run", "anchor-first", "chunk-first")
	addAnchor(t, store, provider, "pkg/second.go", "Run", "pkg.Service.Run", "anchor-second", "chunk-second")
	addAnchor(t, store, provider, "pkg/third_test.go", "Run", "pkg.Service.Run", "anchor-test", "chunk-test")
	addDocSection(t, store, provider, "Notes/Run.md", "Run", "pkg.Service.Run", "run-note", "run-note-chunk")
	addCurrentNoteOwnership(t, store, "Notes/Run.md")

	tool := SemanticQueryTool(Config{
		VaultPath:         vaultPath,
		VaultDef:          obsidian.VaultDefinition{Path: vaultPath},
		IntelStore:        store,
		EmbeddingsOn:      true,
		EmbedProvider:     provider,
		CodeEmbeddingsOn:  true,
		CodeEmbedProvider: provider,
	})
	first := callSemanticQuery(t, tool, map[string]any{
		"queries":            []any{"pkg.Service.Run"},
		"scope":              "code",
		"requireExactSymbol": true,
		"includeTests":       false,
		"compact":            true,
		"limit":              1,
	})
	require.NotEmpty(t, first.ContinuationToken)
	assertCompactCodeOnly(t, first)

	second := callSemanticQuery(t, tool, map[string]any{
		"queries":            []any{"pkg.Service.Run"},
		"scope":              "code",
		"requireExactSymbol": true,
		"includeTests":       false,
		"compact":            true,
		"limit":              1,
		"continuationToken":  first.ContinuationToken,
	})
	assertCompactCodeOnly(t, second)
	require.NotEmpty(t, first.Compact.Sources)
	require.NotEmpty(t, second.Compact.Sources)
	require.NotEqual(t, first.Compact.Sources[0].Ref, second.Compact.Sources[0].Ref)

	detailed := callSemanticQuery(t, tool, map[string]any{"queries": []any{"pkg.Service.Run"}, "scope": "code", "requireExactSymbol": true, "includeTests": false})
	require.Nil(t, detailed.Compact)
	require.NotEmpty(t, detailed.Matches)
	require.NotEmpty(t, detailed.Text)
	require.NotContains(t, detailed.Text, "third_test.go")
	require.NotContains(t, detailed.Text, "Notes/Run.md")
	for _, item := range append(detailed.MustRead, detailed.Supporting...) {
		require.NotContains(t, item.Path, "third_test.go")
		require.NotContains(t, item.Path, "Notes/Run.md")
	}
	direct, err := SemanticQueryUnifiedWithOptions(context.Background(), Config{
		VaultPath: vaultPath, VaultDef: obsidian.VaultDefinition{Path: vaultPath}, IntelStore: store,
		EmbeddingsOn: true, EmbedProvider: provider, CodeEmbeddingsOn: true, CodeEmbedProvider: provider,
	}, SemanticQueryOptions{Profile: searchapplication.ProfileAgent, Query: "pkg.Service.Run", Scope: "code", Limit: 20, BudgetChars: DefaultBudgetChars(), RequireExactSymbol: true, ExcludeTests: true})
	require.NoError(t, err)
	require.Equal(t, semanticMatchPaths(direct.Matches), semanticMatchPaths(detailed.Matches))

	multiple := callSemanticQuery(t, tool, map[string]any{"queries": []any{map[string]any{"text": "pkg.Service.Run", "mode": "search"}, map[string]any{"text": "Run", "mode": "search"}}, "scope": "code", "includeTests": false, "compact": true, "limit": 1})
	require.NotEmpty(t, multiple.ContinuationToken)
	assertCompactCodeOnly(t, multiple)
	require.NotEmpty(t, multiple.Lanes, "merged compact responses retain per-query lane state")
	require.NotEmpty(t, append(multiple.Compact.Roles.MustRead, multiple.Compact.Roles.Supporting...), "merged compact responses retain answer roles")
	more := callSemanticQuery(t, tool, map[string]any{"queries": []any{map[string]any{"text": "pkg.Service.Run", "mode": "search"}, map[string]any{"text": "Run", "mode": "search"}}, "scope": "code", "includeTests": false, "compact": true, "limit": 1, "continuationToken": multiple.ContinuationToken})
	assertCompactCodeOnly(t, more)
	require.NotEmpty(t, more.Compact.Sources)

	res, err := tool(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "semantic_query",
		Arguments: map[string]any{
			"queries":           []any{"pkg.Service.Run"},
			"continuationToken": first.ContinuationToken,
			"scope":             "docs",
		},
	}})
	require.NoError(t, err)
	require.True(t, res.IsError)
}

func TestSemanticQueryNoteTypeExcludesPromotedCodeResults(t *testing.T) {
	vaultPath := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "pkg"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "Notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "pkg", "service.go"), []byte("package pkg\n\nfunc Run() {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "Notes", "Run.md"), []byte("# Run\n\nRun the project.\n"), 0o644))

	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store := newIntelStore(t)
	addAnchor(t, store, provider, "pkg/service.go", "Run", "pkg.Service.Run", "anchor-run", "chunk-run")
	addDocSection(t, store, provider, "Notes/Run.md", "Run", "Run the project", "run-note", "run-note-chunk")
	addCurrentNoteOwnership(t, store, "Notes/Run.md")
	require.NoError(t, store.UpsertOntologyTypes(context.Background(), []semdb.OntologyNoteTypeRow{{
		NotePath: "Notes/Run.md",
		TypeName: "Project",
	}}))

	response, err := SemanticQueryUnifiedWithOptions(context.Background(), Config{
		VaultPath:         vaultPath,
		VaultDef:          obsidian.VaultDefinition{Path: vaultPath},
		IntelStore:        store,
		EmbeddingsOn:      true,
		EmbedProvider:     provider,
		CodeEmbeddingsOn:  true,
		CodeEmbedProvider: provider,
	}, SemanticQueryOptions{Query: "Run", NoteType: "Project"})
	require.NoError(t, err)
	require.NotEmpty(t, response.Matches)
	for _, match := range response.Matches {
		require.Equal(t, "note", match.Type)
		require.Equal(t, "Project", match.NoteType)
	}
}

func TestSemanticQueryNoteTypeExcludesExplicitSeedsWithAnotherOwningType(t *testing.T) {
	vaultPath := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "Notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "Notes", "Reference.md"), []byte("# Reference\n\nSearch reference.\n"), 0o644))

	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store := newIntelStore(t)
	addDocSection(t, store, provider, "Notes/Reference.md", "Reference", "Search reference", "reference-note", "reference-note-chunk")
	addCurrentNoteOwnership(t, store, "Notes/Reference.md")
	require.NoError(t, store.UpsertOntologyTypes(context.Background(), []semdb.OntologyNoteTypeRow{{
		NotePath: "Notes/Reference.md",
		TypeName: "Reference",
	}}))

	cfg := Config{
		VaultPath:     vaultPath,
		VaultDef:      obsidian.VaultDefinition{Path: vaultPath},
		IntelStore:    store,
		EmbeddingsOn:  true,
		EmbedProvider: provider,
	}
	request := SemanticQueryOptions{
		Query:      "search subsystem overview",
		SeedTokens: []string{"Notes/Reference.md"},
		Mode:       string(search.IntentSubsystemOverview),
		NoteType:   "Reference",
	}
	// Positive control: the same seed survives when its owning type matches.
	response, err := SemanticQueryUnifiedWithOptions(context.Background(), cfg, request)
	require.NoError(t, err)
	require.Equal(t, []string{"Notes/Reference.md"}, semanticMatchPaths(response.Matches))
	require.Equal(t, "Reference", response.Matches[0].NoteType)

	request.NoteType = "Project"
	response, err = SemanticQueryUnifiedWithOptions(context.Background(), cfg, request)
	require.NoError(t, err)
	require.Empty(t, response.Matches)
}

func callSemanticQuery(t *testing.T, tool func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) semanticQueryResponse {
	t.Helper()
	res, err := tool(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "semantic_query", Arguments: args}})
	require.NoError(t, err)
	require.False(t, res.IsError, "%+v", res.Content)
	text, ok := res.Content[0].(mcp.TextContent)
	require.True(t, ok)
	var payload semanticQueryResponse
	require.NoError(t, json.Unmarshal([]byte(text.Text), &payload))
	return payload
}

func semanticMatchPaths(matches []SemanticMatchPayload) []string {
	out := make([]string, len(matches))
	for i, match := range matches {
		out[i] = match.Path
	}
	return out
}

func assertCompactCodeOnly(t *testing.T, payload semanticQueryResponse) {
	t.Helper()
	require.NotNil(t, payload.Compact)
	require.Empty(t, payload.Text)
	require.Empty(t, payload.Matches)
	for _, source := range payload.Compact.Sources {
		require.Equal(t, "code", source.Type)
		require.NotContains(t, source.Path, "_test.go")
	}
}

func TestCompactSessionHandlerDedupesPrefixAndAllowsLargerBody(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0755))
	body := "package pkg\n\nfunc Run() {\n" + strings.Repeat("    println(\"source evidence for the selected function\")\n", 90) + "}\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg/service.go"), []byte(body), 0644))
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store := newIntelStore(t)
	addAnchor(t, store, provider, "pkg/service.go", "Run", "pkg.Service.Run", "anchor-run", "chunk-run")
	require.NoError(t, store.ReplaceIntelCodeFile(context.Background(), "pkg/service.go", []codeanchor.IntelAnchor{{AnchorID: "anchor-run", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/service.go", Symbol: "Run", FQN: "pkg.Service.Run", Signature: "func Run()", StartLine: 3, EndLine: 94, Fingerprint: "fp-code"}}, nil, nil))
	cfg := Config{VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, IntelStore: store, SessionStore: store, EmbeddingsOn: true, EmbedProvider: provider, CodeEmbeddingsOn: true, CodeEmbedProvider: provider}
	call := func(budget int) semanticQueryResponse {
		cfg.BudgetCharsOverride = budget
		return callSemanticQuery(t, SemanticQueryTool(cfg), map[string]any{"queries": []any{map[string]any{"text": "pkg.Service.Run", "mode": "go_to_def"}}, "scope": "code", "compact": true, "sessionId": "handler-prefix"})
	}
	first := call(6000)
	require.Len(t, first.Compact.Sources, 1)
	require.NotNil(t, first.Compact.Sources[0].Body)
	require.NotEmpty(t, first.Compact.Sources[0].Body.Content)
	require.True(t, first.Compact.Sources[0].Body.Truncated)
	second := call(6000)
	require.Len(t, second.Compact.Sources, 1)
	require.Empty(t, second.Compact.Sources[0].Body.Content)
	require.True(t, second.Compact.Sources[0].Body.Deduped)
	require.Positive(t, second.DedupeHits)
	larger := call(15000)
	require.Len(t, larger.Compact.Sources, 1)
	require.False(t, larger.Compact.Sources[0].Body.Deduped)
	require.False(t, larger.Compact.Sources[0].Body.Truncated)
	require.Equal(t, strings.TrimSpace(body), larger.Compact.Sources[0].Body.Content)
	repeated := call(15000)
	require.Empty(t, repeated.Compact.Sources[0].Body.Content)
	require.True(t, repeated.Compact.Sources[0].Body.Deduped)

	cfg.BudgetCharsOverride = 15000
	ordinary := SemanticQueryTool(cfg)
	args := map[string]any{"queries": []any{"pkg.Service.Run"}, "mode": "go_to_def", "scope": "code", "compact": true, "sessionId": "ordinary-session"}
	ordinaryFirst := callSemanticQuery(t, ordinary, args)
	require.Len(t, ordinaryFirst.Compact.Sources, 1)
	require.NotEmpty(t, ordinaryFirst.Compact.Sources[0].Body.Content)
	ordinaryAgain := callSemanticQuery(t, ordinary, args)
	require.Len(t, ordinaryAgain.Compact.Sources, 1)
	require.Empty(t, ordinaryAgain.Compact.Sources[0].Body.Content)
	require.True(t, ordinaryAgain.Compact.Sources[0].Body.Deduped)
}
