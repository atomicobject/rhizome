package search

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFiltersAllowsSymbolMatchesNameFQNOrSuffix(t *testing.T) {
	filters := Filters{ExactSymbols: []string{"Run"}}

	require.True(t, filters.AllowsSymbol("Run", ""))
	require.True(t, filters.AllowsSymbol("", "Run"))
	require.True(t, filters.AllowsSymbol("Execute", "pkg.Service.Run"))
	require.False(t, filters.AllowsSymbol("Runner", "pkg.Service.Runner"))
	require.False(t, filters.AllowsSymbol("", ""))
	require.True(t, Filters{}.AllowsSymbol("", ""))
}
