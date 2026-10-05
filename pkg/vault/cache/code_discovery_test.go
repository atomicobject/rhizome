package cache

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/stretchr/testify/require"
)

type observedCodeFS struct {
	fs.FS
	directories []string
}

func (f *observedCodeFS) ReadDir(name string) ([]fs.DirEntry, error) {
	f.directories = append(f.directories, name)
	return fs.ReadDir(f.FS, name)
}

func TestCodeDiscoveryPrunesExcludedDirectories(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".gitignore", ".gocache/\nignored-link/\n")
	writeFile(t, root, "src/keep.go", "package keep")
	writeFile(t, root, "src/keep.ts", "export {}")
	writeFile(t, root, ".gocache/deep/generated.go", "package ignored")
	writeFile(t, root, "generated-deps/deep/generated.ts", "export {}")
	require.NoError(t, os.Symlink(filepath.Join(root, ".gocache"), filepath.Join(root, "ignored-link")))
	service, err := NewService(root, Options{CodeRefConfig: &coderefs.Config{
		Enabled:  true,
		Includes: []string{"**/*.go", "**/*.ts", ".gocache/deep/*.go", "ignored-link/**/*.go"},
		Excludes: []string{"**/generated-deps/**"},
	}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = service.Close() })
	service.loadIgnorePatterns()
	files := &observedCodeFS{FS: os.DirFS(root)}
	got, err := service.discoverCodeFilesIn(files)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"src/keep.go", "src/keep.ts"}, got)
	require.NotContains(t, files.directories, ".gocache", "ignored build caches must not delay note workspace reads")
	require.NotContains(t, files.directories, ".gocache/deep", "literal glob prefixes must still respect ignored ancestors")
	require.NotContains(t, files.directories, "ignored-link", "ignored directory symlinks must be pruned before traversal")
	require.NotContains(t, files.directories, "generated-deps", "excluded dependencies must be pruned before traversal")
}

func TestCodeDiscoveryPreservesGlobAndIgnoreSemantics(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".gitignore", "ignored/\n")
	writeFile(t, root, ".rhizome/ignore", "!/ignored/keep/\n")
	writeFile(t, root, "ignored/keep/.gitignore", "generated/\n")
	for _, name := range []string{
		"src/keep.go", "src/keep.ts", "src/drop.go", "src/nested/lib.ts",
		"name-only/keep.go", "subtree/drop.go", "subtree/deep/drop.ts",
		"ignored/drop.go", "ignored/keep/keep.go", "ignored/keep/generated/drop.go",
		".tools/keep.go", "notes/CONTEXT.md", "notes/hidden.md",
	} {
		writeFile(t, root, name, "// source")
	}
	require.NoError(t, os.Symlink(filepath.Join(root, "src/nested"), filepath.Join(root, "linked")))
	service, err := NewService(root, Options{
		UserExcludes: []string{"notes/"},
		CodeRefConfig: &coderefs.Config{
			Enabled:  true,
			Includes: []string{"**/*.{go,ts}", "src/keep.go", "src/**", "linked/*.ts", "notes/*.md", "[invalid"},
			Excludes: []string{"**/drop.go", "subtree/**", "name-only"},
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = service.Close() })
	service.loadIgnorePatterns()
	got, err := service.discoverCodeFiles()
	require.NoError(t, err)
	require.ElementsMatch(t, []string{
		"src/keep.go", "src/keep.ts", "src/nested/lib.ts", "name-only/keep.go",
		"ignored/keep/keep.go", ".tools/keep.go", "linked/lib.ts", "notes/CONTEXT.md",
	}, got)
}
