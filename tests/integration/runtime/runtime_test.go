//go:build integration

// Package integration drives the vault runtime (SPEC-0104) through the real
// rzm binary: auto-start under concurrent clients, delegation, stop, crash
// recovery, rebuild, attached-versus-headless ownership, idle exit, and index
// freshness after edits. Unit and helper-process tests cover the pieces; these
// prove the pieces compose the way a user meets them.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/require"
)

func TestConcurrentIndexCommandsShareOneRuntime(t *testing.T) {
	t.Parallel()
	vault := newRuntimeVault(t, "")

	const clients = 6
	results := make([]commandResult, clients)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = vault.run(nil, "index")
		}()
	}
	wg.Wait()
	for i, result := range results {
		require.NoError(t, result.err, "client %d: stdout=%s stderr=%s", i, result.stdout, result.stderr)
		require.NotContains(t, result.stderr, "another Rhizome instance", "client %d contended for the index lock", i)
	}

	health := vault.requireLiveRuntime(t)
	require.Equal(t, appruntime.ModeHeadless, health.Mode)
	require.FileExists(t, filepath.Join(vault.root, ".rhizome", "db.sqlite"))

	status := vault.run(nil, "index", "--status")
	require.NoError(t, status.err, "stdout=%s stderr=%s", status.stdout, status.stderr)
	require.Contains(t, status.stderr, "Mode:    headless")
	require.Contains(t, status.stderr, pidLine(health.PID))
	require.Eventually(t, func() bool {
		return strings.Contains(vault.run(nil, "index", "--status").stderr, "Ready:   true")
	}, 60*time.Second, 500*time.Millisecond, "the runtime publishes its ready state once the index is usable")

	// A later index reuses the runtime instead of starting another.
	again := vault.run(nil, "index")
	require.NoError(t, again.err, "stdout=%s stderr=%s", again.stdout, again.stderr)
	require.Equal(t, health.PID, vault.requireLiveRuntime(t).PID)
}

func TestStopEndsTheRuntimeAndTheNextIndexStartsAFreshOne(t *testing.T) {
	t.Parallel()
	vault := newRuntimeVault(t, "")
	indexed := vault.run(nil, "index")
	require.NoError(t, indexed.err, "stdout=%s stderr=%s", indexed.stdout, indexed.stderr)
	first := vault.requireLiveRuntime(t)
	firstManifest, err := appruntime.ReadManifest(vault.root)
	require.NoError(t, err)

	stopped := vault.run(nil, "stop")
	require.NoError(t, stopped.err, "stdout=%s stderr=%s", stopped.stdout, stopped.stderr)
	require.Contains(t, stopped.stdout+stopped.stderr, "stopped")
	requirePIDExits(t, first.PID, 15*time.Second)
	_, err = appruntime.ReadManifest(vault.root)
	require.Error(t, err, "a stopped runtime removes its manifest")

	idle := vault.run(nil, "stop")
	require.NoError(t, idle.err, "stdout=%s stderr=%s", idle.stdout, idle.stderr)
	require.Contains(t, idle.stdout+idle.stderr, "no running")

	indexed = vault.run(nil, "index")
	require.NoError(t, indexed.err, "stdout=%s stderr=%s", indexed.stdout, indexed.stderr)
	require.NotEqual(t, first.PID, vault.requireLiveRuntime(t).PID)
	restarted, err := appruntime.ReadManifest(vault.root)
	require.NoError(t, err)
	require.Equal(t, firstManifest.HTTPPort, restarted.HTTPPort, "a clean restart reuses the worktree's port")
}

func TestIndexRecoversAfterTheRuntimeIsKilled(t *testing.T) {
	t.Parallel()
	vault := newRuntimeVault(t, "")
	indexed := vault.run(nil, "index")
	require.NoError(t, indexed.err, "stdout=%s stderr=%s", indexed.stdout, indexed.stderr)
	first := vault.requireLiveRuntime(t)

	process, err := os.FindProcess(first.PID)
	require.NoError(t, err)
	require.NoError(t, process.Kill())
	requirePIDExits(t, first.PID, 15*time.Second)
	_, err = appruntime.ReadManifest(vault.root)
	require.NoError(t, err, "a killed runtime leaves its manifest and lock behind")

	recovered := vault.run(nil, "index")
	lockPath := filepath.Join(vault.root, ".rhizome", "index.lock")
	lock, readable := indexlock.ReadLockData(lockPath)
	_, guardErr := os.Stat(lockPath + ".guard.lock")
	require.NoError(t, recovered.err, "stdout=%s stderr=%s index.lock readable=%t pid=%d role=%q guard.stat=%v", recovered.stdout, recovered.stderr, readable, lock.PID, lock.Role, guardErr)
	require.NotEqual(t, first.PID, vault.requireLiveRuntime(t).PID)
}

func TestIndexRacingStopStillSucceeds(t *testing.T) {
	t.Parallel()
	vault := newRuntimeVault(t, "")
	for round := 0; round < 3; round++ {
		indexed := vault.run(nil, "index")
		require.NoError(t, indexed.err, "round %d initial index: stdout=%s stderr=%s", round, indexed.stdout, indexed.stderr)
		vault.requireLiveRuntime(t)

		// Whichever wins, neither command may fail: an index that loses its
		// runtime mid-job falls back in-process, and one that arrives during the
		// shutdown window waits it out and starts a fresh runtime.
		var stop, index commandResult
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); stop = vault.run(nil, "stop") }()
		go func() { defer wg.Done(); index = vault.run(nil, "index") }()
		wg.Wait()
		require.NoError(t, stop.err, "round %d stop: stdout=%s stderr=%s", round, stop.stdout, stop.stderr)
		require.NoError(t, index.err, "round %d index: stdout=%s stderr=%s", round, index.stdout, index.stderr)
	}
}

func TestAutostartDisabledIndexesInProcess(t *testing.T) {
	t.Parallel()
	vault := newRuntimeVault(t, "")
	configPath := filepath.Join(vault.root, ".rhizome", "config.yml")
	config, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, []byte(strings.Replace(string(config), "code:\n", "code:\n  enabled: true\n", 1)), 0o644))

	result := vault.run(map[string]string{appruntime.AutostartEnv: "0"}, "index")
	require.NoError(t, result.err, "stdout=%s stderr=%s", result.stdout, result.stderr)
	require.Contains(t, result.stderr, "Indexing in this process")
	require.FileExists(t, filepath.Join(vault.root, ".rhizome", "db.sqlite"))
	// A completed index is immediately usable without a runtime reopening the
	// writer and repairing the schema proof left by SQLite maintenance.
	symbol := vault.run(map[string]string{appruntime.AutostartEnv: "0"}, "agent", "code-symbol", "--symbol", "greet", "--path", "src/example.py")
	require.NoError(t, symbol.err, "stdout=%s stderr=%s", symbol.stdout, symbol.stderr)
	require.Contains(t, symbol.stdout, `"definition"`)
	_, _, err = appruntime.LiveManifest(context.Background(), vault.root)
	require.Error(t, err, "no runtime may start when auto-start is disabled")
}

func TestRebuildStopsAndRestartsTheHeadlessRuntime(t *testing.T) {
	t.Parallel()
	vault := newRuntimeVault(t, "")
	indexed := vault.run(nil, "index")
	require.NoError(t, indexed.err, "stdout=%s stderr=%s", indexed.stdout, indexed.stderr)
	first := vault.requireLiveRuntime(t)

	rebuilt := vault.run(nil, "index", "--rebuild")
	require.NoError(t, rebuilt.err, "stdout=%s stderr=%s", rebuilt.stdout, rebuilt.stderr)
	require.Contains(t, rebuilt.stderr, "Stopping headless vault runtime")
	requirePIDExits(t, first.PID, 15*time.Second)

	// The restart is asynchronous; the rebuilt database must end up served.
	var second appruntime.Health
	require.Eventually(t, func() bool {
		_, health, err := appruntime.LiveManifest(context.Background(), vault.root)
		second = health
		return err == nil
	}, 60*time.Second, 250*time.Millisecond, "the runtime restarts after a rebuild")
	require.NotEqual(t, first.PID, second.PID)
	indexed = vault.run(nil, "index")
	require.NoError(t, indexed.err, "stdout=%s stderr=%s", indexed.stdout, indexed.stderr)
}

// Restart Rhizome in the desktop app sends the bridge an open with restart,
// which replaces a headless runtime even when its build already matches.
func TestDesktopRestartReplacesAMatchingHeadlessRuntime(t *testing.T) {
	t.Parallel()
	vault := newRuntimeVault(t, "")
	bridge := filepath.Join(t.TempDir(), "rhizome-desktop-bridge"+filepath.Ext(vault.binary))
	build := exec.Command("go", "build", "-mod=vendor", "-tags", "fts5", "-o", bridge, "./desktop/bridge")
	build.Dir = fixture.RepoRoot(t)
	output, err := build.CombinedOutput()
	require.NoError(t, err, "%s", output)
	stateDir := t.TempDir()
	open := func(restart bool) appruntime.Health {
		t.Helper()
		request, err := json.Marshal(map[string]any{
			"protocol": 1, "operation": "open", "folder": vault.root,
			"globalExecutable": vault.binary, "restart": restart,
		})
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		command := exec.CommandContext(ctx, bridge, "--state-dir", stateDir)
		command.Dir = vault.root
		command.Env = vault.environment(nil)
		command.Stdin = bytes.NewReader(request)
		response, err := command.Output()
		require.NoError(t, err, "response=%s", response)
		var decoded struct {
			Result struct{ PID int } `json:"result"`
		}
		require.NoError(t, json.Unmarshal(response, &decoded))
		vault.rememberRuntimePID(decoded.Result.PID)
		health := vault.requireLiveRuntime(t)
		require.Equal(t, decoded.Result.PID, health.PID)
		return health
	}

	first := open(false)
	restarted := open(true)
	require.NotEqual(t, first.PID, restarted.PID)
	require.Equal(t, first.BuildID, restarted.BuildID, "the same build, replaced on request")
	requirePIDExits(t, first.PID, 15*time.Second)
	require.Equal(t, restarted.PID, open(false).PID, "an open without restart attaches")
}

func TestAttachedServeReplacesHeadlessAndASecondServeExits3(t *testing.T) {
	t.Parallel()
	vault := newRuntimeVault(t, "")
	indexed := vault.run(nil, "index")
	require.NoError(t, indexed.err, "stdout=%s stderr=%s", indexed.stdout, indexed.stderr)
	headless := vault.requireLiveRuntime(t)

	attached := vault.start(t, "serve", "--open=false", "--port", "0")
	require.Eventually(t, func() bool {
		_, health, err := appruntime.LiveManifest(context.Background(), vault.root)
		return err == nil && health.Mode == appruntime.ModeAttached && health.PID == attached.pid
	}, 60*time.Second, 250*time.Millisecond, "an attached serve takes over from a headless runtime")
	requirePIDExits(t, headless.PID, 15*time.Second)

	second := vault.run(nil, "serve", "--open=false", "--port", "0")
	var exit *exec.ExitError
	require.True(t, errors.As(second.err, &exit), "a second attached serve must fail: %v stdout=%s stderr=%s", second.err, second.stdout, second.stderr)
	require.Equal(t, appruntime.ExitCodeAlreadyRunning, exit.ExitCode(), "stdout=%s stderr=%s", second.stdout, second.stderr)

	// Indexing delegates to the attached runtime rather than replacing it.
	delegated := vault.run(nil, "index")
	require.NoError(t, delegated.err, "stdout=%s stderr=%s", delegated.stdout, delegated.stderr)
	require.Equal(t, attached.pid, vault.requireLiveRuntime(t).PID)

	stopped := vault.run(nil, "stop")
	require.NoError(t, stopped.err, "stdout=%s stderr=%s", stopped.stdout, stopped.stderr)
	select {
	case <-attached.exited:
	case <-time.After(30 * time.Second):
		t.Fatal("rzm stop did not end the attached serve")
	}
}

func TestStartTakesOverAHeadlessRuntimeAndJoinsAnAttachedOne(t *testing.T) {
	t.Parallel()
	vault := newRuntimeVault(t, "")
	indexed := vault.run(nil, "index")
	require.NoError(t, indexed.err, "stdout=%s stderr=%s", indexed.stdout, indexed.stderr)
	headless := vault.requireLiveRuntime(t)

	started := vault.start(t, "start", "--open=false", "--port", "0")
	require.Eventually(t, func() bool {
		_, health, err := appruntime.LiveManifest(context.Background(), vault.root)
		return err == nil && health.Mode == appruntime.ModeAttached && health.PID == started.pid
	}, 60*time.Second, 250*time.Millisecond, "rzm start takes over from a headless runtime")
	requirePIDExits(t, headless.PID, 15*time.Second)

	// Against an attached runtime, start joins it and exits successfully.
	joined := vault.run(nil, "start", "--open=false", "--port", "0")
	require.NoError(t, joined.err, "stdout=%s stderr=%s", joined.stdout, joined.stderr)
	require.Contains(t, joined.stderr, "already serving this vault")
	require.Equal(t, started.pid, vault.requireLiveRuntime(t).PID)
}

func TestHeadlessRuntimeExitsWhenIdle(t *testing.T) {
	t.Parallel()
	vault := newRuntimeVault(t, "runtime:\n  idleTimeout: 3s\n")
	indexed := vault.run(nil, "index")
	require.NoError(t, indexed.err, "stdout=%s stderr=%s", indexed.stdout, indexed.stderr)
	_, health, err := appruntime.LiveManifest(context.Background(), vault.root)
	require.NoError(t, err)

	// Watch the PID, not the health route: every API request counts as activity.
	requirePIDExits(t, health.PID, 60*time.Second)
	_, err = appruntime.ReadManifest(vault.root)
	require.Error(t, err, "an idle exit removes the manifest")

	indexed = vault.run(nil, "index")
	require.NoError(t, indexed.err, "the next command starts a new runtime: stdout=%s stderr=%s", indexed.stdout, indexed.stderr)
}

// An open UI event stream holds a headless runtime regardless of the idle
// timeout; closing it returns the runtime to its normal idle exit.
func TestHeadlessRuntimeStaysAliveUnderAnOpenUIEventStream(t *testing.T) {
	t.Parallel()
	vault := newRuntimeVault(t, "runtime:\n  idleTimeout: 3s\n")
	indexed := vault.run(nil, "index")
	require.NoError(t, indexed.err, "stdout=%s stderr=%s", indexed.stdout, indexed.stderr)
	client, health, err := appruntime.LiveManifest(context.Background(), vault.root)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, client.Manifest.HTTPURL+"/api/v1/events", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	go func() { _, _ = io.Copy(io.Discard, resp.Body) }()

	time.Sleep(15 * time.Second)
	require.True(t, indexlock.PIDExists(health.PID), "the runtime idle-exited under an open UI event stream")
	cancel()
	requirePIDExits(t, health.PID, 60*time.Second)
}

func TestRuntimeIndexesEditsWithoutAnExplicitIndex(t *testing.T) {
	t.Parallel()
	vault := newRuntimeVault(t, "")
	indexed := vault.run(nil, "index")
	require.NoError(t, indexed.err, "stdout=%s stderr=%s", indexed.stdout, indexed.stderr)
	vault.requireLiveRuntime(t)

	const token = "quokkaflangeuniqueterm"
	require.NoError(t, os.WriteFile(filepath.Join(vault.root, "notes", "fresh.md"), []byte("# Fresh note\n\nThe "+token+" arrived after indexing.\n"), 0o644))

	require.Eventually(t, func() bool {
		found := vault.run(nil, "search", "--raw", "--fast", token)
		return found.err == nil && strings.Contains(found.stdout, "fresh")
	}, 90*time.Second, time.Second, "the runtime's watcher indexes a new note without rzm index")
}

func TestNewWorktreeCopiesFromASourceWithALiveRuntime(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	source := newRuntimeVaultIn(t, home, "")
	target := newRuntimeVaultIn(t, home, "")
	indexed := source.run(nil, "index")
	require.NoError(t, indexed.err, "stdout=%s stderr=%s", indexed.stdout, indexed.stderr)
	source.requireLiveRuntime(t)

	copied := target.run(nil, "new-worktree", source.root)
	require.NoError(t, copied.err, "stdout=%s stderr=%s", copied.stdout, copied.stderr)
	require.Regexp(t, `(Copied|Cloned) Rhizome index`, copied.stderr)
	require.Contains(t, copied.stderr, "Rhizome index is up to date.")
	source.requireLiveRuntime(t)
}

func TestStopAllStopsEveryRuntimeForTheUser(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	first := newRuntimeVaultIn(t, home, "")
	second := newRuntimeVaultIn(t, home, "")
	indexed := first.run(nil, "index")
	require.NoError(t, indexed.err, "stdout=%s stderr=%s", indexed.stdout, indexed.stderr)
	indexed = second.run(nil, "index")
	require.NoError(t, indexed.err, "stdout=%s stderr=%s", indexed.stdout, indexed.stderr)
	firstPID := first.requireLiveRuntime(t).PID
	secondPID := second.requireLiveRuntime(t).PID

	stopped := first.run(nil, "stop", "--all")
	require.NoError(t, stopped.err, "stdout=%s stderr=%s", stopped.stdout, stopped.stderr)
	requirePIDExits(t, firstPID, 15*time.Second)
	requirePIDExits(t, secondPID, 15*time.Second)
}

func pidLine(pid int) string {
	return "PID:     " + itoa(pid)
}
