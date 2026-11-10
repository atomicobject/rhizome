package modulereadiness

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUntrustedStartupCannotWriteOutsideTheRepository(t *testing.T) {
	for _, target := range []string{"module-readiness.json.tmp", "module-readiness.json", "state"} {
		t.Run(target, func(t *testing.T) {
			root := fixture(t)
			outside := t.TempDir()
			sentinel := filepath.Join(outside, "sentinel")
			require.NoError(t, os.WriteFile(sentinel, []byte("keep me"), 0600))
			require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome/state"), 0700))
			link := filepath.Join(root, ".rhizome/state", target)
			to := sentinel
			if target == "state" {
				link = filepath.Join(root, ".rhizome/state")
				require.NoError(t, os.Remove(link))
				to = outside
			}
			if err := os.Symlink(to, link); err != nil {
				t.Skip(err)
			}
			c := New(Options{Root: root, Trust: func(context.Context) string { return "not trusted" }})
			c.scan(context.Background(), nil)
			data, err := os.ReadFile(sentinel)
			require.NoError(t, err)
			require.Equal(t, "keep me", string(data))
			if target == "state" {
				require.NoFileExists(t, filepath.Join(outside, "module-readiness.json"))
			}
		})
	}
}
