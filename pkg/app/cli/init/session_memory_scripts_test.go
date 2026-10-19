package init

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSessionMemoryScripts runs the session-memory skill's Node script tests.
// CI must provide Node 20 or later, so there a missing Node fails the test.
func TestSessionMemoryScripts(t *testing.T) {
	node, err := exec.LookPath("node")
	if err == nil {
		err = requireNode20(node)
	}
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("CI must provide Node 20 or later for the session-memory script tests: %v", err)
		}
		t.Skipf("session-memory script tests need Node 20 or later: %v", err)
	}

	// Go caches test results by the files a test reads; Node reads these, so
	// read them here too or an edited script would reuse a cached pass.
	for _, dir := range []string{"testdata/session-memory", "templates/starters/session-memory"} {
		require.NoError(t, filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			_, err = os.ReadFile(path)
			return err
		}))
	}

	// node --test does not search a directory argument, so list the files.
	tests, err := filepath.Glob(filepath.Join("testdata", "session-memory", "*.test.mjs"))
	require.NoError(t, err)
	require.NotEmpty(t, tests)
	out, err := exec.Command(node, append([]string{"--test"}, tests...)...).CombinedOutput()
	require.NoError(t, err, "node --test failed:\n%s", out)
}

func requireNode20(node string) error {
	out, err := exec.Command(node, "--version").Output()
	if err != nil {
		return err
	}
	version := strings.TrimSpace(string(out))
	major, err := strconv.Atoi(strings.SplitN(strings.TrimPrefix(version, "v"), ".", 2)[0])
	if err != nil {
		return err
	}
	if major < 20 {
		return fmt.Errorf("found Node %s", version)
	}
	return nil
}

func TestStarterTemplatesShipNoScriptTests(t *testing.T) {
	require.NoError(t, fs.WalkDir(agentHelperTemplates, "templates", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		require.NotContains(t, path, "testdata", "test fixtures belong under pkg/app/cli/init/testdata")
		require.False(t, strings.HasSuffix(path, ".test.mjs"), "%s would be installed downstream", path)
		return nil
	}))
}
