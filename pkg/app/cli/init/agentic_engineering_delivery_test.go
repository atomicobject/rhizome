package init

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgenticEngineeringDeliveryLibrary(t *testing.T) {
	node, err := exec.LookPath("node")
	require.NoError(t, err, "delivery library tests require pinned Node")
	require.NoError(t, requireNode22(node))
	// Read the Node inputs so Go invalidates cached results when shipped modules change.
	for _, dir := range []string{"testdata/agentic-engineering", "templates/starters/agentic-engineering/module/views/delivery/lib"} {
		require.NoError(t, filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			_, err = os.ReadFile(path)
			return err
		}))
	}
	tests, err := filepath.Glob("testdata/agentic-engineering/*.test.mjs")
	require.NoError(t, err)
	require.NotEmpty(t, tests)
	out, err := exec.Command(node, append([]string{"--experimental-strip-types", "--test"}, tests...)...).CombinedOutput()
	require.NoError(t, err, "delivery library tests failed:\n%s", out)
}
