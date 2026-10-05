package queryrecipe

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadRootsPreservesRootInspectionErrors(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "loop")
	if err := os.Symlink(path, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	recipes, issues := LoadRoots([]string{path, filepath.Join(root, "missing")})
	require.Empty(t, recipes)
	require.Len(t, issues, 1)
	require.Equal(t, "recipe_path_error", issues[0].Code)
	require.Equal(t, path, issues[0].Path)
}

func TestDefaultSourcePresenceRetainsReadErrors(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires POSIX file permission enforcement")
	}
	root := t.TempDir()
	path := filepath.Join(root, ".agents", "skills", "example", "references", "query-recipes.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("unreadable"), 0o000))
	recipes, issues := LoadDefaultSources(root)
	require.Empty(t, recipes)
	require.Len(t, issues, 1)
	require.Equal(t, "recipe_read_error", issues[0].Code)
	require.Equal(t, path, issues[0].Path)
	require.True(t, HasDefaultSources(root))
}
