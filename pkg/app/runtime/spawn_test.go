package runtime

import (
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

	pid, err := spawnHeadlessWithToken(testBinary(t), vault, "")
	require.NoError(t, err)
	require.Positive(t, pid)

	if runtime.GOOS == "windows" {
		// The Windows child gets devNull stdio (an inherited log handle would
		// block its rotation); it captures its own output after election.
		require.Eventually(t, func() bool { return !pidExists(pid) }, 15*time.Second, 50*time.Millisecond,
			"the helper must exit before its working directory is removed")
		return
	}
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(LogPath(vault))
		return err == nil && strings.Contains(string(data), "hello from the runtime")
	}, 15*time.Second, 50*time.Millisecond, "a headless runtime's output belongs in .rhizome/runtime.log")
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
