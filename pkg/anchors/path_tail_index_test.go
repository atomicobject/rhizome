package codeanchor

import (
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestPathTailIndex_UniqueAndAmbiguous(t *testing.T) {
	idx := NewPathTailIndex(5)
	a := filepath.Join(t.TempDir(), "apps", "web", "src", "components", "Button.tsx")
	b := filepath.Join(t.TempDir(), "apps", "admin", "src", "components", "Button.tsx")

	idx.Add(a)
	got, ok := idx.Query(TailQuery{Tail: "components/Button", AllowedExts: []string{".tsx"}})
	require.True(t, ok)
	require.Equal(t, paths.ResolveSymlinks(a).String(), got)

	idx.Add(b)
	_, ok = idx.Query(TailQuery{Tail: "components/Button", AllowedExts: []string{".tsx"}})
	require.False(t, ok, "should be ambiguous when multiple matches exist")

	// PreferUnder can disambiguate.
	got, ok = idx.Query(TailQuery{
		Tail:        "components/Button",
		AllowedExts: []string{".tsx"},
		PreferUnder: []string{filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(a))))}, // .../apps/web
	})
	require.True(t, ok)
	require.Equal(t, paths.ResolveSymlinks(a).String(), got)

	// Removing restores uniqueness.
	idx.Remove(b)
	got, ok = idx.Query(TailQuery{Tail: "components/Button", AllowedExts: []string{".tsx"}})
	require.True(t, ok)
	require.Equal(t, paths.ResolveSymlinks(a).String(), got)
}

func TestPathTailIndex_ExtensionFiltering(t *testing.T) {
	idx := NewPathTailIndex(3)
	a := filepath.Join(t.TempDir(), "src", "util", "index.ts")
	b := filepath.Join(t.TempDir(), "src", "util", "index.js")
	idx.Add(a)
	idx.Add(b)

	got, ok := idx.Query(TailQuery{Tail: "util/index", AllowedExts: []string{".ts"}})
	require.True(t, ok)
	require.Equal(t, paths.ResolveSymlinks(a).String(), got)

	got, ok = idx.Query(TailQuery{Tail: "util/index.ts", AllowedExts: []string{".ts"}})
	require.True(t, ok)
	require.Equal(t, paths.ResolveSymlinks(a).String(), got)
}
