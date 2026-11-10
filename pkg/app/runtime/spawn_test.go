package runtime

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSpawnArgvIsHeadlessServeForTheVaultRoot(t *testing.T) {
	require.Equal(t,
		[]string{"/usr/local/bin/rzm", "serve", "--headless", "--vault", "/vaults/notes"},
		spawnArgv("/usr/local/bin/rzm", "/vaults/notes"),
	)
}

func TestSpawnHeadlessRedirectsOutputToTheRuntimeLog(t *testing.T) {
	vault := newTestVault(t)
	previous := spawnArgv
	t.Cleanup(func() { spawnArgv = previous })
	spawnArgv = func(_, _ string) []string {
		return []string{testBinary(t), "-test.run=^TestHelperEchoProcess$"}
	}
	t.Setenv("RZM_TEST_HELPER_ECHO", "hello from the runtime")

	pid, err := spawnHeadlessWithToken(testBinary(t), vault, "", nil)
	require.NoError(t, err)
	require.Positive(t, pid)
	require.Eventually(t, func() bool { return !pidExists(pid) }, 15*time.Second, 50*time.Millisecond,
		"the helper must be reaped before its working directory is removed")

	if runtime.GOOS == "windows" {
		// The Windows child gets devNull stdio (an inherited log handle would
		// block its rotation); it captures its own output after election.
		return
	}
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(LogPath(vault))
		return err == nil && strings.Contains(string(data), "hello from the runtime")
	}, 15*time.Second, 50*time.Millisecond, "a headless runtime's output belongs in .rhizome/runtime.log")
}

func TestSpawnRegistrationFailureReapsTheChild(t *testing.T) {
	vault := newTestVault(t)
	previous := spawnArgv
	t.Cleanup(func() { spawnArgv = previous })
	spawnArgv = func(_, _ string) []string {
		return []string{testBinary(t), "-test.run=^TestHelperPausedSpawn$"}
	}
	t.Setenv(pausedSpawnEnv, t.TempDir())
	wantErr := errors.New("registration failed")
	var childPID int
	t.Cleanup(func() {
		if pidExists(childPID) {
			process, err := os.FindProcess(childPID)
			if err == nil {
				_ = process.Kill()
				_, _ = process.Wait()
			}
		}
	})
	pid, err := spawnHeadlessWithToken(testBinary(t), vault, "", func(pid int) error {
		childPID = pid
		return wantErr
	})
	require.ErrorIs(t, err, wantErr)
	require.Zero(t, pid)
	require.Positive(t, childPID)
	require.False(t, pidExists(childPID), "failed registration must not leave a detached child")
}

// TestHelperEchoProcess stands in for a runtime that writes to stderr.
func TestHelperEchoProcess(t *testing.T) {
	message := os.Getenv("RZM_TEST_HELPER_ECHO")
	if message == "" {
		t.Skip("helper process")
	}
	_, _ = os.Stderr.WriteString(message + "\n")
	os.Exit(0)
}
