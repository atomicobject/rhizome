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
