package codeanchor_test

import (
	"context"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/stretchr/testify/require"
)

type scopeRecordingStore struct {
	*semdb.Store
	updated []int64
}

func (s *scopeRecordingStore) SetAnchorScopesBatch(ctx context.Context, updates []codeanchor.AnchorScopeUpdate) error {
	for _, update := range updates {
		s.updated = append(s.updated, update.ID)
	}
	return s.Store.SetAnchorScopesBatch(ctx, updates)
}

func TestIndexCodeFile_DirtiesOnlyAffectedPackageAndRemovedSignals(t *testing.T) {
	root := t.TempDir()
	raw, err := semdb.Open(filepath.Join(root, "code.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	store := &scopeRecordingStore{Store: raw}
	ctx := context.Background()
	for _, pkg := range []string{"a", "b"} {
		require.NoError(t, store.UpsertNote(ctx, codeanchor.Note{
			Path: pkg + ".md", Title: pkg,
			DefinedAnchors: []codeanchor.Anchor{
				{Kind: codeanchor.AnchorAnnotation, Lang: codeanchor.LangPy, Label: pkg + "-ann",
					Ann: &codeanchor.AnnotationSelector{Symbol: codeanchor.SymbolRef{Lang: codeanchor.LangPy, Pkg: pkg, Name: "decorator"}}},
				{Kind: codeanchor.AnchorFunc, Lang: codeanchor.LangPy, Label: pkg + "-func",
					BaseSym: &codeanchor.SymbolRef{Lang: codeanchor.LangPy, Pkg: pkg, Name: "shared"}},
			},
		}))
	}
	svc := codeanchor.NewServiceWithOptions(store, []codeanchor.LanguageIndexer{codeanchor.NewPythonIndexer()}, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache(), codeanchor.WithWriteAccess())
	aPath := filepath.Join(root, "src", "a.py")
	bPath := filepath.Join(root, "src", "b.py")
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, aPath, []byte("def decorator(fn):\n    return fn\n@decorator\ndef shared():\n    pass\n")))
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, bPath, []byte("def decorator(fn):\n    return fn\n@decorator\ndef shared():\n    pass\n")))
	require.NoError(t, svc.RecomputeAnchorScopes(ctx))
	anchors, err := store.Anchors(ctx)
	require.NoError(t, err)
	ids := map[string]int64{}
	for _, anchor := range anchors {
		ids[anchor.Label] = anchor.ID
	}
	require.Len(t, ids, 4)
	for _, pkg := range []string{"a", "b"} {
		symbols, _, err := store.AnchorScope(ctx, ids[pkg+"-ann"])
		require.NoError(t, err)
		require.Contains(t, symbols, pkg+".shared")
	}
	store.updated = nil

	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, aPath, []byte("def decorator(fn):\n    return fn\ndef renamed():\n    pass\n")))
	require.NoError(t, svc.RecomputeAnchorScopes(ctx))
	require.ElementsMatch(t, []int64{ids["a-ann"], ids["a-func"]}, store.updated)
	aSymbols, _, err := store.AnchorScope(ctx, ids["a-ann"])
	require.NoError(t, err)
	require.Empty(t, aSymbols, "removed a.shared must leave no stale scope")
	bSymbols, _, err := store.AnchorScope(ctx, ids["b-ann"])
	require.NoError(t, err)
	require.NotEmpty(t, bSymbols, "b.shared must retain its scope")
}
