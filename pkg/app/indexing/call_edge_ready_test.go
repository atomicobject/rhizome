package indexing

import (
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestCallEdgeReadyIndexMatchesDurableFootprintsWithoutReplanning(t *testing.T) {
	t.Parallel()

	index := newCallEdgeReadyIndex()
	index.seedResidual(codeanchor.CallEdgeResidual{
		SymbolRefs: []codeanchor.SymbolRef{{Lang: codeanchor.LangGo, Name: "Run"}},
		Modules:    []string{"pkg/dependency"},
	})

	index.noteDurableFootprints([]codeanchor.DurableRefFootprint{{
		Path:       "pkg/caller.go",
		SymbolKeys: []string{"go||Run"},
	}})
	require.Equal(t, []string{"pkg/caller.go"}, index.drainReady())

	index.noteDurableFootprints([]codeanchor.DurableRefFootprint{{
		Path:    "pkg/caller.go",
		Modules: []string{"pkg/dependency"},
	}})
	require.Nil(t, index.drainReady())

	index.noteDurableFootprints([]codeanchor.DurableRefFootprint{{
		Path:    "pkg/second.go",
		Modules: []string{"pkg/dependency"},
	}})
	require.Equal(t, []string{"pkg/second.go"}, index.drainReady())
}

func TestCallEdgeReadyIndexClearsDeferredResidual(t *testing.T) {
	t.Parallel()

	index := newCallEdgeReadyIndex()
	index.seedResidual(codeanchor.CallEdgeResidual{
		SymbolRefs: []codeanchor.SymbolRef{{Lang: codeanchor.LangPy, Pkg: "app.core", Name: "run"}},
	})
	require.True(t, index.hasDeferred())

	index.clearResidual()
	require.False(t, index.hasDeferred())

	index.noteDurableFootprints([]codeanchor.DurableRefFootprint{{
		Path:       "src/caller.py",
		SymbolKeys: []string{"py|app.core|run"},
	}})
	require.Nil(t, index.drainReady())
}

func TestCallEdgeReadyIndexSkipsPlannerReadyPathsAlreadyRebuiltThisGeneration(t *testing.T) {
	t.Parallel()

	index := newCallEdgeReadyIndex()
	index.seedResidual(codeanchor.CallEdgeResidual{
		SymbolRefs: []codeanchor.SymbolRef{{Lang: codeanchor.LangGo, Name: "Run"}},
	})

	index.noteDurableFootprints([]codeanchor.DurableRefFootprint{{
		Path:       "pkg/caller.go",
		SymbolKeys: []string{"go||Run"},
	}})
	require.Equal(t, []string{"pkg/caller.go"}, index.drainReady())
	index.markRebuilt([]string{"pkg/caller.go"})

	index.enqueuePlannerReady([]string{"pkg/caller.go", "pkg/other.go"})
	require.Equal(t, []string{"pkg/other.go"}, index.drainReady())
}

func TestCallEdgeReadyIndexDoesNotBumpGenerationWhenResidualIsUnchanged(t *testing.T) {
	t.Parallel()

	index := newCallEdgeReadyIndex()
	index.seedResidual(codeanchor.CallEdgeResidual{
		SymbolRefs: []codeanchor.SymbolRef{{Lang: codeanchor.LangGo, Name: "Run"}},
	})
	firstGeneration := index.generation

	index.seedResidual(codeanchor.CallEdgeResidual{
		SymbolRefs: []codeanchor.SymbolRef{{Lang: codeanchor.LangGo, Name: "Run"}},
	})

	require.Equal(t, firstGeneration, index.generation)
}
