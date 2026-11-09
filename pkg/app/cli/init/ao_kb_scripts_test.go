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

// TestAoKbScripts runs the ao-kb module's Node script tests.
// CI must provide Node 22 or later, so there a missing Node fails the test.
func TestAoKbScripts(t *testing.T) {
	node, err := exec.LookPath("node")
	if err == nil {
		err = requireNode22(node)
	}
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("CI must provide Node 22 or later for the ao-kb script tests: %v", err)
		}
		t.Skipf("ao-kb script tests need Node 22 or later: %v", err)
	}

	// Go caches test results by the files a test reads; Node reads these, so
	// read them here too or an edited script would reuse a cached pass.
	for _, dir := range []string{"testdata/ao-kb", "templates/starters/ao-kb", "../../../../testdata/ao-kb-legacy", "../../../../testdata/ao-kb-model/account", "../../../../testdata/ao-kb-model/intermediate.json"} {
		require.NoError(t, filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			_, err = os.ReadFile(path)
			return err
		}))
	}

	// node --test does not search a directory argument, so list the files.
	tests, err := filepath.Glob(filepath.Join("testdata", "ao-kb", "*.test.mjs"))
	require.NoError(t, err)
	require.NotEmpty(t, tests)
	out, err := exec.Command(node, append([]string{"--experimental-strip-types", "--test"}, tests...)...).CombinedOutput()
	require.NoError(t, err, "node --test failed:\n%s", out)
}

func requireNode22(node string) error {
	out, err := exec.Command(node, "--version").Output()
	if err != nil {
		return err
	}
	version := strings.TrimSpace(string(out))
	major, err := strconv.Atoi(strings.SplitN(strings.TrimPrefix(version, "v"), ".", 2)[0])
	if err != nil {
		return err
	}
	if major < 22 {
		return fmt.Errorf("found Node %s", version)
	}
	return nil
}
