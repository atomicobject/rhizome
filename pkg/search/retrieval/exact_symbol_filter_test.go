package retrieval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/relevance"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/stretchr/testify/require"
)

func TestRetrieversApplyExactSymbolsBeforeLimits(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 4})
	query := embedForRetrievalTest(t, provider, "needle")
	vectors := map[string]embeddings.Embedding{}
	for i := range 61 {
		id := fmt.Sprintf("anchor-%02d", i)
		path := fmt.Sprintf("pkg/near-%02d.go", i)
		symbol := fmt.Sprintf("Distractor%02d", i)
		body := strings.Repeat("needle ", 8)
		vector := query
		if i == 60 {
			symbol, body = "Run", "needle"
			vector = append(embeddings.Embedding(nil), query...)
			vector[0] += 0.5
		}
		require.NoError(t, store.ReplaceIntelCodeFile(ctx, path, []codeanchor.IntelAnchor{{
			AnchorID: id, Lang: codeanchor.LangGo, Kind: "function", Path: path,
			Symbol: symbol, FQN: "pkg.Service." + symbol, Fingerprint: id,
		}}, nil, []codeanchor.IntelFTSRow{{ItemType: "anchor", ItemID: id, Path: path, Title: symbol, Body: body}}))
		require.NoError(t, store.ReplaceIntelChunks(ctx, []string{id}, []codeanchor.IntelChunk{{
			ChunkID: "chunk-" + id, OwnerID: id, OwnerType: "anchor", Granularity: "signature_doc", ContentHash: id,
		}}))
		vectors["chunk-"+id] = vector
	}
	require.NoError(t, store.UpsertEmbeddings(ctx, vectors))
	backend := &semantic.Searcher{CodeProvider: provider, NoteProvider: provider, IntelStore: store}
	for name, retriever := range map[string]search.Retriever{
		"lexical": &IntelLexicalRetriever{Store: store},
		"vector":  &VectorRetriever{Semantic: backend},
		"seed":    &SeedVectorRetriever{Semantic: backend},
	} {
		t.Run(name, func(t *testing.T) {
			spec := search.QuerySpec{Text: "needle", Seeds: []knowledge.Handle{knowledge.AnchorHandle("anchor-00")}, Limits: search.Limits{Total: 1}}
			unfiltered, err := retriever.Retrieve(ctx, spec)
			require.NoError(t, err)
			require.NotEmpty(t, unfiltered)
			for _, candidate := range unfiltered {
				require.NotEqual(t, "anchor-60", candidate.AnchorID)
			}
			spec.Filters.ExactSymbols = []string{"Run"}
			filtered, err := retriever.Retrieve(ctx, spec)
			require.NoError(t, err)
			require.Len(t, filtered, 1)
			require.Equal(t, "anchor-60", filtered[0].AnchorID)
		})
	}
}

func TestIntelLexicalRetrieverPreservesExactSymbolOnPathOnlyHit(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	path := "pkg/selection.go"
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, path, []codeanchor.IntelAnchor{
		{AnchorID: "module", Lang: codeanchor.LangGo, Kind: "module", Path: path, Symbol: path, Fingerprint: "module"},
		{AnchorID: "run", Lang: codeanchor.LangGo, Kind: "function", Path: path, Symbol: "Run", FQN: "pkg.Run", Fingerprint: "run"},
	}, nil, []codeanchor.IntelFTSRow{
		{ItemType: "anchor", ItemID: "module", Path: path, Title: path, Body: path},
		{ItemType: "anchor", ItemID: "run", Path: path, Title: "Run", Body: "does work"},
	}))
	retriever := &IntelLexicalRetriever{Store: store}
	filtered, err := retriever.Retrieve(ctx, search.QuerySpec{
		Text: "selection", Filters: search.Filters{ExactSymbols: []string{"Run"}}, Limits: search.Limits{Total: 1},
	})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	require.Equal(t, "run", filtered[0].AnchorID)
	require.Equal(t, "Run", filtered[0].Symbol)
}

func TestServicePreservesExactSymbolsThroughCodeRollup(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := "pkg/selection.go"
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte("package pkg\nfunc Run() {}\nfunc Stop() {}\n"), 0o644))
	store, err := semdb.Open(filepath.Join(root, "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, path, []codeanchor.IntelAnchor{
		{AnchorID: "module", Lang: codeanchor.LangGo, Kind: "module", Path: path, Symbol: path, Fingerprint: "module"},
		{AnchorID: "run", Lang: codeanchor.LangGo, Kind: "function", Path: path, Symbol: "Run", FQN: "pkg.Service.Run", Fingerprint: "run"},
		{AnchorID: "stop", Lang: codeanchor.LangGo, Kind: "function", Path: path, Symbol: "Stop", FQN: "pkg.Service.Stop", Fingerprint: "stop"},
	}, nil, []codeanchor.IntelFTSRow{
		{ItemType: "anchor", ItemID: "module", Path: path, Title: path, Body: path},
		{ItemType: "anchor", ItemID: "run", Path: path, Title: "Run", Body: "does work"},
		{ItemType: "anchor", ItemID: "stop", Path: path, Title: "Stop", Body: "ends work"},
	}))
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"run", "stop"}, []codeanchor.IntelChunk{
		{ChunkID: "run-chunk", OwnerID: "run", OwnerType: "anchor", Granularity: "signature_doc", ContentHash: "run"},
		{ChunkID: "stop-chunk", OwnerID: "stop", OwnerType: "anchor", Granularity: "signature_doc", ContentHash: "stop"},
	}))
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 4})
	query := embedForRetrievalTest(t, provider, "selection")
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{"run-chunk": query, "stop-chunk": query}))
	backend := &semantic.Searcher{CodeProvider: provider, NoteProvider: provider, IntelStore: store}
	lexical := &IntelLexicalRetriever{Store: store, VaultPath: root}
	vector := &VectorRetriever{Semantic: backend}
	seed := &SeedVectorRetriever{Semantic: backend}
	rollupper := &search.CodeRollupper{Intel: store, Root: root}

	for _, lane := range []struct {
		name       string
		retrievers []search.Retriever
		codeChunk  bool
	}{
		{name: "lexical", retrievers: []search.Retriever{lexical}},
		{name: "vector", retrievers: []search.Retriever{vector}, codeChunk: true},
		{name: "vector+lexical", retrievers: []search.Retriever{vector, lexical}, codeChunk: true},
		{name: "seed", retrievers: []search.Retriever{seed}},
		{name: "seed+lexical", retrievers: []search.Retriever{seed, lexical}},
	} {
		for _, filter := range []struct {
			name    string
			symbols []string
			wantIDs []string
		}{
			{name: "symbol", symbols: []string{"Run"}, wantIDs: []string{"run"}},
			{name: "fqn", symbols: []string{"pkg.Service.Run"}, wantIDs: []string{"run"}},
			{name: "dotted suffix", symbols: []string{"Service.Run"}, wantIDs: []string{"run"}},
			{name: "same-file symbols", symbols: []string{"Run", "Stop"}, wantIDs: []string{"run", "stop"}},
		} {
			t.Run(lane.name+"/"+filter.name, func(t *testing.T) {
				spec := search.QuerySpec{
					Text: "selection", Intent: search.IntentSearch,
					Filters: search.Filters{ExactSymbols: filter.symbols}, Limits: search.Limits{Total: len(filter.wantIDs)},
					Seeds: []knowledge.Handle{knowledge.AnchorHandle("run")},
				}
				svc := &search.Service{Retrievers: lane.retrievers, Ranker: &relevance.WeightedRanker{}}
				unrolled, err := svc.Search(ctx, spec)
				require.NoError(t, err)
				if lane.name == "seed+lexical" {
					for _, result := range unrolled.Results {
						var evidenceTypes []string
						for _, evidence := range result.Evidence {
							evidenceTypes = append(evidenceTypes, evidence.Type)
						}
						require.Equal(t, "code", result.Type)
						require.Contains(t, evidenceTypes, "code_vector_similarity")
						require.Contains(t, evidenceTypes, "intel_fts_match")
					}
				}
				svc.Rollupper = rollupper
				rolled, err := svc.Search(ctx, spec)
				require.NoError(t, err)
				require.Len(t, rolled.Results, len(filter.wantIDs))
				var gotIDs []string
				for _, result := range rolled.Results {
					gotIDs = append(gotIDs, result.AnchorID)
					require.Contains(t, filter.wantIDs, result.AnchorID)
					wantHandle := knowledge.AnchorHandle(result.AnchorID)
					if lane.codeChunk {
						wantHandle = knowledge.CodeChunkHandle(result.AnchorID, "signature_doc", 0)
					}
					require.Equal(t, wantHandle, result.Handle)
					require.Equal(t, "function", result.Kind)
				}
				require.ElementsMatch(t, filter.wantIDs, gotIDs)
				require.Equal(t, unrolled.Results, rolled.Results)
			})
		}
	}

	t.Run("unfiltered module", func(t *testing.T) {
		svc := &search.Service{Retrievers: []search.Retriever{vector}, Ranker: &relevance.WeightedRanker{}, Rollupper: rollupper}
		response, err := svc.Search(ctx, search.QuerySpec{Text: "selection", Limits: search.Limits{Total: 2}})
		require.NoError(t, err)
		require.Len(t, response.Results, 1)
		require.Equal(t, "module", response.Results[0].AnchorID)
		require.Equal(t, "module", response.Results[0].Kind)
		require.Equal(t, knowledge.CodeChunkHandle("module", "module", 0), response.Results[0].Handle)
	})
}
