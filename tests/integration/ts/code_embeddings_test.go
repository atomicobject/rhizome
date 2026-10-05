//go:build integration
// +build integration

package integration

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	codeembsql "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/retrieval"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/require"
)

func TestTS_CodeEmbeddings(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ws := fixture.NewWorkspace(t)
	ws.IndexCodeAnchors(t, ctx)

	codeEmbCfg, _, err := obsidian.LoadCodeEmbeddingsConfig(ws.VaultRoot)
	require.NoError(t, err)
	require.True(t, codeEmbCfg.Enabled, "fixture should enable codeEmbeddings")
	require.Equal(t, "test", codeEmbCfg.Provider, "fixture should use deterministic embeddings provider")

	providerCfg := codeEmbCfg.ProviderCfg("")
	provider, err := embeddings.NewProvider(providerCfg)
	require.NoError(t, err)

	intelStore, err := codeanchorsqlite.Open(ws.DBPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	embStore, err := codeembsql.Open(codeEmbCfg.IndexPath, provider.Dimensions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = embStore.Close() })

	syncer := semantic.Syncer{
		Index:        embStore,
		Provider:     provider,
		ProviderInfo: providerCfg,
		Intel:        intelStore,
		Root:         ws.VaultRoot,
		Budget:       semantic.DefaultChunkBudget(),
		Policy:       semantic.DefaultSynthesisPolicy(semantic.PolicyOptions{Budget: semantic.DefaultChunkBudget()}),
	}
	syncer.ChunkWriter = intelStore
	syncer.EmbeddingWriter = intelStore
	require.NoError(t, syncer.Sync(ctx))

	t.Run("lexical_search_finds_symbol", func(t *testing.T) {
		retriever := &retrieval.IntelLexicalRetriever{Store: intelStore}
		matches, err := retriever.Retrieve(ctx, search.QuerySpec{Text: "SomeComponent", Limits: search.Limits{Total: 25}})
		require.NoError(t, err)
		require.True(t, anyCandidate(matches, func(m search.Candidate) bool {
			normalized := filepath.ToSlash(m.Path)
			return strings.HasSuffix(normalized, "tsapp/src/components/SomeComponent.tsx") &&
				(strings.Contains(m.Symbol, "SomeComponent") || strings.Contains(m.FQN, "SomeComponent"))
		}), "expected SomeComponent.tsx symbol in lexical results")
	})

	t.Run("semantic_search_finds_symbol", func(t *testing.T) {
		searcher := semantic.Searcher{
			CodeProvider: provider,
			IntelStore:   intelStore,
		}
		results, err := searcher.Search(ctx, semantic.SearchRequest{
			QueryText: "SomeComponent",
			Filters:   semantic.SearchFilters{Types: []string{"code"}},
			K:         100,
		})
		require.NoError(t, err)
		require.NotEmpty(t, results)
		require.True(t, anySemanticResult(results, func(r semantic.Result) bool {
			normalized := filepath.ToSlash(r.Path)
			return r.Type == "code" &&
				strings.HasSuffix(normalized, "tsapp/src/components/SomeComponent.tsx") &&
				r.Symbol == "SomeComponent"
		}), "expected semantic search to return SomeComponent.tsx")
	})
}

func anyCandidate(chunks []search.Candidate, fn func(search.Candidate) bool) bool {
	for _, c := range chunks {
		if fn(c) {
			return true
		}
	}
	return false
}

func anySemanticResult(results []semantic.Result, fn func(semantic.Result) bool) bool {
	for _, r := range results {
		if fn(r) {
			return true
		}
	}
	return false
}
