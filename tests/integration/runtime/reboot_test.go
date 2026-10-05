//go:build integration

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

func TestRuntimeRecoversLocksFromPreviousBoot(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("platform does not record boot identity")
	}
	for _, command := range []string{"start", "index"} {
		t.Run(command, func(t *testing.T) {
			vault := newRuntimeVault(t, "")
			path := appruntime.LockPath(vault.root)
			release, acquired, err := indexlock.TryAcquire(path)
			require.NoError(t, err)
			require.True(t, acquired)
			owner, ok := indexlock.ReadLockData(path)
			require.True(t, ok)
			require.NoError(t, release())
			prefix, boot, ok := strings.Cut(owner.Runtime, ":boot-")
			require.True(t, ok)
			switch runtime.GOOS {
			case "darwin", "windows":
				seconds, err := strconv.ParseInt(boot, 10, 64)
				require.NoError(t, err)
				owner.Runtime = prefix + ":boot-" + strconv.FormatInt(seconds-86400, 10)
			case "linux":
				_, namespace, ok := strings.Cut(boot, ":pid:")
				require.True(t, ok)
				owner.Runtime = prefix + ":boot-00000000-0000-0000-0000-000000000000:pid:" + namespace
			default:
				t.Skip("platform does not record boot identity")
			}
			owner.PID = 4194303
			require.False(t, indexlock.PIDExists(owner.PID))
			owner.Started = "2000-01-01T00:00:00Z"
			owner.ProcessLifetime = true
			data, err := json.Marshal(owner)
			require.NoError(t, err)
			for _, name := range []string{"runtime.lock", "runtime-spawn.lock", "index.lock"} {
				require.NoError(t, os.WriteFile(filepath.Join(vault.root, ".rhizome", name), data, 0o644))
			}
			if command == "start" {
				started := vault.start(t, "start", "--open=false")
				require.Equal(t, started.pid, vault.requireLiveRuntime(t).PID)
				indexed := vault.run(nil, "index")
				require.NoError(t, indexed.err, "stdout=%s stderr=%s", indexed.stdout, indexed.stderr)
			} else {
				indexed := vault.run(nil, "index")
				require.NoError(t, indexed.err, "stdout=%s stderr=%s", indexed.stdout, indexed.stderr)
				vault.requireLiveRuntime(t)
			}
		})
	}
}
