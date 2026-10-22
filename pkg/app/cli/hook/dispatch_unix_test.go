//go:build !windows

package hook

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func processAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func TestTimeoutKillsTheHandlersProcessTree(t *testing.T) {
	root := repo(t)
	pidFile := filepath.Join(root, "grandchild.pid")
	slow := helperHandler("slow", "sleep", pidFile)
	slow.Timeout = 500 * time.Millisecond
	started := time.Now()
	res, _ := dispatchOnce(t, root, HarnessClaude, PostToolUse, readPayload(root), slow, helperHandler("good", "items:a"))
	require.Less(t, time.Since(started), 3*time.Second)
	require.Equal(t, "text a", res.HookSpecificOutput.AdditionalContext)

	data, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	pid, err := strconv.Atoi(string(data))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return !processAlive(pid) }, 2*time.Second, 20*time.Millisecond, "grandchild %d survived", pid)
}
