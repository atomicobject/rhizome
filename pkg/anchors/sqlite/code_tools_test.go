package sqlite

import (
	"context"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestCodeSymbolCandidatesExactSuffixAmbiguousAndMissing(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/a.go", []codeanchor.IntelAnchor{
		{AnchorID: "a-foo", Lang: codeanchor.LangGo, Kind: "function", Path: "src/a.go", Symbol: "Foo", FQN: "pkg/a.Foo", StartLine: 3, EndLine: 5, Fingerprint: "a"},
	}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/b.go", []codeanchor.IntelAnchor{
		{AnchorID: "b-foo", Lang: codeanchor.LangGo, Kind: "function", Path: "src/b.go", Symbol: "Foo", FQN: "pkg/b.Foo", StartLine: 8, EndLine: 10, Fingerprint: "b"},
	}, nil, nil))

	exact, err := store.CodeSymbolCandidates(ctx, CodeSymbolLookupOptions{Symbol: "pkg/a.Foo", Limit: 10})
	require.NoError(t, err)
	require.Len(t, exact, 1)
	require.Equal(t, "a-foo", exact[0].AnchorID)

	scoped, err := store.CodeSymbolCandidates(ctx, CodeSymbolLookupOptions{Symbol: "Foo", Path: "src/a.go", Limit: 10})
	require.NoError(t, err)
	require.Len(t, scoped, 1)
	require.Equal(t, "a-foo", scoped[0].AnchorID)

	ambiguous, err := store.CodeSymbolCandidates(ctx, CodeSymbolLookupOptions{Symbol: "Foo", Limit: 10})
	require.NoError(t, err)
	require.Len(t, ambiguous, 2)

	missing, err := store.CodeSymbolCandidates(ctx, CodeSymbolLookupOptions{Symbol: "Nope", Limit: 10})
	require.NoError(t, err)
	require.Empty(t, missing)
}

func TestCodeReferencesDeriveCallersAndCalleesFromEdges(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/callee.go", []codeanchor.IntelAnchor{
		{AnchorID: "callee", Lang: codeanchor.LangGo, Kind: "function", Path: "src/callee.go", Symbol: "Target", FQN: "pkg.Target", StartLine: 2, EndLine: 3, Fingerprint: "callee"},
	}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/caller.go", []codeanchor.IntelAnchor{
		{AnchorID: "caller", Lang: codeanchor.LangGo, Kind: "function", Path: "src/caller.go", Symbol: "Run", FQN: "pkg.Run", StartLine: 5, EndLine: 8, Fingerprint: "caller"},
	}, []codeanchor.IntelEdge{
		{SrcID: "caller", DstID: "callee", Kind: "calls"},
	}, nil))

	callers, err := store.CodeCallers(ctx, codeanchor.LangGo, "pkg.Target", 10)
	require.NoError(t, err)
	require.Len(t, callers, 1)
	require.Equal(t, "pkg.Run", callers[0].FQN)

	callees, err := store.CodeCallees(ctx, codeanchor.LangGo, "pkg.Run", 10)
	require.NoError(t, err)
	require.Len(t, callees, 1)
	require.Equal(t, "pkg.Target", callees[0].FQN)

	limited, err := store.CodeCallees(ctx, codeanchor.LangGo, "pkg.Run", 0)
	require.NoError(t, err)
	require.Len(t, limited, 1)
}
