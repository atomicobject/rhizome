package codeanchor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefDeltaAccumulatorMergesSortedUniqueChanges(t *testing.T) {
	t.Parallel()

	first := DefDeltas{
		AddedSymbols: []SymbolRef{
			{Lang: LangGo, Pkg: "a", Name: "Foo"},
			{Lang: LangPy, Pkg: "a", Name: "Foo"},
			{Lang: LangGo, Pkg: "b", Name: "Foo"},
			{Lang: LangGo, Pkg: "a", Name: "Foo"},
			{Lang: LangPy, Pkg: "b", Name: "bar"},
			{Lang: LangPhp, Pkg: `N\Worker`, Name: "run", Member: true},
		},
		RemovedSymbols: []SymbolRef{
			{Lang: LangGo, Pkg: "a", Name: "Foo"},
			{Lang: LangTS, Pkg: "c", Name: "Baz"},
		},
		AddedModules:   []string{"pkg/a", "pkg/a", "pkg/b"},
		RemovedModules: []string{"pkg/c"},
	}
	second := DefDeltas{
		AddedSymbols: []SymbolRef{
			{Lang: LangGo, Pkg: "d", Name: "Qux"},
			{Lang: LangPhp, Pkg: `N\Worker`, Name: "run", Member: false},
		},
		RemovedSymbols: []SymbolRef{
			{Lang: LangTS, Pkg: "c", Name: "Baz"},
			{Lang: LangPy, Pkg: "e", Name: "zap"},
		},
		AddedModules:   []string{"pkg/b", "pkg/d"},
		RemovedModules: []string{"pkg/c", "pkg/e"},
	}

	var acc DefDeltaAccumulator
	acc.Add(first)
	acc.Add(second)

	require.Equal(t, DefDeltas{
		AddedSymbols:   []SymbolRef{{Lang: LangGo, Pkg: "a", Name: "Foo"}, {Lang: LangGo, Pkg: "b", Name: "Foo"}, {Lang: LangGo, Pkg: "d", Name: "Qux"}, {Lang: LangPhp, Pkg: `N\Worker`, Name: "run", Member: false}, {Lang: LangPhp, Pkg: `N\Worker`, Name: "run", Member: true}, {Lang: LangPy, Pkg: "a", Name: "Foo"}, {Lang: LangPy, Pkg: "b", Name: "bar"}},
		RemovedSymbols: []SymbolRef{{Lang: LangGo, Pkg: "a", Name: "Foo"}, {Lang: LangPy, Pkg: "e", Name: "zap"}, {Lang: LangTS, Pkg: "c", Name: "Baz"}},
		AddedModules:   []string{"pkg/a", "pkg/b", "pkg/d"},
		RemovedModules: []string{"pkg/c", "pkg/e"},
	}, acc.Finalize())
}

func TestDefDeltaAccumulatorZeroValueAndFinalizeIdempotent(t *testing.T) {
	t.Parallel()

	var acc DefDeltaAccumulator
	require.False(t, acc.HasChanges())
	require.Equal(t, DefDeltas{}, acc.Finalize())

	acc.Add(DefDeltas{
		AddedSymbols: []SymbolRef{{Lang: LangGo, Pkg: "pkg", Name: "Foo"}},
		AddedModules: []string{"pkg/mod"},
	})
	first := acc.Finalize()
	second := acc.Finalize()
	require.Equal(t, first, second)
	require.True(t, acc.HasChanges())
}
