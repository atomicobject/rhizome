//go:build integration

package integration

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRuntimeCleanupProcess(t *testing.T) {
	if os.Getenv("RZM_TEST_RUNTIME_CLEANUP_PROCESS") == "1" {
		time.Sleep(time.Minute)
	}
}

func startCleanupProcess(t *testing.T) (*os.Process, <-chan struct{}) {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestRuntimeCleanupProcess$")
	command.Env = append(os.Environ(), "RZM_TEST_RUNTIME_CLEANUP_PROCESS=1")
	require.NoError(t, command.Start())
	exited := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = command.Process.Kill()
		<-exited
	})
	return command.Process, exited
}

func TestRuntimeCleanupPreservesAReusedPID(t *testing.T) {
	vault := &runtimeVault{t: t, root: t.TempDir(), spawnedPIDs: make(map[int]*os.Process)}
	original, originalExited := startCleanupProcess(t)
	vault.rememberRuntimePID(original.Pid)
	require.NoError(t, original.Kill())
	<-originalExited
	tracked := vault.spawnedPIDs[original.Pid]
	if runtime.GOOS == "windows" {
		require.NoError(t, tracked.WithHandle(func(uintptr) {}), "retain the original process handle after exit")
	}
	unrelated, unrelatedExited := startCleanupProcess(t)

	// Model PID reuse while preserving the identity captured for the original runtime.
	vault.spawnedPIDs[unrelated.Pid] = vault.spawnedPIDs[original.Pid]
	delete(vault.spawnedPIDs, original.Pid)
	vault.stopRuntime()
	if runtime.GOOS == "windows" {
		require.Error(t, tracked.WithHandle(func(uintptr) {}), "release the handle after cleanup")
	}

	select {
	case <-unrelatedExited:
		t.Fatal("runtime cleanup killed an unrelated process after PID reuse")
	default:
	}
}
