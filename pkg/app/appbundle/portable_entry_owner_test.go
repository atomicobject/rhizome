//go:build !windows

package appbundle

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPortableLinuxEntryRefusesMissingOrDisabledOwner(t *testing.T) {
	for _, scenario := range []string{"missing", "disabled"} {
		t.Run(scenario, func(t *testing.T) {
			root, record, image := portableFixture(t)
			entry := filepath.Join(t.TempDir(), "rzm")
			require.NoError(t, os.Symlink(image, entry))
			absolute := filepath.Join(filepath.Dir(entry), "absolute-alias")
			relative := filepath.Join(filepath.Dir(entry), "relative-alias")
			require.NoError(t, os.Symlink(entry, absolute))
			require.NoError(t, os.Symlink(filepath.Base(absolute), relative))
			if scenario == "missing" {
				require.NoError(t, os.Remove(record.AppPath))
			} else {
				record.Enabled = false
				writePortableFixture(t, root, record)
			}
			for _, invoked := range []string{entry, absolute, relative} {
				t.Run(filepath.Base(invoked), func(t *testing.T) {
					var stderr bytes.Buffer
					handled, code := dispatchEntry(image, invoked, entry, []string{"--version"}, nil, &bytes.Buffer{}, &stderr)
					require.True(t, handled)
					require.Equal(t, 1, code)
					if scenario == "missing" {
						require.Contains(t, stderr.String(), "missing")
					} else {
						require.Contains(t, stderr.String(), "integration is unavailable")
					}
				})
			}
		})
	}
}

func TestPortableLinuxEntryRecognizesOriginalInvocation(t *testing.T) {
	root, _, image := portableFixture(t)
	dir := t.TempDir()
	entry := filepath.Join(dir, "bin", "rzm")
	require.NoError(t, os.MkdirAll(filepath.Dir(entry), 0o700))
	require.NoError(t, os.Symlink(image, entry))
	local := filepath.Join(dir, "repo", "rzm")
	require.NoError(t, os.MkdirAll(filepath.Dir(local), 0o700))
	require.NoError(t, os.Symlink(image, local))
	absolute := filepath.Join(dir, "absolute-alias")
	relative := filepath.Join(dir, "relative-alias")
	require.NoError(t, os.Symlink(entry, absolute))
	require.NoError(t, os.Symlink(filepath.Base(absolute), relative))
	t.Chdir(dir)
	t.Setenv("PATH", filepath.Dir(entry))
	for _, invocation := range []string{entry, filepath.Join("bin", "rzm"), "rzm", absolute, relative, "./" + filepath.Base(relative)} {
		require.Equal(t, root, invokedImageEntryRoot(image, invocation, entry), invocation)
	}
	require.Empty(t, invokedImageEntryRoot(image, local, entry), "a repository-local alias is a separate entry")
	require.Empty(t, invokedImageEntryRoot(image, image, entry), "runtime children execute their immutable image directly")
	t.Setenv("PATH", filepath.Dir(local))
	require.Empty(t, invokedImageEntryRoot(image, "rzm", entry), "PATH precedence must not turn a repository-local alias into the shared entry")
	cycleA, cycleB := filepath.Join(dir, "cycle-a"), filepath.Join(dir, "cycle-b")
	require.NoError(t, os.Symlink(cycleB, cycleA))
	require.NoError(t, os.Symlink(cycleA, cycleB))
	require.Empty(t, invokedImageEntryRoot(image, cycleA, entry), "an alias cycle must terminate without claiming the entry")
	foreign := filepath.Join(dir, "foreign-rzm")
	require.NoError(t, os.WriteFile(foreign, []byte("standalone"), 0o700))
	require.NoError(t, os.Remove(entry))
	require.NoError(t, os.Symlink(foreign, entry))
	require.Empty(t, invokedImageEntryRoot(foreign, entry, entry), "a foreign shared entry is not owned by the app")
	require.Empty(t, invokedImageEntryRoot(foreign, relative, entry), "chaining through a foreign entry does not make it app-owned")
}
