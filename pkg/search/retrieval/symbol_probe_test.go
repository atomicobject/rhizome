package retrieval

import (
	"context"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/stretchr/testify/require"
)

func TestSymbolProbeRetriever_ResolvesIdentifierQuery(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "symbols.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	anchor := codeanchor.IntelAnchor{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/search/service.go",
		Symbol:      "MakePlan",
		FQN:         "github.com/atomicobject/rhizome/pkg/search.MakePlan",
		Signature:   "func MakePlan()",
		StartLine:   10,
		EndLine:     12,
		Fingerprint: "fp-a1",
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, anchor.Path, []codeanchor.IntelAnchor{anchor}, nil, nil))

	r := &SymbolProbeRetriever{Store: store, Limit: 5}
	results, err := r.Retrieve(ctx, search.QuerySpec{Text: "how does MakePlan choose retrievers?"})
	require.NoError(t, err)
	require.Len(t, results, 1)

	got := results[0]
	require.Equal(t, "code", got.Type)
	require.Equal(t, "a1", got.AnchorID)
	require.Equal(t, "MakePlan", got.Symbol)
	require.Equal(t, "symbol_match", got.Evidence[0].Type)
	require.Equal(t, "symbol_probe", got.Evidence[0].Source)
}

func TestSymbolProbeRetriever_ResolvesFQNSuffix(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "symbols.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	anchor := codeanchor.IntelAnchor{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "method",
		Path:        "pkg/search/service.go",
		Symbol:      "Service.Search",
		FQN:         "github.com/atomicobject/rhizome/pkg/search.Service.Search",
		Fingerprint: "fp-a1",
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, anchor.Path, []codeanchor.IntelAnchor{anchor}, nil, nil))

	r := &SymbolProbeRetriever{Store: store, Limit: 5}
	results, err := r.Retrieve(ctx, search.QuerySpec{Text: "Service.Search callers"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "a1", results[0].AnchorID)
	require.Equal(t, "symbol_exact", results[0].Evidence[0].Type)
}

func TestSymbolProbeRetriever_IgnoresPlainLowercaseConcepts(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "symbols.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	anchor := codeanchor.IntelAnchor{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/search/service.go",
		Symbol:      "search",
		FQN:         "pkg.search",
		Fingerprint: "fp-a1",
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, anchor.Path, []codeanchor.IntelAnchor{anchor}, nil, nil))

	r := &SymbolProbeRetriever{Store: store, Limit: 5}
	results, err := r.Retrieve(ctx, search.QuerySpec{Text: "search ranking pipeline"})
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestSymbolProbeRetriever_IgnoresSentenceInitialCapitalizedWords(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "symbols.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	anchor := codeanchor.IntelAnchor{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/search/service.go",
		Symbol:      "How",
		FQN:         "pkg.search.How",
		Fingerprint: "fp-a1",
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, anchor.Path, []codeanchor.IntelAnchor{anchor}, nil, nil))

	r := &SymbolProbeRetriever{Store: store, Limit: 5}
	results, err := r.Retrieve(ctx, search.QuerySpec{Text: "How does search ranking work?"})
	require.NoError(t, err)
	require.Empty(t, results)
}
