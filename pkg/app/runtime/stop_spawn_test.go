package runtime

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/require"
)

const pausedSpawnEnv = "RZM_TEST_PAUSED_SPAWN"

func TestStopWaitsForSpawnedChildBeforeElection(t *testing.T) {
	vault := newTestVault(t)
	signals := t.TempDir()
	t.Setenv(pausedSpawnEnv, signals)
	previous := spawnArgv
	t.Cleanup(func() { spawnArgv = previous })
	spawnArgv = func(_, _ string) []string {
		return []string{testBinary(t), "-test.run=^TestHelperPausedSpawn$"}
	}

	result, err := Ensure(t.Context(), EnsureOptions{
		VaultPath: vault, Executable: testBinary(t), Autostart: true, Wait: false, Budget: time.Millisecond,
	})
	require.NoError(t, err)
	require.True(t, result.Spawned)
	t.Cleanup(func() { _ = os.WriteFile(filepath.Join(signals, "release"), nil, 0o600) })
	var childPID int
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(filepath.Join(signals, "ready"))
		if err != nil {
			return false
		}
		childPID, err = strconv.Atoi(string(data))
		return err == nil && !liveSpawnLease(vault)
	}, 15*time.Second, 10*time.Millisecond)
	registry, err := NewRegistry(vaultconfig.FromContext(t.Context()))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.WriteFile(filepath.Join(signals, "release"), nil, 0o600)
		// Only a failed test needs this fallback; production spawning owns reaping.
		if pidExists(childPID) {
			process, err := os.FindProcess(childPID)
			if err == nil {
				_ = process.Kill()
				_, _ = process.Wait()
			}
		}
	})
	_, live := LockOwner(vault)
	require.False(t, live, "the child is paused before runtime election")

	stopped := make(chan error, 1)
	go func() {
		stopped <- Stop(context.Background(), StopOptions{VaultPath: vault, Out: io.Discard, Grace: 5 * time.Second})
	}()
	require.Eventually(t, func() bool {
		intent, found, err := registry.StartIntentFor(t.Context(), vault)
		return err == nil && found && intent.Cancelled
	}, time.Second, 10*time.Millisecond)
	select {
	case err := <-stopped:
		t.Fatalf("Stop returned while its child could still write: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	require.NoError(t, os.WriteFile(filepath.Join(signals, "release"), nil, 0o600))
	select {
	case err := <-stopped:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not finish after the child exited")
	}
	require.FileExists(t, filepath.Join(vault, ".rhizome", "late-write"))
	require.False(t, pidExists(childPID), "Stop must wait until the child can no longer write")
}

func TestHelperPausedSpawn(t *testing.T) {
	signals := os.Getenv(pausedSpawnEnv)
	if signals == "" {
		t.Skip("helper process")
	}
	if err := os.WriteFile(filepath.Join(signals, "ready"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		os.Exit(1)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(signals, "release")); err == nil {
			if err := os.WriteFile(filepath.Join(".rhizome", "late-write"), nil, 0o600); err != nil {
				os.Exit(1)
			}
			os.Exit(0)
		}
		time.Sleep(10 * time.Millisecond)
	}
	os.Exit(1)
}
