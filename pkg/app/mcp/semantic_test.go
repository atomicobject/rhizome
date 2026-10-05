package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSemanticQueryMatchesWithDeterministicProvider(t *testing.T) {
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	vaultDir := t.TempDir()
	intelStore := newIntelStore(t)
	addDocSection(t, intelStore, prov, "Alpha.md", "Alpha", "kickoff project", "section1", "chunk1")

	cfg := Config{
		VaultPath:     vaultDir,
		VaultDef:      obsidian.VaultDefinition{Path: vaultDir},
		EmbedProvider: prov,
		EmbeddingsOn:  true,
		IntelStore:    intelStore,
	}

	tool := SemanticQueryTool(cfg)
	res, err := tool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "semantic_query",
			Arguments: map[string]any{"queries": []string{"kickoff project"}, "limit": 10},
		},
	})
	assert.NoError(t, err)
	assert.NotNil(t, res)
	assert.False(t, res.IsError)
	assert.Len(t, res.Content, 1)

	foundAlpha := false
	var payload struct {
		Summary    string                 `json:"summary"`
		MustRead   []map[string]any       `json:"mustRead"`
		Confidence map[string]any         `json:"confidence"`
		Coverage   map[string]any         `json:"coverage"`
		Matches    []SemanticMatchPayload `json:"matches"`
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	assert.True(t, ok)
	assert.NoError(t, json.Unmarshal([]byte(tc.Text), &payload))
	assert.NotEmpty(t, payload.Matches)
	assert.NotEmpty(t, payload.Summary)
	assert.NotEmpty(t, payload.MustRead)
	assert.NotEmpty(t, payload.Confidence)
	assert.NotEmpty(t, payload.Coverage)

	for _, r := range payload.Matches {
		if r.Path == "Alpha.md" && r.Type == "note" {
			foundAlpha = true
			break
		}
	}
	assert.True(t, foundAlpha, "expected Alpha.md in semantic matches")
}

func TestSemanticQueryContinuationPagesAreDisjointAndRequireTheSameRequest(t *testing.T) {
	ctx := context.Background()
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	vaultDir := t.TempDir()
	intelStore := newIntelStore(t)
	anchor := codeanchor.IntelAnchor{
		AnchorID: "python-go", Lang: codeanchor.LangPy, Kind: "function", Path: "src/go.py",
		Symbol: "Go", FQN: "todo.Go", Signature: "def Go():", StartLine: 1, EndLine: 2, Fingerprint: "python-go-fp",
	}
	module := codeanchor.IntelAnchor{
		AnchorID: "python-go-module", Lang: codeanchor.LangPy, Kind: "module", Path: "src/go.py",
		Symbol: "go.py", StartLine: 1, EndLine: 2, Fingerprint: "python-go-module-fp",
	}
	other := codeanchor.IntelAnchor{
		AnchorID: "python-going", Lang: codeanchor.LangPy, Kind: "function", Path: "src/going.py",
		Symbol: "Going", FQN: "todo.Going", Signature: "def Going():", StartLine: 1, EndLine: 2, Fingerprint: "python-going-fp",
	}
	require.NoError(t, intelStore.ReplaceIntelCodeFile(ctx, anchor.Path, []codeanchor.IntelAnchor{module, anchor}, nil, nil))
	require.NoError(t, intelStore.ReplaceIntelCodeFile(ctx, other.Path, []codeanchor.IntelAnchor{other}, nil, nil))
	require.NoError(t, intelStore.ReplaceIntelChunks(ctx, []string{anchor.AnchorID, other.AnchorID}, []codeanchor.IntelChunk{
		{ChunkID: "python-go-chunk", OwnerID: anchor.AnchorID, OwnerType: "anchor", Granularity: "symbol", ContentHash: "python-go-content"},
		{ChunkID: "python-going-chunk", OwnerID: other.AnchorID, OwnerType: "anchor", Granularity: "symbol", ContentHash: "python-going-content"},
	}))
	require.NoError(t, intelStore.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{
		"python-go-chunk": embedSingle(t, prov, "Go"), "python-going-chunk": embedSingle(t, prov, "Gos"),
	}))
	callerOne := codeanchor.IntelAnchor{AnchorID: "caller-one", Lang: codeanchor.LangPy, Kind: "function", Path: "src/one.py", Symbol: "one", FQN: "todo.one", Fingerprint: "one-fp"}
	callerTwo := codeanchor.IntelAnchor{AnchorID: "caller-two", Lang: codeanchor.LangPy, Kind: "function", Path: "src/two.py", Symbol: "two", FQN: "todo.two", Fingerprint: "two-fp"}
	require.NoError(t, intelStore.ReplaceIntelCodeFile(ctx, callerOne.Path, []codeanchor.IntelAnchor{callerOne}, []codeanchor.IntelEdge{{SrcID: callerOne.AnchorID, DstID: anchor.AnchorID, Kind: "calls"}}, nil))
	require.NoError(t, intelStore.ReplaceIntelCodeFile(ctx, callerTwo.Path, []codeanchor.IntelAnchor{callerTwo}, []codeanchor.IntelEdge{{SrcID: callerTwo.AnchorID, DstID: anchor.AnchorID, Kind: "calls"}}, nil))
	cfg := Config{
		VaultPath: vaultDir, VaultDef: obsidian.VaultDefinition{Path: vaultDir}, IntelStore: intelStore,
		EmbedProvider: prov, CodeEmbedProvider: prov, EmbeddingsOn: true, CodeEmbeddingsOn: true,
	}

	definitionRequest := SemanticQueryOptions{
		Profile: searchapplication.ProfileInteractive, Query: "Go", Mode: string(search.IntentGoToDef), Types: []string{"code"}, Limit: 1, BudgetChars: 4000,
	}
	definition, err := SemanticQueryUnifiedWithOptions(ctx, cfg, definitionRequest)
	require.NoError(t, err)
	require.Equal(t, search.TargetStatusInferredSymbol, definition.TargetStatus)
	require.NotEmpty(t, definition.MustRead)
	require.Equal(t, "todo.Go", definition.MustRead[0].FQN)

	request := SemanticQueryOptions{
		Profile: searchapplication.ProfileInteractive, Query: "Go", Mode: string(search.IntentCallers), Types: []string{"code"}, Limit: 1, BudgetChars: 4000,
	}
	first, err := SemanticQueryUnifiedWithOptions(ctx, cfg, request)
	require.NoError(t, err)
	require.Equal(t, search.TargetStatusInferredSymbol, first.TargetStatus)
	require.NotEmpty(t, first.Matches)
	require.NotEmpty(t, first.ContinuationToken)

	// Continuation requests repeat the same request; pages stay disjoint.
	resumed := request
	resumed.ContinuationToken = first.ContinuationToken
	second, err := SemanticQueryUnifiedWithOptions(ctx, cfg, resumed)
	require.NoError(t, err)
	require.Equal(t, search.TargetStatusInferredSymbol, second.TargetStatus)
	require.NotEmpty(t, second.Matches)
	require.NotEqual(t, first.Matches[0].FQN, second.Matches[0].FQN)

	// A changed control invalidates the token instead of silently repaging.
	changed := resumed
	changed.Scope = "docs"
	_, err = SemanticQueryUnifiedWithOptions(ctx, cfg, changed)
	require.ErrorContains(t, err, "invalid continuationToken:")
	require.True(t, errors.Is(err, unifiedsearch.ErrCursorInvalid))
}

func TestSemanticQueryTimingsAreOptInAndMachineReadable(t *testing.T) {
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	vaultDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "Alpha.md"), []byte("# Alpha\n\nKickoff project context."), 0o644))
	intelStore := newIntelStore(t)
	addDocSection(t, intelStore, prov, "Alpha.md", "Alpha", "kickoff project", "section1", "chunk1")
	cfg := Config{
		VaultPath: vaultDir, VaultDef: obsidian.VaultDefinition{Path: vaultDir},
		EmbedProvider: prov, EmbeddingsOn: true, IntelStore: intelStore, SessionStore: intelStore,
	}
	tool := SemanticQueryTool(cfg)
	call := func(timings bool) map[string]any {
		res, err := tool(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
			Name: "semantic_query", Arguments: map[string]any{
				"queries": []string{"kickoff project"}, "limit": 10, "timings": timings,
			},
		}})
		require.NoError(t, err)
		require.False(t, res.IsError)
		content, ok := res.Content[0].(mcp.TextContent)
		require.True(t, ok)
		var payload map[string]any
		require.NoError(t, json.Unmarshal([]byte(content.Text), &payload))
		return payload
	}

	withoutTimings := call(false)
	withTimings := call(true)
	require.NotContains(t, withoutTimings, "diagnostics")
	diagnostics, ok := withTimings["diagnostics"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "request", diagnostics["measurementScope"])
	require.NotEmpty(t, diagnostics["phases"])
	require.NotEmpty(t, diagnostics["operations"])
	require.NotEmpty(t, diagnostics["search"])
	operations, ok := diagnostics["operations"].([]any)
	require.True(t, ok)
	noteReads := float64(0)
	for _, raw := range operations {
		operation, ok := raw.(map[string]any)
		if ok && operation["label"] == "semantic_query.note.reads" {
			noteReads, _ = operation["count"].(float64)
			break
		}
	}
	require.Positive(t, noteReads)

	delete(withTimings, "diagnostics")
	require.Equal(t, withoutTimings, withTimings,
		"timing observations must not change search membership, order, roles, warnings, confidence, or continuation")
}

func TestSemanticQueryIndexedReadOnlyMissingStoreReturnsStructuredRemediationBeforeProviderError(t *testing.T) {
	tool := SemanticQueryTool(Config{VaultPath: t.TempDir(), IndexedReadOnlySemanticQuery: true})
	res, err := tool(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "semantic_query", Arguments: map[string]any{"queries": []string{"indexing pipeline"}},
	}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	content, ok := res.Content[0].(mcp.TextContent)
	require.True(t, ok)
	var payload semanticQueryResponse
	require.NoError(t, json.Unmarshal([]byte(content.Text), &payload))
	require.Len(t, payload.Warnings, 1)
	require.Equal(t, "indexed-context-missing", payload.Warnings[0].Code)
	require.Contains(t, payload.Text, "rzm index")
}

func TestSemanticQueryDisabledSkips(t *testing.T) {
	vaultPath := t.TempDir()
	cfg := Config{
		VaultPath:  vaultPath,
		VaultDef:   obsidian.VaultDefinition{Path: vaultPath},
		IntelStore: newIntelStore(t),
	}
	tool := SemanticQueryTool(cfg)
	res, err := tool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "semantic_query",
			Arguments: map[string]any{"queries": []string{"kickoff project"}, "limit": 5},
		},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Equal(t, "semantic search unavailable: embedding provider is not configured; run `rzm index`", res.Content[0].(mcp.TextContent).Text)
}

func TestSemanticQueryIncludesCodeMatches(t *testing.T) {
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	vaultPath := t.TempDir()
	intelStore := newIntelStore(t)
	addAnchor(t, intelStore, prov, "code/main.go", "DoThing", "DoThing", "anchor1", "chunk1")
	addAnchor(t, intelStore, prov, "code/helper.go", "DoThingHelper", "DoThingHelper", "anchor2", "chunk2")

	cfg := Config{
		VaultPath:         vaultPath,
		VaultDef:          obsidian.VaultDefinition{Path: vaultPath},
		EmbedProvider:     prov,
		EmbeddingsOn:      true,
		CodeEmbeddingsOn:  true,
		CodeEmbedProvider: prov,
		IntelStore:        intelStore,
	}

	tool := SemanticQueryTool(cfg)
	res, err := tool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "semantic_query",
			Arguments: map[string]any{"queries": []string{"DoThing"}, "limit": 3},
		},
	})
	assert.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Len(t, res.Content, 1)

	tc, ok := res.Content[0].(mcp.TextContent)
	assert.True(t, ok)
	var payload struct {
		Matches []SemanticMatchPayload `json:"matches"`
	}
	assert.NoError(t, json.Unmarshal([]byte(tc.Text), &payload))
	assert.NotEmpty(t, payload.Matches)

	first := payload.Matches[0]
	assert.Equal(t, "code", first.Type)
	assert.Equal(t, "code/main.go", first.Path)
	assert.Equal(t, "DoThing", first.Symbol)
	assert.Equal(t, 7, first.StartLine)
	assert.Equal(t, 9, first.EndLine)
	require.NotEmpty(t, first.Symbols)
	assert.Equal(t, "DoThing", first.Symbols[0].Symbol)
	assert.Equal(t, "DoThing", first.Symbols[0].FQN)
	assert.Equal(t, 7, first.Symbols[0].StartLine)
	assert.Equal(t, "func DoThing()", first.Symbols[0].Signature)
	assert.Equal(t, "entry_point", first.Role, "the top-ranked entry-point file in a code group is labeled entry_point")
	roles := map[string]string{}
	for _, match := range payload.Matches {
		roles[match.Path] = match.Role
	}
	assert.Equal(t, "impl", roles["code/helper.go"], "non-entry code in the same group stays an implementation source")
}

func TestSemanticQueryTypeFilterNotes(t *testing.T) {
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	vaultPath := t.TempDir()
	intelStore := newIntelStore(t)
	addDocSection(t, intelStore, prov, "Notes/Search (Hub).md", "Search (Hub)", "search pipeline", "section1", "chunk1")
	addAnchor(t, intelStore, prov, "pkg/search/service.go", "Service", "Service", "anchor1", "chunk2")

	cfg := Config{
		VaultPath:         vaultPath,
		VaultDef:          obsidian.VaultDefinition{Path: vaultPath},
		EmbedProvider:     prov,
		EmbeddingsOn:      true,
		CodeEmbeddingsOn:  true,
		CodeEmbedProvider: prov,
		IntelStore:        intelStore,
	}

	tool := SemanticQueryTool(cfg)
	res, err := tool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "semantic_query",
			Arguments: map[string]any{
				"queries": []string{"search pipeline"},
				"types":   []string{"note"},
				"limit":   5,
			},
		},
	})
	assert.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Len(t, res.Content, 1)

	tc, ok := res.Content[0].(mcp.TextContent)
	assert.True(t, ok)
	var payload struct {
		Matches []SemanticMatchPayload `json:"matches"`
	}
	assert.NoError(t, json.Unmarshal([]byte(tc.Text), &payload))
	assert.NotEmpty(t, payload.Matches)
	for _, match := range payload.Matches {
		assert.Equal(t, "note", match.Type)
	}
}

func TestSemanticQueryPinnedSeedHonorsTypeFilter(t *testing.T) {
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	vaultPath := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, "pkg", "search"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultPath, "pkg", "search", "service.go"), []byte("package search\n"), 0o644))
	intelStore := newIntelStore(t)
	require.NoError(t, intelStore.UpsertFileMeta(context.Background(), codeanchor.FileMeta{Path: "pkg/search/service.go", Lang: codeanchor.LangGo, Hash: "service"}))

	cfg := Config{
		VaultPath:         vaultPath,
		VaultDef:          obsidian.VaultDefinition{Path: vaultPath},
		EmbedProvider:     prov,
		EmbeddingsOn:      true,
		CodeEmbeddingsOn:  true,
		CodeEmbedProvider: prov,
		IntelStore:        intelStore,
	}

	tool := SemanticQueryTool(cfg)
	res, err := tool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "semantic_query",
			Arguments: map[string]any{
				"queries": []string{"search subsystem overview"},
				"paths":   []string{"pkg/search/service.go"},
				"types":   []string{"note"},
				"mode":    string(search.IntentSubsystemOverview),
				"limit":   5,
			},
		},
	})
	assert.NoError(t, err)
	require.False(t, res.IsError)
	require.Len(t, res.Content, 1)

	tc, ok := res.Content[0].(mcp.TextContent)
	require.True(t, ok)
	var payload struct {
		Matches []SemanticMatchPayload `json:"matches"`
	}
	require.NoError(t, json.Unmarshal([]byte(tc.Text), &payload))
	require.Empty(t, payload.Matches)
}

func TestSemanticQueryModeMetadata(t *testing.T) {
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	vaultDir := t.TempDir()
	intelStore := newIntelStore(t)
	addDocSection(t, intelStore, prov, "Notes/Planner.md", "Planner", "docs for pkg/search/service.go", "section1", "chunk1")

	cfg := Config{
		VaultPath:     vaultDir,
		VaultDef:      obsidian.VaultDefinition{Path: vaultDir},
		EmbedProvider: prov,
		EmbeddingsOn:  true,
		IntelStore:    intelStore,
	}

	tool := SemanticQueryTool(cfg)

	decode := func(t *testing.T, res *mcp.CallToolResult) (string, string, float64) {
		t.Helper()
		require.NotNil(t, res)
		require.False(t, res.IsError)
		require.Len(t, res.Content, 1)
		tc, ok := res.Content[0].(mcp.TextContent)
		require.True(t, ok)
		var payload struct {
			ModeApplied  string  `json:"modeApplied"`
			ModeDetected string  `json:"modeDetected"`
			ModeScore    float64 `json:"modeScore"`
		}
		require.NoError(t, json.Unmarshal([]byte(tc.Text), &payload))
		return payload.ModeApplied, payload.ModeDetected, payload.ModeScore
	}

	t.Run("explicit", func(t *testing.T) {
		res, err := tool(context.Background(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "semantic_query",
				Arguments: map[string]any{
					"queries": []string{"docs for pkg/search/service.go"},
					"mode":    "docs_for_code",
					"limit":   5,
				},
			},
		})
		require.NoError(t, err)
		applied, detected, score := decode(t, res)
		assert.Equal(t, "docs_for_code", applied)
		assert.Empty(t, detected)
		assert.Equal(t, 0.0, score)
	})

	t.Run("default", func(t *testing.T) {
		res, err := tool(context.Background(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "semantic_query",
				Arguments: map[string]any{
					"queries": []string{"docs for pkg/search/service.go"},
					"limit":   5,
				},
			},
		})
		require.NoError(t, err)
		applied, detected, score := decode(t, res)
		assert.Equal(t, "docs_for_code", applied)
		assert.Equal(t, "docs_for_code", detected)
		assert.Greater(t, score, 0.0)
	})

	t.Run("infersOverviewForBroadHowQuestion", func(t *testing.T) {
		res, err := tool(context.Background(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "semantic_query",
				Arguments: map[string]any{
					"queries": []string{"how does the search architecture work"},
					"limit":   5,
				},
			},
		})
		require.NoError(t, err)
		applied, detected, score := decode(t, res)
		assert.Equal(t, "overview", applied)
		assert.Equal(t, "overview", detected)
		assert.Greater(t, score, 0.0)
	})

	t.Run("infersSubsystemOverviewFromPathLikeQuestion", func(t *testing.T) {
		require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "pkg/search"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "pkg/search/service.go"), []byte("package search\n"), 0o644))
		require.NoError(t, intelStore.UpsertFileMeta(context.Background(), codeanchor.FileMeta{Path: "pkg/search/service.go", Lang: codeanchor.LangGo, Hash: "mode-service"}))

		res, err := tool(context.Background(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "semantic_query",
				Arguments: map[string]any{
					"queries": []string{"search subsystem overview for pkg/search/service.go"},
					"limit":   5,
				},
			},
		})
		require.NoError(t, err)
		applied, detected, score := decode(t, res)
		assert.Equal(t, "subsystem_overview", applied)
		assert.Equal(t, "subsystem_overview", detected)
		assert.Greater(t, score, 0.0)
	})

	t.Run("nudgesExactCodeNavigationToCodeTools", func(t *testing.T) {
		res, err := tool(context.Background(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "semantic_query",
				Arguments: map[string]any{
					"queries": []string{"callers of Service.Search"},
					"limit":   5,
				},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, res)
		require.False(t, res.IsError)
		require.Len(t, res.Content, 1)
		tc, ok := res.Content[0].(mcp.TextContent)
		require.True(t, ok)
		var payload struct {
			Warnings    []search.Warning `json:"warnings"`
			NextQueries []struct {
				Mode  string `json:"mode"`
				Query string `json:"query"`
			} `json:"nextQueries"`
		}
		require.NoError(t, json.Unmarshal([]byte(tc.Text), &payload))
		require.NotEmpty(t, payload.Warnings)
		assert.Equal(t, "exact_code_tool_suggested", payload.Warnings[0].Code)
		require.NotEmpty(t, payload.NextQueries)
		assert.Equal(t, "code_references", payload.NextQueries[0].Mode)
		assert.Equal(t, "Service.Search", payload.NextQueries[0].Query)
	})

	for name, query := range map[string]string{
		"doesNotNudgeGenericReferenceDiscovery": "search related active specs decisions references",
		"doesNotNudgeHowQuestionAboutSymbol":    "how does MakePlan choose retrievers?",
	} {
		t.Run(name, func(t *testing.T) {
			res, err := tool(context.Background(), mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name: "semantic_query",
					Arguments: map[string]any{
						"queries": []string{query},
						"limit":   5,
					},
				},
			})
			require.NoError(t, err)
			require.NotNil(t, res)
			require.False(t, res.IsError)
			require.Len(t, res.Content, 1)
			tc, ok := res.Content[0].(mcp.TextContent)
			require.True(t, ok)
			var payload struct {
				Warnings    []search.Warning `json:"warnings"`
				NextQueries []struct {
					Mode string `json:"mode"`
				} `json:"nextQueries"`
			}
			require.NoError(t, json.Unmarshal([]byte(tc.Text), &payload))
			for _, warning := range payload.Warnings {
				assert.NotEqual(t, "exact_code_tool_suggested", warning.Code)
			}
			for _, next := range payload.NextQueries {
				assert.NotEqual(t, "code_references", next.Mode)
			}
		})
	}

	t.Run("rejectsIntentArg", func(t *testing.T) {
		res, err := tool(context.Background(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "semantic_query",
				Arguments: map[string]any{
					"queries": []string{"docs for pkg/search/service.go"},
					"intent":  "docs_for_code",
				},
			},
		})
		require.NoError(t, err)
		require.True(t, res.IsError)
		tc, ok := res.Content[0].(mcp.TextContent)
		require.True(t, ok)
		assert.Contains(t, tc.Text, "mode")
	})

	t.Run("rejectsInvalidMode", func(t *testing.T) {
		res, err := tool(context.Background(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "semantic_query",
				Arguments: map[string]any{
					"queries": []string{"docs for pkg/search/service.go"},
					"mode":    "nope",
				},
			},
		})
		require.NoError(t, err)
		require.True(t, res.IsError)
		tc, ok := res.Content[0].(mcp.TextContent)
		require.True(t, ok)
		assert.Contains(t, tc.Text, "unknown mode")
		assert.Contains(t, tc.Text, "docs_for_code")
	})
}

func TestSemanticQueryRepairsSubsystemOverviewWithoutPaths(t *testing.T) {
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	vaultDir := t.TempDir()
	intelStore := newIntelStore(t)
	addDocSection(t, intelStore, prov, "pkg/search/CONTEXT.md", "Search Context", "search subsystem context", "section1", "chunk1")
	addAnchor(t, intelStore, prov, "pkg/search/service.go", "Service", "Service", "anchor1", "chunk2")

	cfg := Config{
		VaultPath:         vaultDir,
		VaultDef:          obsidian.VaultDefinition{Path: vaultDir},
		EmbedProvider:     prov,
		EmbeddingsOn:      true,
		CodeEmbeddingsOn:  true,
		CodeEmbedProvider: prov,
		IntelStore:        intelStore,
	}

	tool := SemanticQueryTool(cfg)
	res, err := tool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "semantic_query",
			Arguments: map[string]any{
				"queries": []string{"search subsystem overview for pkg/search/service.go"},
				"mode":    "subsystem_overview",
				"limit":   5,
			},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	tc, ok := res.Content[0].(mcp.TextContent)
	require.True(t, ok)
	var payload struct {
		ModeApplied          string              `json:"modeApplied"`
		TargetStatus         search.TargetStatus `json:"targetStatus"`
		ResolutionConfidence float64             `json:"resolutionConfidence"`
		Warnings             []search.Warning    `json:"warnings"`
		Lanes                []search.LaneStatus `json:"lanes"`
	}
	require.NoError(t, json.Unmarshal([]byte(tc.Text), &payload))
	require.Equal(t, "subsystem_overview", payload.ModeApplied)
	require.Equal(t, search.TargetStatusInferredPath, payload.TargetStatus)
	require.Greater(t, payload.ResolutionConfidence, 0.0)
	require.Equal(t, search.LaneStateRan, semanticLaneStatus(payload.Lanes, "code_vector").Status)
	require.Equal(t, search.LaneStateEmpty, semanticLaneStatus(payload.Lanes, "intel_fts").Status)
}

func TestSemanticQueryDowngradesSubsystemOverviewWithoutResolvablePaths(t *testing.T) {
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	vaultDir := t.TempDir()
	intelStore := newIntelStore(t)
	addDocSection(t, intelStore, prov, "README.md", "README", "repo overview", "section1", "chunk1")

	cfg := Config{
		VaultPath:     vaultDir,
		VaultDef:      obsidian.VaultDefinition{Path: vaultDir},
		EmbedProvider: prov,
		EmbeddingsOn:  true,
		IntelStore:    intelStore,
	}

	tool := SemanticQueryTool(cfg)
	res, err := tool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "semantic_query",
			Arguments: map[string]any{
				"queries": []string{"search subsystem overview"},
				"mode":    "subsystem_overview",
				"limit":   5,
			},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	tc, ok := res.Content[0].(mcp.TextContent)
	require.True(t, ok)
	var payload struct {
		ModeApplied      string                   `json:"modeApplied"`
		TargetStatus     search.TargetStatus      `json:"targetStatus"`
		TargetCandidates []search.TargetCandidate `json:"targetCandidates"`
		Warnings         []search.Warning         `json:"warnings"`
	}
	require.NoError(t, json.Unmarshal([]byte(tc.Text), &payload))
	require.Equal(t, "overview", payload.ModeApplied)
	require.Equal(t, search.TargetStatusDowngraded, payload.TargetStatus)
	require.Empty(t, payload.TargetCandidates)
	require.NotEmpty(t, payload.Warnings)
	require.Equal(t, "subsystem_overview_downgraded", payload.Warnings[len(payload.Warnings)-1].Code)
}

func semanticLaneStatus(lanes []search.LaneStatus, lane string) search.LaneStatus {
	for _, status := range lanes {
		if status.Lane == lane {
			return status
		}
	}
	return search.LaneStatus{}
}

func TestSemanticQueryBlocksPrecisionFallbackWhenSymbolIsAmbiguous(t *testing.T) {
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	vaultDir := t.TempDir()
	intelStore := newIntelStore(t)
	addAnchor(t, intelStore, prov, "pkg/search/service.go", "Service", "pkg.search.Service", "anchor1", "chunk1")
	addAnchor(t, intelStore, prov, "pkg/cache/service.go", "Service", "pkg.cache.Service", "anchor2", "chunk2")

	cfg := Config{
		VaultPath:         vaultDir,
		VaultDef:          obsidian.VaultDefinition{Path: vaultDir},
		EmbedProvider:     prov,
		EmbeddingsOn:      true,
		CodeEmbeddingsOn:  true,
		CodeEmbedProvider: prov,
		IntelStore:        intelStore,
	}

	tool := SemanticQueryTool(cfg)
	res, err := tool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "semantic_query",
			Arguments: map[string]any{
				"queries": []string{"go to def Service"},
				"mode":    "go_to_def",
				"limit":   5,
			},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	tc, ok := res.Content[0].(mcp.TextContent)
	require.True(t, ok)
	var payload struct {
		TargetStatus     search.TargetStatus      `json:"targetStatus"`
		TargetCandidates []search.TargetCandidate `json:"targetCandidates"`
		Warnings         []search.Warning         `json:"warnings"`
		Matches          []SemanticMatchPayload   `json:"matches"`
	}
	require.NoError(t, json.Unmarshal([]byte(tc.Text), &payload))
	require.Equal(t, search.TargetStatusAmbiguous, payload.TargetStatus)
	require.Len(t, payload.TargetCandidates, 2)
	require.Len(t, payload.Matches, 2, "ambiguous candidates are returned as supporting choices")
	for i, match := range payload.Matches {
		require.Equal(t, payload.TargetCandidates[i].FQN, match.FQN)
	}
	require.NotEmpty(t, payload.Warnings)
	require.Equal(t, "target_ambiguous", payload.Warnings[0].Code)
}
