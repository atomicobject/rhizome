//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/stretchr/testify/require"
)

func TestManifestReaderPreservesDiscoveryThroughReadiness(t *testing.T) {
	t.Parallel()
	vault := newRuntimeVault(t, "")
	// Block cold database initialization so the reader opens before readiness.
	release, err := sqliteutil.LockSchemaInit(context.Background(), filepath.Join(vault.root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	releaseOnce := sync.OnceFunc(release)
	defer releaseOnce()
	server := vault.start(t, "serve", "--headless", "--port", "0")
	health := vault.requireLiveRuntime(t)
	require.False(t, health.Ready)
	published, err := appruntime.ReadManifest(vault.root)
	require.NoError(t, err)
	require.False(t, published.Ready)
	reader, err := holdManifestReader(appruntime.ManifestPath(vault.root))
	require.NoError(t, err)
	defer reader.Close()
	releaseOnce()

	// Wait for all readiness publication attempts, not merely live health:
	// SetReady updates health before it tries to replace the discovery file.
	var output string
	require.Eventually(t, func() bool {
		data, readErr := os.ReadFile(appruntime.LogPath(vault.root))
		output = string(data)
		return readErr == nil && strings.Contains(output, "Rhizome ready")
	}, 60*time.Second, 50*time.Millisecond, "server must finish readiness publication")
	// A delete-sharing reader must not block replacement on any platform.
	require.NotContains(t, output, "warning: could not record readiness in the runtime manifest")
	client, ready, err := appruntime.LiveManifest(context.Background(), vault.root)
	require.NoError(t, err, "clients must still discover the server after readiness publication")
	require.Equal(t, published.RunID, ready.RunID)
	require.Equal(t, server.pid, ready.PID)
	require.True(t, ready.Ready)
	require.True(t, client.Manifest.Ready, "readiness replaces the manifest under an open reader")

	status := vault.run(nil, "index", "--status")
	require.NoError(t, status.err, status.stderr)
	require.Contains(t, status.stderr, pidLine(server.pid))
	require.Contains(t, status.stderr, "Ready:   true")
	// Keep the delete-sharing reader open while the real CLI discovers and stops
	// the process. Cleanup must remove the retained manifest before lock release.
	stopped := vault.run(nil, "stop")
	require.NoError(t, stopped.err, stopped.stderr)
	select {
	case <-server.exited:
	case <-time.After(30 * time.Second):
		t.Fatal("stop did not end the runtime while a reader held the manifest")
	}
	require.NoError(t, reader.Close())
	_, err = appruntime.ReadManifest(vault.root)
	require.ErrorIs(t, err, os.ErrNotExist)
}
