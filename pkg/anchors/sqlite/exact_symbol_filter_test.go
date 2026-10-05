package sqlite

import (
	"context"
	"fmt"
	"strings"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

func TestSearchExactSymbolsAreLiteralAndAppliedBeforeLimit(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "exact-symbol.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	fixtures := []struct{ symbol, fqn string }{
		{"Run", "pkg.Service.Execute"},
		{"Different", "pkg.Worker.Apply"},
		{"Wild%_", "pkg.Special.Wild%_"},
		{" Run ", "pkg.Spaced"},
		{"Éxecute", "pkg.Unicode"},
	}
	vectors := map[string]embeddings.Embedding{}
	for i := range 45 {
		id := fmt.Sprintf("anchor-%02d", i)
		path := fmt.Sprintf("pkg/anchor-%02d.go", i)
		symbol, fqn := "WildXY", "pkg.WildXY"
		body := strings.Repeat("needle ", 8)
		vector := embeddings.Embedding{1, 0, 0, 0}
		if i < len(fixtures) {
			symbol, fqn = fixtures[i].symbol, fixtures[i].fqn
			body = "needle"
			vector = embeddings.Embedding{1, float32(i+1) / 10, 0, 0}
		}
		require.NoError(t, store.ReplaceIntelCodeFile(ctx, path, []codeanchor.IntelAnchor{{
			AnchorID: id, Lang: codeanchor.LangGo, Kind: "function", Path: path,
			Symbol: symbol, FQN: fqn, Fingerprint: id,
		}}, nil, []codeanchor.IntelFTSRow{{ItemType: "anchor", ItemID: id, Path: path, Title: symbol, Body: body}}))
		require.NoError(t, store.ReplaceIntelChunks(ctx, []string{id}, []codeanchor.IntelChunk{{
			ChunkID: id, OwnerID: id, OwnerType: "anchor", Granularity: "symbol", ContentHash: id,
		}}))
		vectors[id] = vector
	}
	require.NoError(t, store.ReplaceIntelDocSections(ctx, "notes/Run.md", []codeanchor.IntelDocSection{{
		SectionID: "note", Path: "notes/Run.md", Title: "Run", Level: 1, Content: "needle", Fingerprint: "note",
	}}, nil, []codeanchor.IntelFTSRow{{ItemType: "doc_section", ItemID: "note", Path: "notes/Run.md", Title: "Run", Body: "needle"}}))
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"note"}, []codeanchor.IntelChunk{{
		ChunkID: "note", OwnerID: "note", OwnerType: "doc_section", Granularity: "section", ContentHash: "note",
	}}))
	vectors["note"] = embeddings.Embedding{1, 0, 0, 0}
	require.NoError(t, store.UpsertEmbeddings(ctx, vectors))
	query := embeddings.Embedding{1, 0, 0, 0}
	unfiltered, _, err := store.SearchEmbeddings(ctx, query, 1, EmbeddingSearchFilters{})
	require.NoError(t, err)
	require.Equal(t, []string{"anchor-05"}, chunkIDs(unfiltered))
	for _, tt := range []struct {
		name    string
		symbols []string
		ids     []string
	}{
		{"symbol", []string{"Run"}, []string{"anchor-00"}},
		{"fqn", []string{"pkg.Worker.Apply"}, []string{"anchor-01"}},
		{"suffix", []string{"Worker.Apply"}, []string{"anchor-01"}},
		{"percent and underscore", []string{"Wild%_"}, []string{"anchor-02"}},
		{"literal whitespace", []string{" Run "}, []string{"anchor-03"}},
		{"unicode", []string{"Éxecute"}, []string{"anchor-04"}},
		{"multiple needles", []string{"Run", "Wild%_"}, []string{"anchor-00", "anchor-02"}},
		{"empty and match", []string{"", "Run"}, []string{"anchor-00"}},
		{"case mismatch", []string{"run"}, []string{}},
		{"suffix boundary", []string{"pply"}, []string{}},
		{"empty needle", []string{""}, []string{}},
		{"no match", []string{"Missing"}, []string{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, limit := range []int{1, 10} {
				want := tt.ids[:min(limit, len(tt.ids))]
				filters := EmbeddingSearchFilters{ExactSymbols: tt.symbols}
				knn, _, err := store.SearchEmbeddings(ctx, query, limit, filters)
				require.NoError(t, err)
				require.Equal(t, want, chunkIDs(knn))
				scalar, _, err := store.searchEmbeddingsScalar(ctx, query, limit, filters)
				require.NoError(t, err)
				require.Equal(t, want, chunkIDs(scalar))
				requireScoredChunkParity(t, scalar, knn)
				lexical, err := store.SearchIntelFTSFiltered(ctx, "needle", limit, IntelSearchFilters{ExactSymbols: tt.symbols})
				require.NoError(t, err)
				require.Equal(t, want, intelSearchIDs(lexical))
			}
		})
	}
}
