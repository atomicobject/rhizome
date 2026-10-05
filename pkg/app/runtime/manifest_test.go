package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testManifest(t *testing.T, vaultPath string, pid int) InstanceManifest {
	t.Helper()
	token, err := NewControlToken()
	require.NoError(t, err)
	runID, err := NewRunID()
	require.NoError(t, err)
	return InstanceManifest{
		InstanceID:   InstanceID(vaultPath),
		RunID:        runID,
		VaultName:    "Vault A",
		VaultPath:    vaultPath,
		PID:          pid,
		Version:      "test",
		Mode:         ModeHeadless,
		BuildID:      "build-1",
		ControlToken: token,
		HTTPHost:     "127.0.0.1",
		StartedAt:    time.Now().UTC(),
	}
}

func TestWriteManifestIsOwnerOnlyAndReplacesWithoutTempLeak(t *testing.T) {
	vaultPath := t.TempDir()
	manifest := testManifest(t, vaultPath, os.Getpid())
	require.NoError(t, WriteManifest(vaultPath, manifest))
	manifest.Ready = true
	require.NoError(t, WriteManifest(vaultPath, manifest))

	entries, err := os.ReadDir(filepath.Join(vaultPath, ".rhizome"))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, ManifestFileName, entries[0].Name())
	if runtime.GOOS != "windows" {
		info, err := os.Stat(ManifestPath(vaultPath))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	read, err := ReadManifest(vaultPath)
	require.NoError(t, err)
	require.True(t, read.Ready)
	require.Equal(t, manifest.ControlToken, read.ControlToken)
}

func TestRemoveManifestIfOwnerKeepsAnotherProcessManifest(t *testing.T) {
	vaultPath := t.TempDir()
	winner := testManifest(t, vaultPath, os.Getpid()+1000)
	require.NoError(t, WriteManifest(vaultPath, winner))

	require.NoError(t, RemoveManifestIfOwner(vaultPath, os.Getpid()))
	_, err := os.Stat(ManifestPath(vaultPath))
	require.NoError(t, err, "loser must not remove the winner's manifest")

	require.NoError(t, RemoveManifestIfOwner(vaultPath, winner.PID))
	_, err = os.Stat(ManifestPath(vaultPath))
	require.True(t, os.IsNotExist(err))
	require.NoError(t, RemoveManifestIfOwner(vaultPath, winner.PID), "idempotent")
}

func healthServer(t *testing.T, health Health) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != HealthPath {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(health)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestProbeRejectsManifestWhosePortWasReused(t *testing.T) {
	vaultPath := t.TempDir()
	manifest := testManifest(t, vaultPath, os.Getpid())
	srv := healthServer(t, Health{InstanceID: manifest.InstanceID, RunID: "another-run", PID: manifest.PID})
	manifest.HTTPURL = srv.URL

	_, err := Probe(context.Background(), manifest)
	require.ErrorIs(t, err, ErrManifestMismatch)
}

func TestProbeDistinguishesRefusedFromBooting(t *testing.T) {
	vaultPath := t.TempDir()
	manifest := testManifest(t, vaultPath, os.Getpid())
	manifest.HTTPURL = "http://127.0.0.1:1"
	_, err := Probe(context.Background(), manifest)
	require.ErrorIs(t, err, ErrNoRuntime, "connection refused means the writer is gone")

	booting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer booting.Close()
	manifest.HTTPURL = booting.URL
	_, err = Probe(context.Background(), manifest)
	require.ErrorIs(t, err, ErrRuntimeUnresponsive, "listening but not healthy means wait")
}

func TestLiveManifestStates(t *testing.T) {
	ctx := context.Background()

	t.Run("missing manifest", func(t *testing.T) {
		_, _, err := LiveManifest(ctx, t.TempDir())
		require.ErrorIs(t, err, ErrNoRuntime)
	})

	t.Run("dead pid is absent", func(t *testing.T) {
		vaultPath := t.TempDir()
		manifest := testManifest(t, vaultPath, 2147483000)
		manifest.HTTPURL = "http://127.0.0.1:1"
		require.NoError(t, WriteManifest(vaultPath, manifest))
		_, _, err := LiveManifest(ctx, vaultPath)
		require.ErrorIs(t, err, ErrNoRuntime)
	})

	t.Run("alive pid, nobody listening: absent (reused pid or crashed after unlisten)", func(t *testing.T) {
		vaultPath := t.TempDir()
		manifest := testManifest(t, vaultPath, os.Getpid())
		manifest.HTTPURL = "http://127.0.0.1:1"
		require.NoError(t, WriteManifest(vaultPath, manifest))
		_, _, err := LiveManifest(ctx, vaultPath)
		require.ErrorIs(t, err, ErrNoRuntime)
	})

	t.Run("alive pid, listening but unhealthy: unresponsive, wait rather than spawn", func(t *testing.T) {
		vaultPath := t.TempDir()
		manifest := testManifest(t, vaultPath, os.Getpid())
		booting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer booting.Close()
		manifest.HTTPURL = booting.URL
		require.NoError(t, WriteManifest(vaultPath, manifest))
		_, _, err := LiveManifest(ctx, vaultPath)
		require.ErrorIs(t, err, ErrRuntimeUnresponsive)
	})

	t.Run("another process answering is absent", func(t *testing.T) {
		vaultPath := t.TempDir()
		manifest := testManifest(t, vaultPath, os.Getpid())
		srv := healthServer(t, Health{InstanceID: manifest.InstanceID, RunID: "other", PID: manifest.PID})
		manifest.HTTPURL = srv.URL
		require.NoError(t, WriteManifest(vaultPath, manifest))
		_, _, err := LiveManifest(ctx, vaultPath)
		require.ErrorIs(t, err, ErrNoRuntime)
	})

	t.Run("healthy probe attaches", func(t *testing.T) {
		vaultPath := t.TempDir()
		manifest := testManifest(t, vaultPath, os.Getpid())
		srv := healthServer(t, Health{InstanceID: manifest.InstanceID, RunID: manifest.RunID, PID: manifest.PID, Ready: true})
		manifest.HTTPURL = srv.URL
		require.NoError(t, WriteManifest(vaultPath, manifest))
		client, health, err := LiveManifest(ctx, vaultPath)
		require.NoError(t, err)
		require.True(t, health.Ready)
		req, err := client.NewRequest(ctx, http.MethodPost, ShutdownPath, map[string]string{"reason": "test"})
		require.NoError(t, err)
		require.Equal(t, manifest.ControlToken, BearerToken(req))
		require.Equal(t, "application/json", req.Header.Get("Content-Type"))
	})
}

func TestBuildIDChangesWithExecutableAndVersion(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "rzm")
	require.NoError(t, os.WriteFile(exe, []byte("one"), 0o755))
	first := BuildID("dev", exe)
	require.Equal(t, first, BuildID("dev", exe))
	require.NotEqual(t, first, BuildID("v1", exe))
	require.NoError(t, os.WriteFile(exe, []byte("two-bytes"), 0o755))
	require.NoError(t, os.Chtimes(exe, time.Now().Add(time.Hour), time.Now().Add(time.Hour)))
	require.NotEqual(t, first, BuildID("dev", exe))
}

func TestManifestWriteDoesNotRemoveTargetAfterRenameFailure(t *testing.T) {
	vault := t.TempDir()
	// A directory makes rename fail deterministically on every platform.
	// Publication must report that failure without deleting the existing target.
	require.NoError(t, os.MkdirAll(ManifestPath(vault), 0o755))
	require.Error(t, WriteManifest(vault, testManifest(t, vault, os.Getpid())))
	info, err := os.Stat(ManifestPath(vault))
	require.NoError(t, err)
	require.True(t, info.IsDir())
	entries, err := os.ReadDir(filepath.Dir(ManifestPath(vault)))
	require.NoError(t, err)
	require.Len(t, entries, 1, "failed publication must clean up its temporary file")
}
