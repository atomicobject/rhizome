//go:build !windows

package actions

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTimeoutKillsProcessTree(t *testing.T) {
	root := repo(t, map[string]string{".rhizome/modules/demo/actions/hang.yaml": shAction(`sleep 60 & echo $! > child.pid; wait`, "timeout: 1s\n")})
	started := time.Now()
	got, err := (&Runner{RepoRoot: root, Trust: trusted}).Run(context.Background(), "demo/hang", nil)
	require.NoError(t, err)
	require.Equal(t, Envelope{Status: StatusError, Reason: "timed out after 1s"}, got)
	require.Less(t, time.Since(started), 10*time.Second)
	data, err := os.ReadFile(filepath.Join(root, "child.pid"))
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return syscall.Kill(pid, 0) != nil }, 5*time.Second, 50*time.Millisecond, "child %d survived", pid)
}
