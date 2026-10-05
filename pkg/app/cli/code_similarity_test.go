package actions

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semstore "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

type stubCodeSimilarityIntel struct {
	metas       []codeanchor.IntelAnchorMeta
	anchors     map[string]codeanchor.IntelAnchor
	chunks      []codeanchor.IntelChunk
	embeddings  map[string]embeddings.Embedding
	refsByPath  map[string][]codeanchor.SymbolRefRow
	ancestors   map[string][]string
	search      map[string][]semstore.ScoredChunk
	lastQueries []semstore.EmbeddingSearchFilters
}

func (s *stubCodeSimilarityIntel) IntelAnchorMetas(ctx context.Context) ([]codeanchor.IntelAnchorMeta, error) {
	return s.metas, nil
}

func (s *stubCodeSimilarityIntel) IntelAnchorsByIDs(ctx context.Context, anchorIDs []string) (map[string]codeanchor.IntelAnchor, error) {
	out := make(map[string]codeanchor.IntelAnchor, len(anchorIDs))
	for _, id := range anchorIDs {
		if anchor, ok := s.anchors[id]; ok {
			out[id] = anchor
		}
	}
	return out, nil
}

func (s *stubCodeSimilarityIntel) IntelChunksByOwners(ctx context.Context, ownerIDs []string) ([]codeanchor.IntelChunk, error) {
	keep := make(map[string]struct{}, len(ownerIDs))
	for _, id := range ownerIDs {
		keep[id] = struct{}{}
	}
	var out []codeanchor.IntelChunk
	for _, chunk := range s.chunks {
		if _, ok := keep[chunk.OwnerID]; ok {
			out = append(out, chunk)
		}
	}
	return out, nil
}

func (s *stubCodeSimilarityIntel) EmbeddingsByChunkIDs(ctx context.Context, chunkIDs []string) (map[string]embeddings.Embedding, error) {
	out := make(map[string]embeddings.Embedding, len(chunkIDs))
	for _, id := range chunkIDs {
		if emb, ok := s.embeddings[id]; ok {
			out[id] = emb
		}
	}
	return out, nil
}

func (s *stubCodeSimilarityIntel) SearchEmbeddings(ctx context.Context, query embeddings.Embedding, k int, filters semstore.EmbeddingSearchFilters) ([]semstore.ScoredChunk, int, error) {
	s.lastQueries = append(s.lastQueries, filters)
	return append([]semstore.ScoredChunk(nil), s.search[embeddingKey(query)]...), 0, nil
}

func (s *stubCodeSimilarityIntel) SymbolRefsByPaths(ctx context.Context, paths []string) (map[string][]codeanchor.SymbolRefRow, error) {
	out := make(map[string][]codeanchor.SymbolRefRow, len(paths))
	for _, path := range paths {
		out[path] = append([]codeanchor.SymbolRefRow(nil), s.refsByPath[path]...)
	}
	return out, nil
}

func (s *stubCodeSimilarityIntel) Ancestors(ctx context.Context, fqn string) ([]string, error) {
	return append([]string(nil), s.ancestors[fqn]...), nil
}

func TestCodeSimilarity_ReportsOverlappingTypes(t *testing.T) {
	intel := newSimilarityFixture()
	report, err := CodeSimilarity(context.Background(), intel, CodeSimilarityOptions{
		Roots: []string{"pkg/source"},
		Limit: 5,
	})
	require.NoError(t, err)
	require.NotEmpty(t, report.OverlappingTypes)
	require.Equal(t, "pkg.Source.UserDTO", report.OverlappingTypes[0].Source.FQN)
	require.Equal(t, "pkg.Match.UserDTO", report.OverlappingTypes[0].Match.FQN)
	require.Contains(t, report.OverlappingTypes[0].SharedSignals, "type_ref:pkg.model.User")
	require.Contains(t, report.OverlappingTypes[0].SharedSignals, "ancestor:pkg.api.Payload")
}

func TestCodeSimilarity_MissingEmbeddingsWarns(t *testing.T) {
	intel := newSimilarityFixture()
	intel.embeddings = map[string]embeddings.Embedding{}
	report, err := CodeSimilarity(context.Background(), intel, CodeSimilarityOptions{
		Roots: []string{"pkg/source"},
	})
	require.NoError(t, err)
	require.Contains(t, report.Warnings, codeSimilarityMissingWarning)
}

func TestCodeSimilarity_NoPairDoesNotWarnMissingEmbeddings(t *testing.T) {
	source := codeanchor.IntelAnchor{AnchorID: "source-fn", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/source/load.go", Symbol: "Load", FQN: "pkg.Source.Load"}
	intel := &stubCodeSimilarityIntel{
		metas: []codeanchor.IntelAnchorMeta{{
			AnchorID: source.AnchorID,
			Lang:     source.Lang,
			Kind:     source.Kind,
			Path:     source.Path,
			Symbol:   source.Symbol,
			FQN:      source.FQN,
		}},
		anchors: map[string]codeanchor.IntelAnchor{source.AnchorID: source},
		chunks: []codeanchor.IntelChunk{
			{ChunkID: "source-fn-chunk", OwnerID: source.AnchorID, OwnerType: "anchor", Ord: 0, Granularity: "signature_doc"},
		},
		embeddings: map[string]embeddings.Embedding{
			"source-fn-chunk": {1, 0},
		},
		search: map[string][]semstore.ScoredChunk{
			embeddingKey(embeddings.Embedding{1, 0}): {
				{ChunkID: "self", OwnerID: source.AnchorID, OwnerType: "anchor", Granularity: "signature_doc", Score: 1.0},
			},
		},
	}

	report, err := CodeSimilarity(context.Background(), intel, CodeSimilarityOptions{
		Roots: []string{"pkg/source"},
	})
	require.NoError(t, err)
	require.Empty(t, report.SimilarFunctions)
	require.Empty(t, report.OverlappingTypes)
	require.NotContains(t, report.Warnings, codeSimilarityMissingWarning)
}

func TestCodeSimilarity_DeduplicatesReversedPairs(t *testing.T) {
	left := codeanchor.IntelAnchor{AnchorID: "left-fn", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/z.go", Symbol: "Load", FQN: "pkg.Left.Load", StartByte: 20}
	right := codeanchor.IntelAnchor{AnchorID: "right-fn", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/a.go", Symbol: "Load", FQN: "pkg.Right.Load", StartByte: 10}
	intel := &stubCodeSimilarityIntel{
		metas: []codeanchor.IntelAnchorMeta{
			{AnchorID: left.AnchorID, Lang: left.Lang, Kind: left.Kind, Path: left.Path, Symbol: left.Symbol, FQN: left.FQN},
			{AnchorID: right.AnchorID, Lang: right.Lang, Kind: right.Kind, Path: right.Path, Symbol: right.Symbol, FQN: right.FQN},
		},
		anchors: map[string]codeanchor.IntelAnchor{
			left.AnchorID:  left,
			right.AnchorID: right,
		},
		chunks: []codeanchor.IntelChunk{
			{ChunkID: "left-chunk", OwnerID: left.AnchorID, OwnerType: "anchor", Ord: 0, Granularity: "signature_doc"},
			{ChunkID: "right-chunk", OwnerID: right.AnchorID, OwnerType: "anchor", Ord: 0, Granularity: "signature_doc"},
		},
		embeddings: map[string]embeddings.Embedding{
			"left-chunk":  {1, 0},
			"right-chunk": {0, 1},
		},
		search: map[string][]semstore.ScoredChunk{
			embeddingKey(embeddings.Embedding{1, 0}): {
				{ChunkID: "right-chunk", OwnerID: right.AnchorID, OwnerType: "anchor", Granularity: "signature_doc", Score: 0.91},
			},
			embeddingKey(embeddings.Embedding{0, 1}): {
				{ChunkID: "left-chunk", OwnerID: left.AnchorID, OwnerType: "anchor", Granularity: "signature_doc", Score: 0.92},
			},
		},
	}

	report, err := CodeSimilarity(context.Background(), intel, CodeSimilarityOptions{
		Roots: []string{"."},
		Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, report.SimilarFunctions, 1)
	pair := map[string]struct{}{
		report.SimilarFunctions[0].Source.AnchorID: {},
		report.SimilarFunctions[0].Match.AnchorID:  {},
	}
	require.Contains(t, pair, left.AnchorID)
	require.Contains(t, pair, right.AnchorID)
	require.Equal(t, right.AnchorID, report.SimilarFunctions[0].Source.AnchorID, "public pair orientation is path/start-position stable")
	require.Equal(t, left.AnchorID, report.SimilarFunctions[0].Match.AnchorID)
}

func TestCodeSimilarity_WithSQLiteStoreRanksEmbeddedFunctionMatches(t *testing.T) {
	ctx := context.Background()
	store, err := semstore.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	source := codeanchor.IntelAnchor{AnchorID: "source-fn", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/source/load.go", Symbol: "Load", FQN: "pkg.Source.Load", Signature: "func Load() error", StartLine: 3, EndLine: 12, Fingerprint: "source-fp"}
	sourceType := codeanchor.IntelAnchor{AnchorID: "source-type", Lang: codeanchor.LangGo, Kind: "struct", Path: "pkg/source/type.go", Symbol: "Config", FQN: "pkg.Source.Config", Signature: "type Config struct", StartLine: 1, EndLine: 3, Fingerprint: "source-type-fp"}
	match := codeanchor.IntelAnchor{AnchorID: "match-fn", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/match/load.go", Symbol: "Load", FQN: "pkg.Match.Load", Signature: "func Load() error", StartLine: 4, EndLine: 13, Fingerprint: "match-fp"}
	unrelated := codeanchor.IntelAnchor{AnchorID: "unrelated-fn", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/other/render.go", Symbol: "Render", FQN: "pkg.Other.Render", Signature: "func Render() string", StartLine: 5, EndLine: 8, Fingerprint: "unrelated-fp"}

	require.NoError(t, store.ReplaceIntelCodeFile(ctx, source.Path, []codeanchor.IntelAnchor{source}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, sourceType.Path, []codeanchor.IntelAnchor{sourceType}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, match.Path, []codeanchor.IntelAnchor{match}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, unrelated.Path, []codeanchor.IntelAnchor{unrelated}, nil, nil))
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{source.AnchorID, match.AnchorID, unrelated.AnchorID}, []codeanchor.IntelChunk{
		{ChunkID: "source-chunk", OwnerID: source.AnchorID, OwnerType: "anchor", Ord: 0, Granularity: "signature_doc", ContentHash: "source"},
		{ChunkID: "match-chunk", OwnerID: match.AnchorID, OwnerType: "anchor", Ord: 0, Granularity: "signature_doc", ContentHash: "match"},
		{ChunkID: "unrelated-chunk", OwnerID: unrelated.AnchorID, OwnerType: "anchor", Ord: 0, Granularity: "signature_doc", ContentHash: "unrelated"},
	}))
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{
		"source-chunk":    {1, 0},
		"match-chunk":     {0.97, 0.03},
		"unrelated-chunk": {0, 1},
	}))
	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{
		source.Path: {{SrcPath: source.Path, OwnerFQN: source.FQN, RefKind: codeanchor.RefKindCalls, DstFQN: "pkg.config.Read"}},
		match.Path:  {{SrcPath: match.Path, OwnerFQN: match.FQN, RefKind: codeanchor.RefKindCalls, DstFQN: "pkg.config.Read"}},
	}))

	report, err := CodeSimilarity(ctx, store, CodeSimilarityOptions{Roots: []string{"pkg/source"}, Limit: 3})
	require.NoError(t, err)
	require.Empty(t, report.Warnings)
	require.Equal(t, 2, report.SourceAnchorCount)
	require.NotEmpty(t, report.SimilarFunctions)
	require.Equal(t, source.FQN, report.SimilarFunctions[0].Source.FQN)
	require.Equal(t, match.FQN, report.SimilarFunctions[0].Match.FQN)
	require.Equal(t, "pkg/match/load.go", report.SimilarFunctions[0].Match.Path)
	require.NotEqual(t, report.SimilarFunctions[0].Source.AnchorID, report.SimilarFunctions[0].Match.AnchorID)
	require.Contains(t, report.SimilarFunctions[0].Reasons, "shared code references")
	require.Contains(t, report.SimilarFunctions[0].SharedSignals, "calls:pkg.config.Read")
	for i, row := range report.SimilarFunctions {
		require.Equal(t, source.AnchorID, row.Source.AnchorID)
		require.NotEqual(t, row.Source.AnchorID, row.Match.AnchorID)
		if i > 0 {
			require.GreaterOrEqual(t, report.SimilarFunctions[i-1].CombinedScore, row.CombinedScore)
		}
	}
}

func newSimilarityFixture() *stubCodeSimilarityIntel {
	anchors := map[string]codeanchor.IntelAnchor{
		"source-fn":    {AnchorID: "source-fn", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/source/load.go", Symbol: "Load", FQN: "pkg.Source.Load", Signature: "func Load(path string) (Config, error)", StartLine: 3, EndLine: 20},
		"source-type":  {AnchorID: "source-type", Lang: codeanchor.LangGo, Kind: "struct", Path: "pkg/source/user.go", Symbol: "UserDTO", FQN: "pkg.Source.UserDTO", Signature: "type UserDTO struct", StartLine: 5, EndLine: 12},
		"match-fn":     {AnchorID: "match-fn", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/match/load.go", Symbol: "Load", FQN: "pkg.Match.Load", Signature: "func Load(name string) (Settings, error)", StartLine: 4, EndLine: 21},
		"unrelated-fn": {AnchorID: "unrelated-fn", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/other/render.go", Symbol: "Render", FQN: "pkg.Other.Render", Signature: "func Render() string", StartLine: 4, EndLine: 8},
		"match-type":   {AnchorID: "match-type", Lang: codeanchor.LangGo, Kind: "struct", Path: "pkg/match/user.go", Symbol: "UserDTO", FQN: "pkg.Match.UserDTO", Signature: "type UserDTO struct", StartLine: 5, EndLine: 12},
	}
	var metas []codeanchor.IntelAnchorMeta
	for _, anchor := range anchors {
		metas = append(metas, codeanchor.IntelAnchorMeta{
			AnchorID: anchor.AnchorID,
			Lang:     anchor.Lang,
			Kind:     anchor.Kind,
			Path:     anchor.Path,
			Symbol:   anchor.Symbol,
			FQN:      anchor.FQN,
		})
	}
	return &stubCodeSimilarityIntel{
		metas:   metas,
		anchors: anchors,
		chunks: []codeanchor.IntelChunk{
			{ChunkID: "source-fn-chunk", OwnerID: "source-fn", OwnerType: "anchor", Ord: 0, Granularity: "signature_doc"},
			{ChunkID: "source-type-chunk", OwnerID: "source-type", OwnerType: "anchor", Ord: 0, Granularity: "decl_full"},
		},
		embeddings: map[string]embeddings.Embedding{
			"source-fn-chunk":   {1, 0},
			"source-type-chunk": {0, 1},
		},
		refsByPath: map[string][]codeanchor.SymbolRefRow{
			"pkg/source/load.go": {
				{OwnerFQN: "pkg.Source.Load", RefKind: codeanchor.RefKindCalls, DstFQN: "pkg.config.Read"},
			},
			"pkg/match/load.go": {
				{OwnerFQN: "pkg.Match.Load", RefKind: codeanchor.RefKindCalls, DstFQN: "pkg.config.Read"},
			},
			"pkg/source/user.go": {
				{OwnerFQN: "pkg.Source.UserDTO", RefKind: codeanchor.RefKindTypeRef, DstFQN: "pkg.model.User"},
			},
			"pkg/match/user.go": {
				{OwnerFQN: "pkg.Match.UserDTO", RefKind: codeanchor.RefKindTypeRef, DstFQN: "pkg.model.User"},
			},
		},
		ancestors: map[string][]string{
			"pkg.Source.UserDTO": {"pkg.api.Payload"},
			"pkg.Match.UserDTO":  {"pkg.api.Payload"},
		},
		search: map[string][]semstore.ScoredChunk{
			embeddingKey(embeddings.Embedding{1, 0}): {
				{ChunkID: "self", OwnerID: "source-fn", OwnerType: "anchor", Granularity: "signature_doc", Score: 1.0},
				{ChunkID: "match", OwnerID: "match-fn", OwnerType: "anchor", Granularity: "signature_doc", Score: 0.91},
				{ChunkID: "unrelated", OwnerID: "unrelated-fn", OwnerType: "anchor", Granularity: "signature_doc", Score: 0.82},
			},
			embeddingKey(embeddings.Embedding{0, 1}): {
				{ChunkID: "match-type", OwnerID: "match-type", OwnerType: "anchor", Granularity: "decl_full", Score: 0.88},
			},
		},
	}
}

func embeddingKey(emb embeddings.Embedding) string {
	if len(emb) == 2 && emb[0] == 1 && emb[1] == 0 {
		return "fn"
	}
	if len(emb) == 2 && emb[0] == 0 && emb[1] == 1 {
		return "type"
	}
	return "other"
}

func TestCodeSimilarity_WithSQLiteStoreFiltersBeforeCandidateLimit(t *testing.T) {
	for _, dimension := range []string{"owner", "kind", "granularity"} {
		t.Run(dimension, func(t *testing.T) {
			ctx := context.Background()
			store, err := semstore.Open(filepath.Join(t.TempDir(), "intel.db"))
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })
			source := codeanchor.IntelAnchor{AnchorID: "source", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/source/load.go", Symbol: "Load", FQN: "pkg.Source.Load", Fingerprint: "source"}
			match := codeanchor.IntelAnchor{AnchorID: "match", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/match/load.go", Symbol: "Load", FQN: "pkg.Match.Load", Fingerprint: "match"}
			require.NoError(t, store.ReplaceIntelCodeFile(ctx, source.Path, []codeanchor.IntelAnchor{source}, nil, nil))
			require.NoError(t, store.ReplaceIntelCodeFile(ctx, match.Path, []codeanchor.IntelAnchor{match}, nil, nil))
			owners := []string{source.AnchorID, match.AnchorID}
			chunks := []codeanchor.IntelChunk{
				{ChunkID: "source-chunk", OwnerID: source.AnchorID, OwnerType: "anchor", Granularity: "signature_doc", ContentHash: "source"},
				{ChunkID: "match-chunk", OwnerID: match.AnchorID, OwnerType: "anchor", Granularity: "signature_doc", ContentHash: "match"},
			}
			vectors := map[string]embeddings.Embedding{"source-chunk": {1, 0}, "match-chunk": {0.9, 0.1}}
			var distractions []codeanchor.IntelAnchor
			var sections []codeanchor.IntelDocSection
			for i := 0; i < 51; i++ {
				id := fmt.Sprintf("distractor-%02d", i)
				chunkID := id + "-chunk"
				owners = append(owners, id)
				chunk := codeanchor.IntelChunk{ChunkID: chunkID, OwnerID: id, OwnerType: "anchor", Granularity: "signature_doc", ContentHash: id}
				switch dimension {
				case "owner":
					sections = append(sections, codeanchor.IntelDocSection{SectionID: id, Path: "docs/distractors.md", Title: id, Level: 1, Content: id, Fingerprint: id})
					chunk.OwnerType = "doc_section"
				case "kind":
					distractions = append(distractions, codeanchor.IntelAnchor{AnchorID: id, Lang: codeanchor.LangGo, Kind: "struct", Path: "pkg/distractors.go", Symbol: id, FQN: "pkg." + id, Fingerprint: id})
				case "granularity":
					distractions = append(distractions, codeanchor.IntelAnchor{AnchorID: id, Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/distractors.go", Symbol: id, FQN: "pkg." + id, Fingerprint: id})
					chunk.Granularity = "body"
				}
				chunks = append(chunks, chunk)
				vectors[chunkID] = embeddings.Embedding{1, 0}
			}
			if len(distractions) > 0 {
				require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/distractors.go", distractions, nil, nil))
			}
			if len(sections) > 0 {
				require.NoError(t, store.ReplaceIntelDocSections(ctx, "docs/distractors.md", sections, nil, nil))
			}
			require.NoError(t, store.ReplaceIntelChunks(ctx, owners, chunks))
			require.NoError(t, store.UpsertEmbeddings(ctx, vectors))
			report, err := CodeSimilarity(ctx, store, CodeSimilarityOptions{Roots: []string{"pkg/source"}, Limit: 1})
			require.NoError(t, err)
			require.Len(t, report.SimilarFunctions, 1)
			require.Equal(t, "match", report.SimilarFunctions[0].Match.AnchorID)
		})
	}
}
