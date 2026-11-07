package runtime

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"syscall"
	"testing"
	"time"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

type deniedTransport struct{ err error }

func (d deniedTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, d.err }

func TestEnsureReturnsAccessDenialWithoutWaitingOrStarting(t *testing.T) {
	for _, cause := range []error{syscall.EPERM, syscall.EACCES} {
		t.Run(cause.Error(), func(t *testing.T) {
			original := probeHTTPClient
			probeHTTPClient = &http.Client{Transport: deniedTransport{cause}}
			t.Cleanup(func() { probeHTTPClient = original })
			root := t.TempDir()
			manifest := testManifest(t, root, os.Getpid())
			manifest.HTTPURL = "http://127.0.0.1:12345"
			require.NoError(t, WriteManifest(root, manifest))
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err := Ensure(ctx, EnsureOptions{VaultPath: root, Autostart: true, Wait: true, Budget: time.Minute})
			require.ErrorIs(t, err, cause, "preserve the actionable OS permission failure")
			require.NotErrorIs(t, err, ErrNoRuntime, "denied access does not establish absence")
			require.False(t, errors.Is(err, context.DeadlineExceeded), "denial must not poll until the deadline")
			require.Contains(t, err.Error(), "local network access")
			require.NoFileExists(t, SpawnLockPath(root), "denied discovery must not attempt a competing spawn")
		})
	}
}

func TestEnsurePreservesDeniedProcessStart(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("Unix executable permission fixture")
	}
	root := newTestVault(t)
	executable := filepath.Join(root, "denied-rzm")
	require.NoError(t, os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o600))
	_, err := Ensure(context.Background(), EnsureOptions{VaultPath: root, Executable: executable, Autostart: true, Wait: true})
	require.ErrorIs(t, err, ErrRuntimeAccessDenied)
	require.ErrorIs(t, err, os.ErrPermission)
}

func TestPostSpawnProbeReturnsPermissionDenial(t *testing.T) {
	root := newTestVault(t)
	manifest := testManifest(t, root, os.Getpid())
	manifest.HTTPURL = "http://127.0.0.1:12345"
	require.NoError(t, WriteManifest(root, manifest))
	original := probeHTTPClient
	probeHTTPClient = &http.Client{Transport: deniedTransport{syscall.EPERM}}
	t.Cleanup(func() { probeHTTPClient = original })
	err := waitProbeReady(context.Background(), root, time.Now().Add(time.Second))
	require.ErrorIs(t, err, ErrRuntimeAccessDenied)
	require.ErrorIs(t, err, syscall.EPERM)
}

func TestFailedSpawnCleanupHonorsRequestDeadline(t *testing.T) {
	root := newTestVault(t)
	registry, err := NewRegistry(vaultconfig.SystemEnv())
	require.NoError(t, err)
	original := spawnArgv
	var release func() error
	spawnArgv = func(_, _ string) []string {
		var acquired bool
		var lockErr error
		release, acquired, lockErr = indexlock.TryAcquire(registry.intentGatePath(root))
		require.NoError(t, lockErr)
		require.True(t, acquired)
		return []string{filepath.Join(root, "missing-executable")}
	}
	t.Cleanup(func() {
		spawnArgv = original
		if release != nil {
			_ = release()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := Ensure(ctx, EnsureOptions{VaultPath: root, Autostart: true, Wait: true})
		done <- err
	}()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("failed-spawn cleanup ignored the request deadline while the intent gate was held")
	}
}
