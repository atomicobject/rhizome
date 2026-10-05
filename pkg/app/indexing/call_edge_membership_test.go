package indexing

import (
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestCallEdgeMembershipIndexResolvesResidualPaths(t *testing.T) {
	t.Parallel()

	index := newCallEdgeMembershipIndex()
	index.upsertFootprints([]codeanchor.DurableRefFootprint{
		{
			Path:       "pkg/a.go",
			SymbolKeys: []string{"go|pkg.core|Run"},
			Modules:    []string{"pkg/dep"},
		},
		{
			Path:       "pkg/b.go",
			SymbolKeys: []string{"go|pkg.core|Run"},
		},
	})

	paths := index.resolveResidual(codeanchor.CallEdgeResidual{
		SymbolRefs: []codeanchor.SymbolRef{{Lang: codeanchor.LangGo, Pkg: "pkg.core", Name: "Run"}},
		Modules:    []string{"pkg/dep"},
	}, nil)
	require.Equal(t, []string{"pkg/a.go", "pkg/b.go"}, paths)
}

func TestCallEdgeMembershipIndexReplacesExistingFootprint(t *testing.T) {
	t.Parallel()

	index := newCallEdgeMembershipIndex()
	index.upsertFootprints([]codeanchor.DurableRefFootprint{{
		Path:       "pkg/a.go",
		SymbolKeys: []string{"go||Run"},
	}})
	index.upsertFootprints([]codeanchor.DurableRefFootprint{{
		Path:    "pkg/a.go",
		Modules: []string{"pkg/dep"},
	}})

	paths := index.resolveResidual(codeanchor.CallEdgeResidual{
		SymbolRefs: []codeanchor.SymbolRef{{Lang: codeanchor.LangGo, Name: "Run"}},
		Modules:    []string{"pkg/dep"},
	}, nil)
	require.Equal(t, []string{"pkg/a.go"}, paths)

	paths = index.resolveResidual(codeanchor.CallEdgeResidual{
		SymbolRefs: []codeanchor.SymbolRef{{Lang: codeanchor.LangGo, Name: "Run"}},
	}, nil)
	require.Nil(t, paths)
}
