package modulelifecycle

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLifecycleDescriptorRejectsUnsafeFiles(t *testing.T) {
	for _, kind := range []string{"symlink", "directory", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, ".rhizome/modules/demo")
			require.NoError(t, os.MkdirAll(dir, 0755))
			file := filepath.Join(dir, "lifecycle.yml")
			switch kind {
			case "symlink":
				outside := filepath.Join(t.TempDir(), "outside")
				require.NoError(t, os.WriteFile(outside, []byte("title: outside"), 0600))
				if err := os.Symlink(outside, file); err != nil {
					t.Skip(err)
				}
			case "directory":
				require.NoError(t, os.Mkdir(file, 0755))
			case "oversized":
				require.NoError(t, os.WriteFile(file, bytes.Repeat([]byte("x"), (64<<10)+1), 0600))
			}
			descriptors, err := Load(root)
			require.NoError(t, err)
			require.Len(t, descriptors, 1)
			require.NotEmpty(t, descriptors[0].Problem)
		})
	}
}

func TestLifecycleModuleRootCannotEscapeRepository(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0755))
	if err := os.Symlink(t.TempDir(), filepath.Join(root, ".rhizome/modules")); err != nil {
		t.Skip(err)
	}
	_, err := Load(root)
	require.Error(t, err)
}
