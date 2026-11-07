package desktop

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/appbundle"
	"github.com/stretchr/testify/require"
)

func TestPortableCLILinkShellSkipsNonExecutablePATHEntries(t *testing.T) {
	dir := t.TempDir()
	working := workingScript(t, filepath.Join(dir, "working", "rzm"))
	blocked := filepath.Join(dir, "blocked", "rzm")
	require.NoError(t, os.MkdirAll(filepath.Dir(blocked), 0o755))
	require.NoError(t, os.WriteFile(blocked, []byte("not executable"), 0o644))

	s := testService(t)
	require.NoError(t, os.MkdirAll(s.stateDir, 0o755))
	s.env.LookupEnv = func(key string) (string, bool) {
		return filepath.Dir(blocked) + string(os.PathListSeparator) + filepath.Dir(working), key == "PATH"
	}
	previousEntryPath := portableCLIEntryPath
	portableCLIEntryPath = func() (string, error) { return filepath.Join(dir, "owned", "rzm"), nil }
	t.Cleanup(func() { portableCLIEntryPath = previousEntryPath })

	result := s.portableCLILinkShell(context.Background(), &appbundle.Info{
		ExecutablePath: filepath.Join(dir, "image", "rzm"),
		Portable:       true,
	})
	require.Equal(t, CLILinkShell{Path: working, Version: "v1.2.3"}, result)
}
