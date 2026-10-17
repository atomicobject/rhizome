package appbundle

import (
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveBundleThroughSymlink(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Rhizome.app")
	executable := filepath.Join(root, "Contents", "MacOS", "rzm")
	require.NoError(t, os.MkdirAll(filepath.Dir(executable), 0o755))
	require.NoError(t, os.WriteFile(executable, []byte("binary"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Contents", "Info.plist"), []byte(`<plist><dict><key>CFBundleIdentifier</key><string>com.atomicobject.rhizome.dev</string><key>CFBundleShortVersionString</key><string>1.2.3</string></dict></plist>`), 0o644))
	link := filepath.Join(t.TempDir(), "rzm")
	require.NoError(t, os.Symlink(executable, link))
	info, err := Resolve(link)
	require.NoError(t, err)
	resolved, err := filepath.EvalSymlinks(executable)
	require.NoError(t, err)
	require.Equal(t, resolved, info.ExecutablePath)
	require.Equal(t, filepath.Dir(filepath.Dir(filepath.Dir(resolved))), info.Root)
	require.Equal(t, "1.2.3", info.Version)
	require.True(t, info.Dev)
	require.Equal(t, root, Root(executable))
	require.Empty(t, Root(filepath.Join(root, "Elsewhere", "rzm")))
	plain, err := Resolve(t.TempDir())
	require.NoError(t, err)
	require.Nil(t, plain)
	require.NoError(t, os.WriteFile(filepath.Join(root, "Contents", "Info.plist"), []byte(`<plist><dict/></plist>`), 0o644))
	_, err = Resolve(link)
	require.ErrorContains(t, err, "requires")
}

func TestReadBinaryPlistUsesValueReader(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Rhizome.app")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "Contents"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Contents", "Info.plist"), []byte("bplist fixture"), 0o644))
	original := plistValue
	t.Cleanup(func() { plistValue = original })
	plistValue = func(path, key string) ([]byte, error) {
		require.Equal(t, filepath.Join(root, "Contents", "Info.plist"), path)
		if key == "CFBundleIdentifier" {
			return []byte("com.atomicobject.rhizome"), nil
		}
		require.Equal(t, "CFBundleShortVersionString", key)
		return []byte("1.2.3\n"), nil
	}
	info, err := Read(root)
	require.NoError(t, err)
	require.Equal(t, "1.2.3", info.Version)
	require.False(t, info.Dev)
}
