//go:build unix

package noteownership

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// SPEC-0136 R8: a read selecting many notes through one links cache resolves
// each distinct directory once, and selects exactly what fresh resolution does,
// including a path that reaches a code root only through a symlink.
func TestSelectCachedResolvesEachDirectoryOncePerRead(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	var inputs []paths.RelPath
	for _, dir := range []string{"notes/a", "notes/b", "src"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0o755))
		for i := range 25 {
			rel := fmt.Sprintf("%s/n%02d.md", dir, i)
			require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte("# n"), 0o600))
			inputs = append(inputs, paths.RelPath(rel))
		}
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "src/main.go"), []byte("package main"), 0o600))
	require.NoError(t, os.Symlink(filepath.Join(root, "src"), filepath.Join(root, "alias")))
	inputs = append(inputs, "src/main.go", "alias/main.go", "alias/n00.md", "notes/missing/x.md")

	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	selector, err := CompileSelector(SelectorInput{
		VaultDefinition: obsidian.VaultDefinition{Root: root, Includes: []string{"notes/**", "src/**", "alias/**"}},
		Registry:        runtime.Registry(),
		CodeRoots:       []paths.AbsPath{paths.AbsPath(filepath.Join(root, "src"))},
		CodeLanguage: func(ref paths.CodePathRef) codeanchor.Lang {
			if filepath.Ext(ref.Abs.String()) == ".go" {
				return "go"
			}
			return ""
		},
	})
	require.NoError(t, err)

	links := paths.NewSymlinkCache()
	for _, input := range inputs {
		fresh, freshErr := selector.Select(input)
		cached, cachedErr := selector.SelectCached(input, links)
		require.Equal(t, freshErr, cachedErr, input)
		require.Equal(t, fresh, cached, input)
	}
	aliasCode, err := selector.SelectCached("alias/main.go", links)
	require.NoError(t, err)
	require.True(t, aliasCode.CodeCandidate, "an alias into a code root is still a code candidate")

	prefixes := map[string]bool{}
	for _, input := range inputs {
		for p := filepath.Dir(filepath.Join(root, input.String())); p != filepath.Dir(p); p = filepath.Dir(p) {
			prefixes[p] = true
		}
	}
	// Each file plus each distinct directory at most, however many files share it.
	require.Positive(t, links.Resolutions())
	require.LessOrEqual(t, links.Resolutions(), len(prefixes)+len(inputs))
	before := links.Resolutions()
	for _, input := range inputs {
		_, _ = selector.SelectCached(input, links)
	}
	require.Equal(t, before, links.Resolutions(), "a repeated selection resolves nothing new")
}
