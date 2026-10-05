package codeanchor_test

import (
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestBuildDurableRefFootprintCompactsStoredRefs(t *testing.T) {
	t.Parallel()

	footprint := codeanchor.BuildDurableRefFootprint("pkg/caller.go",
		[]codeanchor.SymbolRefRow{
			{SrcPath: "pkg/caller.go", DstLang: codeanchor.LangGo, DstName: "Run"},
			{SrcPath: "pkg/caller.go", DstLang: codeanchor.LangGo, DstName: "Run"},
			{SrcPath: "pkg/caller.go", DstLang: codeanchor.LangGo, DstPkg: "pkg/core", DstName: "Handle"},
		},
		[]codeanchor.ImportRefRow{
			{SrcPath: "pkg/caller.go", Module: "pkg/dependency"},
			{SrcPath: "pkg/caller.go", Module: "pkg/dependency"},
			{SrcPath: "pkg/caller.go", Module: "pkg/other"},
		},
	)

	require.Equal(t, "pkg/caller.go", footprint.Path)
	require.Equal(t, []string{"go|pkg/core|Handle", "go||Run"}, footprint.SymbolKeys)
	require.Equal(t, []string{"pkg/dependency", "pkg/other"}, footprint.Modules)
}

func TestCallEdgeResidualKeyViewsDeduped(t *testing.T) {
	t.Parallel()

	residual := codeanchor.CallEdgeResidual{
		SymbolRefs: []codeanchor.SymbolRef{
			{Lang: codeanchor.LangPy, Pkg: "app.core", Name: "run"},
			{Lang: codeanchor.LangPy, Pkg: "app.core", Name: "run"},
		},
		Modules: []string{"app.core", "app.core", "app.util"},
		Fallbacks: []codeanchor.ReverseIndexFallback{
			{Ref: codeanchor.SymbolRef{Lang: codeanchor.LangPy, Pkg: "app.core", Name: "run"}},
			{Ref: codeanchor.SymbolRef{Lang: codeanchor.LangPy, Pkg: "app.core", Name: "run"}},
		},
	}

	require.Equal(t, []string{"py|app.core|run"}, residual.SymbolKeys())
	require.Equal(t, []string{"app.core", "app.util"}, residual.ModuleKeys())
	require.Len(t, residual.FallbackFiltersBySymbolKey()["py|app.core|run"], 2)
}
