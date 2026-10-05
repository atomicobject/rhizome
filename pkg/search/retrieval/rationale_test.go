package retrieval

import (
	"context"
	"path/filepath"
	"testing"
	"unicode/utf8"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

type countingRationaleStore struct {
	*semdb.Store
	fqnLookups  int
	pathLookups int
}

func (s *countingRationaleStore) IntelAnchorsByFQNsLimited(ctx context.Context, fqns []string, limitPerFQN int) (map[string][]codeanchor.IntelAnchor, error) {
	s.fqnLookups++
	return s.Store.IntelAnchorsByFQNsLimited(ctx, fqns, limitPerFQN)
}

func (s *countingRationaleStore) IntelAnchorsByPaths(ctx context.Context, paths []string) (map[string][]codeanchor.IntelAnchor, error) {
	s.pathLookups++
	return s.Store.IntelAnchorsByPaths(ctx, paths)
}

func TestRationaleFTSRetriever_SymbolRationaleSupportsAnchor(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	anchor := codeanchor.IntelAnchor{
		AnchorID: "search-anchor",
		Lang:     codeanchor.LangGo,
		Kind:     "method",
		Path:     "pkg/search/service.go",
		Symbol:   "Search",
		FQN:      "github.com/acme/pkg/search.Service.Search",
		// required by STRICT schema
		Fingerprint: "fp",
		StartLine:   10,
		EndLine:     20,
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, anchor.Path, []codeanchor.IntelAnchor{anchor}, nil, nil))
	require.NoError(t, store.ReplaceRationaleForPath(ctx, anchor.Path, []codeanchor.Rationale{{
		ID:          "rat-symbol-0001",
		Path:        anchor.Path,
		SymbolFQN:   anchor.FQN,
		Kind:        codeanchor.RationaleWhy,
		Content:     "WHY: preserve mixed evidence across retrievers",
		StartLine:   12,
		EndLine:     12,
		Fingerprint: "rfp",
	}}))

	retriever := &RationaleFTSRetriever{Store: store}
	cands, err := retriever.Retrieve(ctx, search.QuerySpec{
		Text:   "why preserve evidence",
		Intent: search.IntentSearch,
		Limits: search.Limits{Total: 10},
	})
	require.NoError(t, err)
	require.Len(t, cands, 1)
	require.True(t, cands[0].SupportOnly)
	require.Equal(t, knowledge.AnchorHandle(anchor.AnchorID), cands[0].Handle)
	require.Equal(t, "rationale_fts_match", cands[0].Evidence[0].Type)
	require.Equal(t, "rat-symbol-0001", cands[0].Evidence[0].Details["rationaleID"])
}

func TestRationaleFTSRetriever_FileLevelRationaleSupportsFileAndModuleAnchor(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	path := "pkg/search/service.go"
	module := codeanchor.IntelAnchor{
		AnchorID:    "module-anchor",
		Lang:        codeanchor.LangGo,
		Kind:        "module",
		Path:        path,
		Symbol:      "service.go",
		Fingerprint: "fp",
		StartLine:   1,
		EndLine:     1,
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, path, []codeanchor.IntelAnchor{module}, nil, nil))
	require.NoError(t, store.ReplaceRationaleForPath(ctx, path, []codeanchor.Rationale{{
		ID:          "rat-file-0001",
		Path:        path,
		Kind:        codeanchor.RationaleImportant,
		Content:     "IMPORTANT: preserve deadline fallback behavior",
		StartLine:   1,
		EndLine:     1,
		Fingerprint: "rfp",
	}}))

	retriever := &RationaleFTSRetriever{Store: store}
	cands, err := retriever.Retrieve(ctx, search.QuerySpec{
		Text:   "why deadline fallback",
		Intent: search.IntentSearch,
		Limits: search.Limits{Total: 10},
	})
	require.NoError(t, err)
	require.Len(t, cands, 2)
	handles := map[string]bool{}
	for _, c := range cands {
		require.True(t, c.SupportOnly)
		handles[c.Handle.String()] = true
	}
	require.True(t, handles[knowledge.FileHandle(path).String()])
	require.True(t, handles[knowledge.AnchorHandle(module.AnchorID).String()])
}

func TestRationaleFTSRetriever_BatchesProjectionLookups(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	anchors := []codeanchor.IntelAnchor{
		{
			AnchorID:    "symbol-one",
			Lang:        codeanchor.LangGo,
			Kind:        "function",
			Path:        "pkg/search/one.go",
			Symbol:      "One",
			FQN:         "github.com/acme/pkg/search.One",
			Fingerprint: "fp-one",
			StartLine:   10,
			EndLine:     12,
		},
		{
			AnchorID:    "symbol-two",
			Lang:        codeanchor.LangGo,
			Kind:        "function",
			Path:        "pkg/search/two.go",
			Symbol:      "Two",
			FQN:         "github.com/acme/pkg/search.Two",
			Fingerprint: "fp-two",
			StartLine:   20,
			EndLine:     22,
		},
	}
	for _, anchor := range anchors {
		require.NoError(t, store.ReplaceIntelCodeFile(ctx, anchor.Path, []codeanchor.IntelAnchor{anchor}, nil, nil))
		require.NoError(t, store.ReplaceRationaleForPath(ctx, anchor.Path, []codeanchor.Rationale{{
			ID:          "rat-" + anchor.AnchorID,
			Path:        anchor.Path,
			SymbolFQN:   anchor.FQN,
			Kind:        codeanchor.RationaleWhy,
			Content:     "WHY: shared rationale evidence should batch lookup projection",
			StartLine:   anchor.StartLine,
			EndLine:     anchor.StartLine,
			Fingerprint: "rfp-" + anchor.AnchorID,
		}}))
	}
	module := codeanchor.IntelAnchor{
		AnchorID:    "module-three",
		Lang:        codeanchor.LangGo,
		Kind:        "module",
		Path:        "pkg/search/three.go",
		Symbol:      "three.go",
		Fingerprint: "fp-three",
		StartLine:   1,
		EndLine:     1,
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, module.Path, []codeanchor.IntelAnchor{module}, nil, nil))
	require.NoError(t, store.ReplaceRationaleForPath(ctx, module.Path, []codeanchor.Rationale{{
		ID:          "rat-file-three",
		Path:        module.Path,
		Kind:        codeanchor.RationaleWhy,
		Content:     "WHY: shared rationale evidence should batch lookup projection",
		StartLine:   1,
		EndLine:     1,
		Fingerprint: "rfp-three",
	}}))

	counting := &countingRationaleStore{Store: store}
	retriever := &RationaleFTSRetriever{Store: counting}
	cands, err := retriever.Retrieve(ctx, search.QuerySpec{
		Text:   "why shared rationale evidence",
		Intent: search.IntentSearch,
		Limits: search.Limits{Total: 10},
	})
	require.NoError(t, err)
	require.NotEmpty(t, cands)
	require.Equal(t, 1, counting.fqnLookups)
	require.Equal(t, 1, counting.pathLookups)
}

func TestRationaleFTSRetriever_GatesGenericQueries(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	path := "pkg/search/service.go"
	require.NoError(t, store.ReplaceRationaleForPath(ctx, path, []codeanchor.Rationale{{
		ID: "rat-gate", Path: path, Kind: codeanchor.RationaleWhy,
		Content: "WHY: search service preserves evidence", StartLine: 1, EndLine: 1, Fingerprint: "fp",
	}}))
	rows, err := store.SearchRationaleFTS(ctx, "search service", defaultRationaleFTSKinds, 10)
	require.NoError(t, err)
	require.NotEmpty(t, rows, "generic terms must reach FTS before the gate suppresses them")
	retriever := &RationaleFTSRetriever{Store: store}
	positive, err := retriever.Retrieve(ctx, search.QuerySpec{Text: "why search service", Intent: search.IntentSearch})
	require.NoError(t, err)
	require.NotEmpty(t, positive)
	generic, err := retriever.Retrieve(ctx, search.QuerySpec{Text: "search service", Intent: search.IntentSearch})
	require.NoError(t, err)
	require.Empty(t, generic)
}

func TestTrimRationaleOneLine_UTF8SafeAndMarked(t *testing.T) {
	got := trimRationaleOneLine("WHY: prefix 😀 suffix", 14)
	require.True(t, utf8.ValidString(got))
	require.Contains(t, got, "...")
}
