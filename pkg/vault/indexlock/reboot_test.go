package indexlock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func previousBootLock(t *testing.T) LockData {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("platform does not record boot identity")
	}
	identity := testRuntimeIdentity(t)
	prefix, boot, ok := strings.Cut(identity, ":boot-")
	require.True(t, ok)
	switch runtime.GOOS {
	case "darwin", "windows":
		seconds, err := strconv.ParseInt(boot, 10, 64)
		require.NoError(t, err)
		identity = prefix + ":boot-" + strconv.FormatInt(seconds-86400, 10)
	case "linux":
		_, namespace, ok := strings.Cut(boot, ":pid:")
		require.True(t, ok)
		identity = prefix + ":boot-00000000-0000-0000-0000-000000000000:pid:" + namespace
	default:
		t.Skip("platform does not record boot identity")
	}
	return LockData{PID: deadPID(t), Host: getHostname(), Runtime: identity,
		Started: "2000-01-01T00:00:00Z", Token: "previous-boot"}
}

func TestTryAcquireRecoversPreviousBoot(t *testing.T) {
	for _, processLifetime := range []bool{false, true} {
		t.Run(strconv.FormatBool(processLifetime), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime.lock")
			owner := previousBootLock(t)
			owner.ProcessLifetime = processLifetime
			data, err := json.Marshal(owner)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, data, 0o644))
			release, acquired, err := TryAcquireWithOptions(path, AcquireOptions{ProcessLifetime: processLifetime})
			require.NoError(t, err)
			require.True(t, acquired)
			require.NoError(t, release())
		})
	}
}

func TestTryAcquirePreservesUncertainPreviousBoot(t *testing.T) {
	for _, name := range []string{"live pid", "foreign host", "same short hostname", "malformed identity", "missing acquisition time", "current acquisition", "same boot foreign namespace"} {
		t.Run(name, func(t *testing.T) {
			owner := previousBootLock(t)
			switch name {
			case "live pid":
				owner.PID = os.Getpid()
			case "foreign host":
				owner.Host = "another-host"
			case "same short hostname":
				owner.Host = shortHostname() + ".another-host"
			case "malformed identity":
				owner.Runtime += "-invalid"
			case "missing acquisition time":
				owner.Started = ""
			case "current acquisition":
				owner.Started = time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
			case "same boot foreign namespace":
				if runtime.GOOS != "linux" {
					t.Skip("Linux PID namespaces only")
				}
				identity := testRuntimeIdentity(t)
				prefix, _, _ := strings.Cut(identity, ":pid:")
				owner.Runtime = prefix + ":pid:[1]"
			}
			path := filepath.Join(t.TempDir(), "runtime.lock")
			data, err := json.Marshal(owner)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, data, 0o644))
			release, acquired, err := TryAcquire(path)
			require.ErrorIs(t, err, ErrForeignRuntime)
			require.False(t, acquired)
			require.Nil(t, release)
			got, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, data, got)
		})
	}
}
