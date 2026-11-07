package runtime

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/stretchr/testify/require"
)

// A runtime that republishes over a dead runtime's registration while pruning
// judges the old one stale must keep its file.
func TestPruneStaleKeepsRegistrationRewrittenAfterRead(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv
	env.HomeDir = t.TempDir()
	registry, err := NewRegistry(env)
	require.NoError(t, err)
	require.NoError(t, registry.Write(InstanceManifest{InstanceID: "vault", PID: 1 << 30, RunID: "dead-a"}))
	registry.pidAlive = func(int) bool {
		// Runtime B publishes between pruning's read and its removal.
		require.NoError(t, registry.Write(InstanceManifest{InstanceID: "vault", PID: os.Getpid(), RunID: "live-b"}))
		return false
	}

	require.NoError(t, registry.PruneStale())

	data, err := os.ReadFile(registry.Path("vault"))
	require.NoError(t, err, "live runtime's registration was deleted")
	var manifest InstanceManifest
	require.NoError(t, json.Unmarshal(data, &manifest))
	require.Equal(t, "live-b", manifest.RunID)
}
