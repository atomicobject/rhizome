//go:build unix

package paths

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// SPEC-0136 R8: one request resolves each distinct prefix once, with the same
// results as resolving every path fresh.
func TestSymlinkCacheResolvesEachPrefixOnceAndMatchesFreshResolution(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	for _, dir := range []string{"real/deep", "other"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0755))
	}
	require.NoError(t, os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "alias")))
	require.NoError(t, os.Symlink("../other", filepath.Join(root, "real/up")))
	var inputs []string
	for i := range 20 {
		name := filepath.Join(root, "real/deep", string(rune('a'+i))+".md")
		require.NoError(t, os.WriteFile(name, nil, 0644))
		inputs = append(inputs, name, filepath.Join(root, "alias/deep", string(rune('a'+i))+".md"))
	}
	inputs = append(inputs, filepath.Join(root, "real/up/missing.md"), filepath.Join(root, "missing/x.md"), filepath.Join(root, "real/deep/a.md/not-a-dir"))

	vault, err := NewVaultPaths(root)
	require.NoError(t, err)
	links := NewSymlinkCache()
	for _, input := range inputs {
		fresh, freshErr := vault.RelPhysicalStrict(input)
		cached, cachedErr := vault.RelPhysicalStrictCached(input, links)
		require.Equal(t, fresh, cached, input)
		require.Equal(t, freshErr == nil, cachedErr == nil, input)
	}
	prefixes := map[string]bool{}
	for _, input := range inputs {
		for p := filepath.Dir(input); p != filepath.Dir(p); p = filepath.Dir(p) {
			prefixes[p] = true
		}
	}
	// Each file plus each distinct directory, at most, however many files share it.
	require.LessOrEqual(t, links.Resolutions(), len(prefixes)+len(inputs))
	before := links.Resolutions()
	for _, input := range inputs {
		_, _ = vault.RelPhysicalStrictCached(input, links)
	}
	require.Equal(t, before, links.Resolutions(), "a repeated read resolves nothing new")
}
