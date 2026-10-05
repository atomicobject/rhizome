package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

// Environment contract for the helper processes. The test binary re-executes
// itself as the fake `serve --headless` child and as concurrent ensure clients,
// so detached spawn and the spawn lease are exercised without building rzm.
const (
	helperRuntimeEnv = "RZM_TEST_HELPER_RUNTIME"
	helperEnsureEnv  = "RZM_TEST_HELPER_ENSURE"
	helperVaultEnv   = "RZM_TEST_HELPER_VAULT"
	helperExeEnv     = "RZM_TEST_HELPER_EXE"
)

func newTestVault(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	t.Cleanup(func() { replacedBuilds.Delete(root) })
	return root
}

// fakeRuntime is an in-process stand-in for a live runtime: a real listener, a
// real manifest, and a health answer that matches it.
type fakeRuntime struct {
	server   *httptest.Server
	manifest InstanceManifest
	health   Health
	probes   atomic.Int64
	posts    atomic.Int64
	stopped  atomic.Bool
	// hijackPosts drops this many leading index-job connections without
	// answering, standing in for a runtime that idle-exits between probe and
	// request.
	hijackPosts int64
	// breakRunID makes health answer with a different run id (PID reuse).
	breakRunID bool
	// unhealthy makes the address accept connections but never answer health,
	// which is the alive-but-wedged state clients must wait out.
	unhealthy atomic.Bool
}

func startFakeRuntime(t *testing.T, vaultPath string, mode Mode, buildID string) *fakeRuntime {
	t.Helper()
	fake := &fakeRuntime{}
	mux := http.NewServeMux()
	mux.HandleFunc(HealthPath, func(w http.ResponseWriter, r *http.Request) {
		fake.probes.Add(1)
		if fake.unhealthy.Load() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		health := fake.health
		if fake.breakRunID {
			health.RunID = "other-run-id"
		}
		writeTestJSON(w, health)
	})
	mux.HandleFunc(ShutdownPath, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		go fake.stop(vaultPath)
	})
	mux.HandleFunc(IndexJobsPath, func(w http.ResponseWriter, r *http.Request) {
		if fake.posts.Add(1) <= fake.hijackPosts {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		w.WriteHeader(http.StatusAccepted)
		writeTestJSON(w, IndexJobResponse{JobID: "job-1"})
	})
	fake.server = httptest.NewServer(mux)
	t.Cleanup(func() { fake.stop(vaultPath) })

	host, port := splitTestAddr(t, fake.server.Listener.Addr().String())
	fake.manifest = InstanceManifest{
		InstanceID:   "fake-instance",
		VaultPath:    vaultPath,
		PID:          os.Getpid(),
		RunID:        "fake-run-id",
		Mode:         mode,
		BuildID:      buildID,
		ControlToken: "fake-token",
		HTTPHost:     host,
		HTTPPort:     port,
		HTTPURL:      fake.server.URL,
		StartedAt:    time.Now(),
		Ready:        true,
	}
	fake.health = Health{
		InstanceID: fake.manifest.InstanceID,
		RunID:      fake.manifest.RunID,
		PID:        fake.manifest.PID,
		VaultPath:  vaultPath,
		Mode:       mode,
		BuildID:    buildID,
		Ready:      true,
		StartedAt:  fake.manifest.StartedAt,
	}
	require.NoError(t, WriteManifest(vaultPath, fake.manifest))
	return fake
}

func (f *fakeRuntime) stop(vaultPath string) {
	if f.stopped.Swap(true) {
		return
	}
	f.server.Close()
	_ = RemoveManifestIfOwner(vaultPath, f.manifest.PID)
}

func (f *fakeRuntime) client() *Client {
	return &Client{Manifest: f.manifest, HTTP: &http.Client{}}
}

func writeTestJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func splitTestAddr(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portText, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	var port int
	_, err = fmt.Sscanf(portText, "%d", &port)
	require.NoError(t, err)
	return host, port
}

// writeDeadManifest publishes a manifest for a PID that has already exited and
// an address nobody listens on.
func writeDeadManifest(t *testing.T, vaultPath string) InstanceManifest {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.NoError(t, listener.Close())
	host, port := splitTestAddr(t, addr)
	manifest := InstanceManifest{
		InstanceID: "dead", VaultPath: vaultPath, PID: deadPID(t), RunID: "dead-run",
		Mode: ModeHeadless, HTTPHost: host, HTTPPort: port,
		HTTPURL: "http://" + addr, StartedAt: time.Now(),
	}
	require.NoError(t, WriteManifest(vaultPath, manifest))
	return manifest
}

func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(testBinary(t), "-test.run=^TestHelperExitImmediately$")
	cmd.Env = append(os.Environ(), "RZM_TEST_HELPER_EXIT=1")
	require.NoError(t, cmd.Start())
	pid := cmd.Process.Pid
	_ = cmd.Wait()
	return pid
}

func testBinary(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	return exe
}

// useHelperSpawn points spawnHeadlessWithToken at this test binary's fake runtime.
func useHelperSpawn(t *testing.T) *atomic.Value {
	t.Helper()
	exe := testBinary(t)
	previous := spawnArgv
	seen := &atomic.Value{}
	spawnArgv = func(executable, vaultRoot string) []string {
		seen.Store(executable)
		return helperRuntimeArgv(exe, vaultRoot)
	}
	t.Cleanup(func() { spawnArgv = previous })
	t.Setenv(helperRuntimeEnv, "1")
	return seen
}

func helperRuntimeArgv(exe, vaultRoot string) []string {
	return []string{exe, "-test.run=^TestHelperRuntimeProcess$", "--", "serve", "--headless", "--vault", vaultRoot}
}

// stopSpawnedRuntime shuts down whatever runtime the test left running.
func stopSpawnedRuntime(t *testing.T, vaultPath string) {
	t.Helper()
	t.Cleanup(func() {
		manifest, err := ReadManifest(vaultPath)
		if err != nil {
			return
		}
		// Cleanup runs after t.Context is canceled. Give the helper a fresh
		// context so it can remove its manifest and release runtime.lock.
		ctx, cancel := context.WithTimeout(context.Background(), StopGrace)
		defer cancel()
		client := &Client{Manifest: manifest, HTTP: &http.Client{}}
		shutdownErr := RequestShutdown(ctx, client, StopGrace)
		if shutdownErr == nil {
			return
		}

		// A failed graceful shutdown must not leave the helper writing into a
		// TempDir while the test framework removes it.
		proc, findErr := os.FindProcess(manifest.PID)
		require.NoError(t, findErr)
		require.NoError(t, proc.Kill())
		require.Eventually(t, func() bool {
			_, probeErr := Probe(context.Background(), manifest)
			return probeErr != nil
		}, StopGrace, 20*time.Millisecond)
		t.Logf("graceful shutdown failed; helper killed instead: %v", shutdownErr)
	})
}

// TestHelperExitImmediately exists only to produce a PID that is certainly dead.
func TestHelperExitImmediately(t *testing.T) {
	if os.Getenv("RZM_TEST_HELPER_EXIT") != "1" {
		t.Skip("helper process")
	}
	os.Exit(0)
}

// TestHelperRuntimeProcess stands in for `rzm serve --headless`: it elects on
// the runtime lock, publishes a manifest, and answers health until shutdown.
func TestHelperRuntimeProcess(t *testing.T) {
	if os.Getenv(helperRuntimeEnv) != "1" {
		t.Skip("helper process")
	}
	os.Exit(runHelperRuntime(os.Args))
}

func runHelperRuntime(args []string) int {
	root, ok := helperVaultArg(args)
	if !ok {
		fmt.Fprintf(os.Stderr, "helper runtime: unexpected argv %v\n", args)
		return 2
	}
	if os.Getenv(skipRepoDelegateEnv) != "1" {
		fmt.Fprintf(os.Stderr, "helper runtime: %s not set\n", skipRepoDelegateEnv)
		return 4
	}
	if cwd, err := os.Getwd(); err != nil || !sameTestPath(cwd, root) {
		fmt.Fprintf(os.Stderr, "helper runtime: cwd %q is not the vault root %q\n", cwd, root)
		return 5
	}

	// Election: a second runtime for the same vault exits without touching the
	// winner's files.
	releaseLock, acquired, err := indexlock.TryAcquire(LockPath(root))
	if err != nil || !acquired {
		return ExitCodeAlreadyRunning
	}
	defer func() { _ = releaseLock() }()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 6
	}
	runID, _ := NewRunID()
	token, _ := NewControlToken()
	manifest := InstanceManifest{
		InstanceID: "helper", VaultPath: root, PID: os.Getpid(), RunID: runID,
		Mode: ModeHeadless, BuildID: os.Getenv("RZM_TEST_HELPER_BUILD"),
		ControlToken: token, HTTPURL: "http://" + listener.Addr().String(),
		StartedAt: time.Now(), Ready: true,
	}
	health := Health{
		InstanceID: manifest.InstanceID, RunID: runID, PID: manifest.PID, VaultPath: root,
		Mode: ModeHeadless, BuildID: manifest.BuildID, Ready: true, StartedAt: manifest.StartedAt,
	}
	done := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc(HealthPath, func(w http.ResponseWriter, r *http.Request) { writeTestJSON(w, health) })
	mux.HandleFunc(ShutdownPath, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		select {
		case <-done:
		default:
			close(done)
		}
	})
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	if err := WriteManifest(root, manifest); err != nil {
		return 7
	}
	select {
	case <-done:
	case <-time.After(60 * time.Second): // never outlive the test run
	}
	_ = RemoveManifestIfOwner(root, manifest.PID)
	_ = server.Close()
	return 0
}

// TestHelperEnsureClient is one concurrent ensure caller in its own process.
func TestHelperEnsureClient(t *testing.T) {
	if os.Getenv(helperEnsureEnv) != "1" {
		t.Skip("helper process")
	}
	exe := os.Getenv(helperExeEnv)
	root := os.Getenv(helperVaultEnv)
	spawnArgv = func(_, vaultRoot string) []string { return helperRuntimeArgv(exe, vaultRoot) }
	result, err := Ensure(t.Context(), EnsureOptions{
		VaultPath: root, Executable: exe, Autostart: true, Wait: true, Budget: 45 * time.Second,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ensure failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("ENSURE %d %s\n", result.Health.PID, result.Health.RunID)
	os.Exit(0)
}

func helperVaultArg(args []string) (string, bool) {
	for i, arg := range args {
		if arg == "--vault" && i+1 < len(args) {
			if i < 2 || args[i-2] != "serve" || args[i-1] != "--headless" {
				return "", false
			}
			return args[i+1], true
		}
	}
	return "", false
}

func sameTestPath(a, b string) bool {
	resolve := func(p string) string {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			return resolved
		}
		return filepath.Clean(p)
	}
	return strings.EqualFold(resolve(a), resolve(b))
}
