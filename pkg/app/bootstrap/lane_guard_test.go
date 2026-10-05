package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The indexing lane is the only runtime component that may take the index lock
// (SPEC-0104 US3). live_election.go is exempt: it holds the separate runtime
// election lock, not .rhizome/index.lock.
func TestOnlyTheLaneAcquiresTheIndexLock(t *testing.T) {
	allowed := map[string]bool{"live_election.go": true}

	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || allowed[name] {
			continue
		}
		source, err := os.ReadFile(filepath.Join(".", name))
		require.NoError(t, err)
		require.NotContains(t, string(source), "indexlock.TryAcquire",
			"%s must submit a lane job instead of acquiring the index lock itself", name)
	}
}
