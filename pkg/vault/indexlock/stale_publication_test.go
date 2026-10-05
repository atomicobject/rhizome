package indexlock

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/stretchr/testify/require"
)

func writeStalePublicationFixture(t *testing.T, dir string) (string, []byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o700))
	identity, err := runtimeIdentity()
	require.NoError(t, err)
	old, err := json.Marshal(LockData{
		PID: deadPID(t), Host: getHostname(), Runtime: identity,
		Token: "old-owner", Started: time.Now().UTC().Format(time.RFC3339Nano),
	})
	require.NoError(t, err)
	path := filepath.Join(dir, "index.lock")
	require.NoError(t, os.WriteFile(path, old, 0o644))
	return path, old
}

func requireNoPublicationTemps(t *testing.T, path string) {
	t.Helper()
	temps, err := filepath.Glob(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*"))
	require.NoError(t, err)
	require.Empty(t, temps)
}

func TestStaleTakeoverKeepsMetadataReadable(t *testing.T) {
	dir := t.TempDir()
	_, old := writeStalePublicationFixture(t, dir)
	type observations struct {
		reads, unexpected int
		first             []string
	}
	var total observations
	for trial := 0; trial < 500; trial++ {
		path := filepath.Join(dir, fmt.Sprintf("index-%d.lock", trial))
		require.NoError(t, os.WriteFile(path, old, 0o644))
		ready := make(chan struct{}, 4)
		completed := make(chan struct{})
		observed := make(chan observations, 4)
		for reader := 0; reader < 4; reader++ {
			go func() {
				var result observations
				for {
					postTakeover := false
					select {
					case <-completed:
						postTakeover = true
					default:
					}
					raw, err := fileio.ReadFile(path)
					result.reads++
					valid := err == nil && bytes.Equal(raw, old)
					if err == nil && !valid {
						var owner LockData
						valid = json.Unmarshal(raw, &owner) == nil && owner.PID == os.Getpid() &&
							owner.Role == "replacement-owner" && owner.Token != "" && owner.Token != "old-owner"
					}
					if !valid {
						result.unexpected++
						if len(result.first) < 10 {
							result.first = append(result.first, fmt.Sprintf("trial=%d reader=%d bytes=%d err=%v", trial, reader, len(raw), err))
						}
					}
					if result.reads == 1 {
						ready <- struct{}{}
					}
					if postTakeover {
						observed <- result
						return
					}
				}
			}()
		}
		for reader := 0; reader < 4; reader++ {
			<-ready
		}
		// Every reader opens before takeover and once again after it returns.
		// No missing path, partial record or open error is a valid handoff.
		release, acquired, err := TryAcquireWithOptions(path, AcquireOptions{Role: "replacement-owner"})
		close(completed)
		for reader := 0; reader < 4; reader++ {
			result := <-observed
			total.reads += result.reads
			total.unexpected += result.unexpected
			for _, detail := range result.first {
				if len(total.first) < 10 {
					total.first = append(total.first, detail)
				}
			}
		}
		require.NoError(t, err)
		require.True(t, acquired)
		owner, ok := ReadLockData(path)
		require.True(t, ok)
		require.Equal(t, "replacement-owner", owner.Role)
		require.NoError(t, release()) // Readers have joined before owner cleanup.
		requireNoPublicationTemps(t, path)
	}
	t.Logf("trials=500 reads=%d unexpected=%d", total.reads, total.unexpected)
	require.Zero(t, total.unexpected, "unexpected takeover observations: %v", total.first)
}

func TestStaleRecheckPublishesAfterOwnerDisappears(t *testing.T) {
	path, _ := writeStalePublicationFixture(t, t.TempDir())
	require.NoError(t, os.Remove(path))
	owner := LockData{PID: os.Getpid(), Token: "new-owner"}
	raw, err := json.Marshal(owner)
	require.NoError(t, err)
	release, acquired, err := acquireAfterStale(path, owner, raw, func([]byte) bool {
		t.Fatal("a missing owner has no stale metadata to revalidate")
		return false
	})
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { require.NoError(t, release()) })
	current, err := fileio.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, raw, current)
	requireNoPublicationTemps(t, path)
}

func TestStaleTakeoverWithOpenMetadataReader(t *testing.T) {
	for _, long := range []bool{false, true} {
		name := "short"
		if long {
			name = "long"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if long {
				dir = filepath.Join(dir, strings.Repeat("nested", 20), strings.Repeat("vault", 24))
			}
			path, old := writeStalePublicationFixture(t, dir)
			if long {
				require.Greater(t, len(path), 260)
			}
			reader, err := fileio.OpenRead(path)
			require.NoError(t, err)
			defer reader.Close()

			// Hold the old production reader throughout acquisition and the new
			// opener. This overlap does not depend on sleeps or scheduling.
			release, acquired, acquireErr := TryAcquireWithOptions(path, AcquireOptions{Role: "replacement-owner"})
			if release != nil {
				t.Cleanup(func() { require.NoError(t, release()) })
			}
			current, readErr := fileio.ReadFile(path)
			t.Logf("acquired=%v acquireErr=%v readErr=%v", acquired, acquireErr, readErr)
			require.NoError(t, acquireErr)
			require.True(t, acquired)
			require.NoError(t, readErr)
			var owner LockData
			require.NoError(t, json.Unmarshal(current, &owner))
			require.Equal(t, "replacement-owner", owner.Role)

			retained, err := io.ReadAll(reader)
			require.NoError(t, err)
			require.Equal(t, old, retained)
			require.NoError(t, reader.Close())
			current, err = fileio.ReadFile(path)
			require.NoError(t, err, "closing the old reader must preserve the successor")
			var afterClose LockData
			require.NoError(t, json.Unmarshal(current, &afterClose))
			require.True(t, sameLockOwner(owner, afterClose))
			requireNoPublicationTemps(t, path)
		})
	}
}
