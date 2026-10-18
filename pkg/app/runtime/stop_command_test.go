package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/atomicobject/rhizome/internal/testkit"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

// stopTestRuntime is a published manifest backed by a health endpoint, which is the
// only thing a client is allowed to trust (SPEC-0104 US1). Its liveness is
// driven by the injected process probe, so a runtime can convincingly ignore
// shutdown without spawning a real one.
type stopTestRuntime struct {
	vaultPath    string
	manifest     InstanceManifest
	shutdowns    atomic.Int32
	obeysStop    bool
	alive        atomic.Bool
	terminations atomic.Int32
}

func newStopTestRuntime(t *testing.T, mode Mode, pid int, obeysStop bool) *stopTestRuntime {
	t.Helper()
	vaultPath := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultPath, ".rhizome"), 0o755))

	rt := &stopTestRuntime{vaultPath: vaultPath, obeysStop: obeysStop}
	rt.alive.Store(true)
	rt.manifest = InstanceManifest{
		InstanceID:   InstanceID(vaultPath),
		VaultName:    filepath.Base(vaultPath),
		VaultPath:    vaultPath,
		PID:          pid,
		RunID:        "run-" + filepath.Base(vaultPath),
		Mode:         mode,
		ControlToken: "secret-token",
		StartedAt:    time.Now().UTC(),
		Ready:        true,
	}

	mux := http.NewServeMux()
	mux.HandleFunc(HealthPath, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Health{
			InstanceID: rt.manifest.InstanceID,
			RunID:      rt.manifest.RunID,
			PID:        rt.manifest.PID,
			VaultPath:  vaultPath,
			Mode:       mode,
			Ready:      true,
		})
	})
	mux.HandleFunc(ShutdownPath, func(w http.ResponseWriter, r *http.Request) {
		if BearerToken(r) != rt.manifest.ControlToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		rt.shutdowns.Add(1)
		if rt.obeysStop {
			rt.alive.Store(false)
		}
		w.WriteHeader(http.StatusAccepted)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	rt.manifest.HTTPURL = server.URL
	require.NoError(t, WriteManifest(vaultPath, rt.manifest))
	return rt
}

// stubProcessOps replaces the process helpers for the duration of one test and
// answers for every runtime passed in.
func stubProcessOps(t *testing.T, runtimes ...*stopTestRuntime) {
	t.Helper()
	aliveOrig, terminateOrig := processIsAlive, processTerminateFn
	t.Cleanup(func() { processIsAlive, processTerminateFn = aliveOrig, terminateOrig })

	find := func(pid int) *stopTestRuntime {
		for _, rt := range runtimes {
			if rt.manifest.PID == pid {
				return rt
			}
		}
		return nil
	}
	processIsAlive = func(pid int) bool {
		rt := find(pid)
		return rt != nil && rt.alive.Load()
	}
	processTerminateFn = func(_ string, manifest InstanceManifest) error {
		if rt := find(manifest.PID); rt != nil {
			rt.terminations.Add(1)
			rt.alive.Store(false)
		}
		return nil
	}
}

func TestStopShutsDownAGracefulRuntime(t *testing.T) {
	rt := newStopTestRuntime(t, ModeHeadless, 424242, true)
	stubProcessOps(t, rt)
	var out bytes.Buffer

	require.NoError(t, Stop(context.Background(), StopOptions{VaultPath: rt.vaultPath, Out: &out, Grace: time.Second}))

	require.Equal(t, int32(1), rt.shutdowns.Load())
	require.Zero(t, rt.terminations.Load(), "a runtime that stops on request must never be terminated")
	require.Contains(t, out.String(), "stopped")
}

func TestStopAcceptsAReleasedLockFromARuntimeItsParentHasNotReaped(t *testing.T) {
	// An exited process stays visible to PID checks until its parent reaps it.
	// The runtime releases runtime.lock last, so that release is the exit.
	rt := newStopTestRuntime(t, ModeAttached, 424242, false)
	stubProcessOps(t, rt)
	lockPath := LockPath(rt.vaultPath)
	require.NoError(t, os.WriteFile(lockPath, []byte(`{"pid":424242}`), 0o600))
	go func() {
		for rt.shutdowns.Load() == 0 {
			time.Sleep(5 * time.Millisecond)
		}
		_ = os.Remove(lockPath)
	}()
	var out bytes.Buffer

	require.NoError(t, Stop(context.Background(), StopOptions{VaultPath: rt.vaultPath, Out: &out, Grace: 2 * time.Second}))

	require.True(t, rt.alive.Load(), "the PID never disappeared in this scenario")
	require.Contains(t, out.String(), "stopped")
}

func TestStopTerminatesAHeadlessRuntimeThatIgnoresShutdown(t *testing.T) {
	rt := newStopTestRuntime(t, ModeHeadless, 424242, false)
	stubProcessOps(t, rt)
	var out bytes.Buffer

	require.NoError(t, Stop(context.Background(), StopOptions{VaultPath: rt.vaultPath, Out: &out, Grace: 50 * time.Millisecond}))

	require.Equal(t, int32(1), rt.shutdowns.Load())
	require.Equal(t, int32(1), rt.terminations.Load())
	require.Contains(t, out.String(), "terminating headless runtime")
}

func TestStopRefusesToKillAnAttachedRuntime(t *testing.T) {
	rt := newStopTestRuntime(t, ModeAttached, 424242, false)
	stubProcessOps(t, rt)
	var out bytes.Buffer

	err := Stop(context.Background(), StopOptions{VaultPath: rt.vaultPath, Out: &out, Grace: 50 * time.Millisecond})

	require.ErrorIs(t, err, ErrStopIncomplete)
	require.Equal(t, int32(1), rt.shutdowns.Load(), "an attached runtime is still asked to stop")
	require.Zero(t, rt.terminations.Load(), "an attached runtime is never force-killed")
	require.Contains(t, out.String(), "stop it in its own terminal")
	require.True(t, rt.alive.Load(), "the attached runtime is left running")
}

func TestStopPreservesUnresponsiveOwnerPolicyWhenIntentReadTimesOut(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	for _, tc := range []struct {
		name string
		mode Mode
	}{
		{"headless is terminated", ModeHeadless},
		{"attached is left running", ModeAttached},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			env.HomeDir = home
			rt := newStopTestRuntime(t, tc.mode, os.Getpid(), false)
			stubProcessOps(t, rt)
			_, err := NewRegistry(env)
			require.NoError(t, err)
			cliDir, _, err := env.CliPath()
			require.NoError(t, err)
			gatePath := filepath.Join(cliDir, "instances", InstanceID(rt.vaultPath)+".intent.lock")
			type gateResult struct {
				release func() error
				ok      bool
				err     error
			}
			gateReady := make(chan gateResult, 1)
			var once sync.Once
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				once.Do(func() {
					release, ok, err := indexlock.TryAcquireWithOptions(gatePath, indexlock.AcquireOptions{})
					gateReady <- gateResult{release: release, ok: ok, err: err}
				})
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			t.Cleanup(server.Close)
			rt.manifest.HTTPURL = server.URL
			require.NoError(t, WriteManifest(rt.vaultPath, rt.manifest))
			lockPath := LockPath(rt.vaultPath)
			require.NoError(t, os.WriteFile(lockPath, []byte(fmt.Sprintf(`{"pid":%d}`, os.Getpid())), 0o600))

			var out bytes.Buffer
			stopErr := Stop(vaultconfig.WithEnv(context.Background(), env), StopOptions{VaultPath: rt.vaultPath, Out: &out, Grace: 200 * time.Millisecond})
			var gate gateResult
			select {
			case gate = <-gateReady:
			case <-time.After(time.Second):
				t.Fatal("stop did not probe the unresponsive runtime")
			}
			require.NoError(t, gate.err)
			require.True(t, gate.ok)
			require.NoError(t, gate.release())
			if tc.mode == ModeHeadless {
				require.NoError(t, stopErr)
				require.Equal(t, int32(1), rt.terminations.Load())
				require.Contains(t, out.String(), "terminating headless runtime")
			} else {
				require.ErrorIs(t, stopErr, ErrStopIncomplete)
				require.Zero(t, rt.terminations.Load())
				require.True(t, rt.alive.Load())
				require.Contains(t, out.String(), "stop it in its own terminal")
			}
		})
	}
}

func TestStopReportsWhenNoRuntimeIsRunning(t *testing.T) {
	stubProcessOps(t)
	var out bytes.Buffer

	require.NoError(t, Stop(context.Background(), StopOptions{VaultPath: t.TempDir(), Out: &out}))
	require.Contains(t, out.String(), "no running runtime")
}

func TestStopWaitsForOwnerWithoutManifestAndReportsIncomplete(t *testing.T) {
	root := t.TempDir()
	lockPath := LockPath(root)
	require.NoError(t, os.MkdirAll(filepath.Dir(lockPath), 0o755))
	require.NoError(t, os.WriteFile(lockPath, []byte(fmt.Sprintf(`{"pid":%d}`, os.Getpid())), 0o600))
	var out bytes.Buffer

	err := Stop(context.Background(), StopOptions{VaultPath: root, Out: &out, Grace: 100 * time.Millisecond})

	require.ErrorIs(t, err, ErrStopIncomplete)
	require.Contains(t, out.String(), "without a responding runtime manifest")
}

func TestStopWaitsForDrainingOwnerToReleaseLock(t *testing.T) {
	root := t.TempDir()
	lockPath := LockPath(root)
	require.NoError(t, os.MkdirAll(filepath.Dir(lockPath), 0o755))
	require.NoError(t, os.WriteFile(lockPath, []byte(fmt.Sprintf(`{"pid":%d}`, os.Getpid())), 0o600))
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- Stop(context.Background(), StopOptions{VaultPath: root, Out: &out, Grace: time.Second})
	}()
	select {
	case err := <-done:
		t.Fatalf("stop returned before the owner released runtime.lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	require.NoError(t, os.Remove(lockPath))
	require.NoError(t, <-done)
	require.Contains(t, out.String(), "no running runtime")
}

func TestStopWaitsForStartingOwnerToPublishManifest(t *testing.T) {
	rt := newStopTestRuntime(t, ModeHeadless, 424242, true)
	stubProcessOps(t, rt)
	require.NoError(t, os.Remove(ManifestPath(rt.vaultPath)))
	lockPath := LockPath(rt.vaultPath)
	require.NoError(t, os.WriteFile(lockPath, []byte(fmt.Sprintf(`{"pid":%d}`, os.Getpid())), 0o600))
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- Stop(context.Background(), StopOptions{VaultPath: rt.vaultPath, Out: &out, Grace: time.Second})
	}()
	select {
	case err := <-done:
		t.Fatalf("stop returned before startup published its manifest: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	require.NoError(t, WriteManifest(rt.vaultPath, rt.manifest))
	require.NoError(t, <-done)
	require.Equal(t, int32(1), rt.shutdowns.Load())
}

func TestStopAllVisitsEveryRegisteredRuntime(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	home := t.TempDir()
	env.HomeDir = home

	// The global registry prunes entries whose PID is really gone, so the fakes
	// borrow live PIDs from this test process and its parent.
	first := newStopTestRuntime(t, ModeHeadless, os.Getpid(), true)
	second := newStopTestRuntime(t, ModeHeadless, os.Getppid(), true)
	stubProcessOps(t, first, second)

	registry, err := NewRegistry(env)
	require.NoError(t, err)
	require.NoError(t, registry.Write(first.manifest))
	require.NoError(t, registry.Write(second.manifest))

	var out bytes.Buffer
	require.NoError(t, Stop(vaultconfig.WithEnv(context.Background(), env), StopOptions{All: true, Out: &out, Grace: time.Second}))

	require.Contains(t, out.String(), first.vaultPath)
	require.Contains(t, out.String(), second.vaultPath)
	require.Equal(t, int32(1), first.shutdowns.Load())
	require.Equal(t, int32(1), second.shutdowns.Load())
}

func TestStopAllStillStopsRegisteredRuntimeWhenIntentDiscoveryTimesOut(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	home := t.TempDir()
	env.HomeDir = home

	rt := newStopTestRuntime(t, ModeHeadless, os.Getpid(), true)
	stubProcessOps(t, rt)
	registry, err := NewRegistry(env)
	require.NoError(t, err)
	require.NoError(t, registry.Write(rt.manifest))
	blockedVault := t.TempDir()
	created, err := registry.CreateSpawnIntent(vaultconfig.WithEnv(context.Background(), env), blockedVault, "blocked-startup")
	require.NoError(t, err)
	require.True(t, created)
	cliDir, _, err := env.CliPath()
	require.NoError(t, err)
	gatePath := filepath.Join(cliDir, "instances", InstanceID(blockedVault)+".intent.lock")
	release, acquired, err := indexlock.TryAcquireWithOptions(gatePath, indexlock.AcquireOptions{})
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { _ = release() })

	var out bytes.Buffer
	err = Stop(vaultconfig.WithEnv(context.Background(), env), StopOptions{All: true, Out: &out, Grace: 100 * time.Millisecond})

	require.Equal(t, int32(1), rt.shutdowns.Load(), "a blocked intent must not prevent stopping a registered runtime")
	require.ErrorIs(t, err, ErrStopIncomplete)
	require.Contains(t, out.String(), "runtime discovery incomplete")
	require.Contains(t, out.String(), ".pending.json")
	require.Contains(t, out.String(), "stopped "+rt.vaultPath)
	require.NoError(t, release())
	intent, found, err := registry.StartIntentFor(vaultconfig.WithEnv(context.Background(), env), blockedVault)
	require.NoError(t, err)
	require.True(t, found, "the unavailable startup must remain discoverable for a later stop")
	require.False(t, intent.Cancelled)
}

func TestStopAllCancelsAPendingChildBeforeElection(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	home := t.TempDir()
	env.HomeDir = home
	vault := t.TempDir()
	registry, err := NewRegistry(env)
	require.NoError(t, err)
	created, err := registry.CreateSpawnIntent(vaultconfig.WithEnv(context.Background(), env), vault, "pending-child")
	require.NoError(t, err)
	require.True(t, created)

	var out bytes.Buffer
	require.NoError(t, Stop(vaultconfig.WithEnv(context.Background(), env), StopOptions{All: true, Out: &out, Grace: time.Second}))
	require.Contains(t, out.String(), "stopped")
	manifest := InstanceManifest{InstanceID: InstanceID(vault), VaultPath: vault, PID: os.Getpid(), RunID: "pending-child"}
	require.ErrorIs(t, registry.ElectIntent(vaultconfig.WithEnv(context.Background(), env), vault, "pending-child", true, manifest), ErrStartCancelled)
	require.NoFileExists(t, ManifestPath(vault))
}
