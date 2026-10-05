package obsidian

import (
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestCodeFoldersFallBackToTheWholeProject(t *testing.T) {
	merge := func(local LocalCodeConfig) codeanchor.Config {
		return mergeLocalCodeToAnchorConfig(codeanchor.DefaultConfig(""), local)
	}
	whole := []string{"."}

	automatic := merge(LocalCodeConfig{Enabled: true})
	require.True(t, automatic.AutomaticScope)
	for _, roots := range [][]string{automatic.PythonRoots, automatic.GoRoots, automatic.TSRoots, automatic.CSharpRoots, automatic.PHPRoots} {
		require.Equal(t, whole, roots)
	}
	require.Equal(t, whole, automatic.CodeRoots(), "one folder, not one per language")

	limited := merge(LocalCodeConfig{Enabled: true, Go: &LocalCodeLangConfig{Roots: []string{"cmd"}}, TypeScript: &LocalCodeLangConfig{}})
	require.False(t, limited.AutomaticScope)
	require.Equal(t, []string{"cmd"}, limited.GoRoots)
	require.Equal(t, whole, limited.TSRoots, "a language block without folders covers the project")
	require.Empty(t, limited.PythonRoots, "folder limits leave other languages to those folders")

	jsLimited := merge(LocalCodeConfig{Enabled: true, TypeScript: &LocalCodeLangConfig{Ignore: []string{"**/gen/**"}}, JavaScript: &LocalCodeLangConfig{Roots: []string{"web"}}})
	require.False(t, jsLimited.AutomaticScope, "JavaScript folders limit code indexing too")
	require.Equal(t, []string{"web"}, jsLimited.TSRoots)

	off := merge(LocalCodeConfig{})
	require.False(t, off.Enabled)
	require.Empty(t, off.CodeRoots())
}

func TestCodeAnchorRootsConfigured(t *testing.T) {
	roots := &LocalCodeLangConfig{Roots: []string{"src"}}
	tests := []struct {
		name  string
		local LocalCodeConfig
		want  bool
	}{
		{name: "empty config", local: LocalCodeConfig{}, want: false},
		{name: "enabled without folders covers the project", local: LocalCodeConfig{Enabled: true, Scan: []string{"**/*.go"}}, want: true},
		{name: "language block without roots covers the project", local: LocalCodeConfig{Go: &LocalCodeLangConfig{Scan: []string{"**/*.go"}}}, want: true},
		{name: "go roots", local: LocalCodeConfig{Go: roots}, want: true},
		{name: "javascript roots count as ts", local: LocalCodeConfig{JavaScript: roots}, want: true},
		{name: "disabled language roots do not count", local: LocalCodeConfig{Python: roots, CSharp: roots, DisabledLanguages: []string{"python", "cs"}}, want: false},
		{name: "one enabled language is enough", local: LocalCodeConfig{Python: roots, PHP: roots, DisabledLanguages: []string{"python"}}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CodeAnchorRootsConfigured(tt.local); got != tt.want {
				t.Fatalf("CodeAnchorRootsConfigured() = %v, want %v", got, tt.want)
			}
		})
	}
}
