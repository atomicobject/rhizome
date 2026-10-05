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

func TestGo_CodeEmbeddings(t *testing.T) {
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
		matches, err := retriever.Retrieve(ctx, search.QuerySpec{Text: "PushUpdates", Limits: search.Limits{Total: 25}})
		require.NoError(t, err)
		require.True(t, anyCandidate(matches, func(m search.Candidate) bool {
			normalized := filepath.ToSlash(m.Path)
			return strings.HasSuffix(normalized, "go/todo/todo.go") &&
				(strings.Contains(m.Symbol, "PushUpdates") || strings.Contains(m.FQN, "PushUpdates"))
		}), "expected go/todo.PushUpdates in lexical results")
	})

	t.Run("semantic_search_finds_symbol", func(t *testing.T) {
		searcher := semantic.Searcher{
			CodeProvider: provider,
			IntelStore:   intelStore,
		}
		results, err := searcher.Search(ctx, semantic.SearchRequest{
			QueryText: "PushUpdates",
			Filters:   semantic.SearchFilters{Types: []string{"code"}},
			K:         100,
		})
		require.NoError(t, err)
		require.NotEmpty(t, results)
		require.True(t, anySemanticResult(results, func(r semantic.Result) bool {
			normalized := filepath.ToSlash(r.Path)
			return r.Type == "code" &&
				strings.HasSuffix(normalized, "go/todo/todo.go") &&
				r.Symbol == "PushUpdates"
		}), "expected semantic search to return go/todo.PushUpdates")
	})

	t.Run("plan_skips_when_intel_embeddings_present", func(t *testing.T) {
		items, chunks, err := embStore.Stats(ctx)
		require.NoError(t, err)
		require.Greater(t, items, 0, "expected code items to be indexed")
		require.Equal(t, 0, chunks, "expected no legacy chunk embeddings when EmbeddingWriter is set")

		meta, ok, err := embStore.Metadata(ctx)
		require.NoError(t, err)
		require.True(t, ok)
		require.False(t, meta.SourceHighWater.IsZero(), "initial sync must publish a source high-water mark")

		anchors, err := intelStore.IntelAnchorMetas(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, anchors)
		ownerIDs := make([]string, 0, len(anchors))
		for _, anchor := range anchors {
			ownerIDs = append(ownerIDs, anchor.AnchorID)
		}
		statesBefore, err := intelStore.IntelChunkEmbeddingStates(ctx, ownerIDs)
		require.NoError(t, err)
		require.True(t, anyNonEmptyChunkHash(statesBefore), "expected canonical Intel chunk hashes after sync")

		// Move only anchor timestamps above the stored high-water mark so Plan
		// bypasses its freshness fast path; content, IDs, fingerprints and
		// Intel chunk hashes stay unchanged. UpdateSourceHighWater is
		// monotonic, so rewinding LastSync alone would not reach this branch.
		afterHighWater := meta.SourceHighWater.Unix() + 1
		_, err = intelStore.DB().ExecContext(ctx, `UPDATE intel_code_anchors SET updated_at = ?`, afterHighWater)
		require.NoError(t, err)
		updated, err := intelStore.IntelAnchorMetas(ctx)
		require.NoError(t, err)
		require.Len(t, updated, len(anchors))
		var maxUpdated int64
		for i, anchor := range updated {
			require.Equal(t, anchors[i].AnchorID, anchor.AnchorID)
			require.Equal(t, anchors[i].Fingerprint, anchor.Fingerprint)
			if anchor.UpdatedAt > maxUpdated {
				maxUpdated = anchor.UpdatedAt
			}
		}
		require.Greater(t, maxUpdated, meta.SourceHighWater.Unix())
		statesAfter, err := intelStore.IntelChunkEmbeddingStates(ctx, ownerIDs)
		require.NoError(t, err)
		require.Equal(t, statesBefore, statesAfter)

		plan, err := syncer.Plan(ctx)
		require.NoError(t, err)
		require.Equal(t, 0, plan.TotalWork, "expected intel chunk hashes to skip embedding work")
		// TotalWork excludes embedding-cache hits, so a plan that re-queued every
		// task from empty legacy states could still report zero work.
		require.Equal(t, 0, plan.CacheHits, "expected intel chunk hashes to leave no task planned")
	})
}

func anyNonEmptyChunkHash(states map[string]map[int]string) bool {
	for _, hashes := range states {
		for _, hash := range hashes {
			if strings.TrimSpace(hash) != "" {
				return true
			}
		}
	}
	return false
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
