package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type testVault struct {
	name string
	path string
}

// newTestVault registers a vault the serve runtime can resolve, with an
// isolated home so the global instance registry never leaks between tests.
func newTestVault(t *testing.T) testVault {
	t.Helper()
	root := t.TempDir()
	vaultDir := filepath.Join(root, "testvault")
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "Alpha.md"), []byte("# Alpha\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, ".rhizome", "config.yml"), []byte("notes: {}\n"), 0o644))
	store, err := semdb.Open(filepath.Join(vaultDir, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	require.NoError(t, store.Close())

	configFile := filepath.Join(root, "obsidian.json")
	body, err := json.Marshal(map[string]any{"vaults": map[string]any{"testvault": map[string]string{"path": vaultDir}}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configFile, body, 0o644))

	origConfig := obsidian.ObsidianConfigFile
	obsidian.ObsidianConfigFile = func() (string, error) { return configFile, nil }
	origHome := vaultconfig.UserHomeDirectory
	home := t.TempDir()
	vaultconfig.UserHomeDirectory = func() (string, error) { return home, nil }
	t.Cleanup(func() {
		obsidian.ObsidianConfigFile = origConfig
		vaultconfig.UserHomeDirectory = origHome
	})
	return testVault{name: "testvault", path: vaultDir}
}

// runResult captures one backgrounded Run invocation.
type runResult struct {
	err    error
	stderr string
}

type runHandle struct {
	cancel context.CancelFunc
	done   chan runResult
	stderr *syncBuffer
	opened chan string
	exited atomic.Bool
	// collected records that a test already consumed the run result, so the
	// cleanup does not wait on an empty channel.
	collected atomic.Bool
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func startRun(t *testing.T, vault testVault, mutate func(*Options)) *runHandle {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	handle := &runHandle{
		cancel: cancel,
		done:   make(chan runResult, 1),
		stderr: &syncBuffer{},
		opened: make(chan string, 4),
	}
	opts := Options{
		VaultName:          vault.name,
		Host:               "127.0.0.1",
		Port:               0,
		ReuseDiscoveryPort: false,
		CommandName:        "serve",
		Stderr:             handle.stderr,
		Listen:             net.Listen,
		OpenBrowser: func(url string) error {
			handle.opened <- url
			return nil
		},
		IdlePoll: 5 * time.Millisecond,
		RootPoll: 5 * time.Millisecond,
	}
	if mutate != nil {
		mutate(&opts)
	}
	go func() {
		err := Run(ctx, opts)
		handle.exited.Store(true)
		handle.done <- runResult{err: err, stderr: handle.stderr.String()}
	}()
	t.Cleanup(func() {
		cancel()
		if handle.collected.Load() {
			return
		}
		select {
		case <-handle.done:
		case <-time.After(20 * time.Second):
			t.Error("serve did not stop after cancellation")
		}
	})
	return handle
}

func (h *runHandle) waitForManifest(t *testing.T, vaultPath string) appruntime.InstanceManifest {
	t.Helper()
	var manifest appruntime.InstanceManifest
	require.Eventually(t, func() bool {
		select {
		case result := <-h.done:
			h.collected.Store(true)
			t.Fatalf("serve exited before publishing a manifest: %v\n%s", result.err, result.stderr)
		default:
		}
		read, err := appruntime.ReadManifest(vaultPath)
		if err != nil {
			return false
		}
		manifest = read
		return manifest.HTTPPort > 0
	}, 20*time.Second, 25*time.Millisecond)
	return manifest
}

// awaitExit waits for a runtime that stops itself (idle timeout, vanished vault
// root, control shutdown) rather than one the test cancels.
func (h *runHandle) awaitExit(t *testing.T, within time.Duration) runResult {
	t.Helper()
	select {
	case result := <-h.done:
		h.collected.Store(true)
		return result
	case <-time.After(within):
		t.Fatalf("the runtime never exited on its own within %s", within)
		return runResult{}
	}
}

func (h *runHandle) stop(t *testing.T) runResult {
	t.Helper()
	h.cancel()
	select {
	case result := <-h.done:
		h.collected.Store(true)
		return result
	case <-time.After(20 * time.Second):
		t.Fatal("serve did not stop after cancellation")
		return runResult{}
	}
}

func TestRunPublishesACompleteManifestOnlyAfterListening(t *testing.T) {
	vault := newTestVault(t)
	listening := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseListen := func() { releaseOnce.Do(func() { close(release) }) }
	handle := startRun(t, vault, func(opts *Options) {
		opts.Listen = func(network, address string) (net.Listener, error) {
			close(listening)
			<-release
			return net.Listen(network, address)
		}
	})
	// Registered after startRun so it runs first: a failed assertion must not
	// leave Run blocked inside Listen while cleanup waits for it to stop.
	t.Cleanup(releaseListen)

	// While the listener is still being created no manifest may exist: a client
	// that can read runtime.json can always connect to what it names.
	select {
	case <-listening:
	case <-time.After(20 * time.Second):
		t.Fatal("serve never attempted to listen")
	}
	require.NoFileExists(t, appruntime.ManifestPath(vault.path), "the manifest must not precede the listener")
	releaseListen()
	manifest := handle.waitForManifest(t, vault.path)

	require.Equal(t, os.Getpid(), manifest.PID)
	require.Equal(t, appruntime.ModeAttached, manifest.Mode)
	require.NotEmpty(t, manifest.RunID)
	require.NotEmpty(t, manifest.ControlToken)
	require.NotEmpty(t, manifest.Executable)
	require.Equal(t, ProcessBuildID(), manifest.BuildID)
	require.Equal(t, vault.path, manifest.VaultPath)
	require.Greater(t, manifest.HTTPPort, 0)
	require.Contains(t, manifest.HTTPURL, "127.0.0.1")

	info, err := os.Stat(appruntime.ManifestPath(vault.path))
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		// Windows has no Unix permission bits to report; the file is still
		// created owner-only through the ACL Go applies for 0600.
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the manifest carries the control token")
	}

	result := handle.stop(t)
	require.NoError(t, result.err)
	require.NoFileExists(t, appruntime.ManifestPath(vault.path), "the owner removes its own manifest")
}

func TestRunHealthAnswersTheManifestIdentity(t *testing.T) {
	vault := newTestVault(t)
	handle := startRun(t, vault, nil)
	manifest := handle.waitForManifest(t, vault.path)

	health, err := appruntime.Probe(context.Background(), manifest)
	require.NoError(t, err, "a published manifest must be probe-verifiable")
	require.Equal(t, manifest.RunID, health.RunID)
	require.Equal(t, manifest.PID, health.PID)
	require.Equal(t, appruntime.ModeAttached, health.Mode)
	require.Equal(t, ProcessBuildID(), health.BuildID)
	require.Equal(t, vault.path, health.VaultPath)
	if health.Lane.Busy {
		require.Equal(t, string(lane.KindValidationRefresh), health.Lane.JobKind,
			"startup validation can use the lane without an installed explicit-index hook")
	}

	_ = handle.stop(t)
}

func TestRunExitsWhenItLosesElectionAndLeavesTheWinnerUntouched(t *testing.T) {
	vault := newTestVault(t)

	// A winner holds the election lock and has published its manifest.
	lockPath := appruntime.LockPath(vault.path)
	release, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { _ = release() })

	winner := appruntime.InstanceManifest{
		InstanceID: appruntime.InstanceID(vault.path),
		VaultName:  vault.name,
		VaultPath:  vault.path,
		PID:        999001,
		RunID:      "winner-run",
		Mode:       appruntime.ModeHeadless,
		HTTPURL:    "http://127.0.0.1:59999",
		HTTPPort:   59999,
	}
	require.NoError(t, appruntime.WriteManifest(vault.path, winner))
	lockBefore, err := os.ReadFile(lockPath)
	require.NoError(t, err)

	var stderr bytes.Buffer
	var listened bool
	err = Run(context.Background(), Options{
		VaultName: vault.name,
		Host:      "127.0.0.1",
		// Headless is the mode a client spawns, and the mode that would
		// otherwise open the winner's log file.
		Headless:    true,
		CommandName: "serve",
		Stderr:      &stderr,
		Listen: func(network, address string) (net.Listener, error) {
			listened = true
			return net.Listen(network, address)
		},
	})

	require.ErrorIs(t, err, ErrAlreadyRunning)
	require.False(t, listened, "a loser must never bind a port")
	require.NoFileExists(t, appruntime.LogPath(vault.path), "the loser must not touch the winner's log")
	require.Contains(t, stderr.String(), "already owns this vault")
	require.Contains(t, stderr.String(), "999001", "the loser names the winner's pid")
	require.Contains(t, stderr.String(), winner.HTTPURL)

	after, err := appruntime.ReadManifest(vault.path)
	require.NoError(t, err)
	require.Equal(t, winner.RunID, after.RunID, "the loser must not overwrite the winner's manifest")
	lockAfter, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	require.Equal(t, lockBefore, lockAfter, "the loser must not touch the winner's lock")
}

func TestRunHeadlessCapturesOutputAndNeverOpensABrowser(t *testing.T) {
	vault := newTestVault(t)
	handle := startRun(t, vault, func(o *Options) {
		o.Headless = true
		o.Open = true // headless must win over an explicit open request
	})
	manifest := handle.waitForManifest(t, vault.path)
	require.Equal(t, appruntime.ModeHeadless, manifest.Mode)

	select {
	case url := <-handle.opened:
		t.Fatalf("a headless runtime opened a browser at %s", url)
	case <-time.After(300 * time.Millisecond):
	}

	_ = handle.stop(t)

	data, err := os.ReadFile(appruntime.LogPath(vault.path))
	require.NoError(t, err, "a headless runtime logs to .rhizome/runtime.log")
	require.Contains(t, string(data), "Rhizome serve")
	require.Empty(t, handle.stderr.String(), "headless output goes to the log, not the terminal")
}

func TestRunHeadlessExitsWhenIdle(t *testing.T) {
	vault := newTestVault(t)
	handle := startRun(t, vault, func(o *Options) {
		o.Headless = true
		o.IdleTimeout = 50 * time.Millisecond
	})

	result := handle.awaitExit(t, 30*time.Second)
	require.NoError(t, result.err)
	require.NoFileExists(t, appruntime.ManifestPath(vault.path))
}

func TestRunAttachedNeverIdleExits(t *testing.T) {
	vault := newTestVault(t)
	handle := startRun(t, vault, func(o *Options) {
		// An attached runtime ignores the idle timeout entirely.
		o.IdleTimeout = time.Millisecond
	})
	handle.waitForManifest(t, vault.path)

	select {
	case result := <-handle.done:
		t.Fatalf("an attached runtime idle-exited: %v", result.err)
	case <-time.After(500 * time.Millisecond):
	}
	_ = handle.stop(t)
}

func TestRunExitsWhenTheVaultRootDisappears(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot remove a directory whose files a live runtime holds open; the root watcher only fires after the process exits there")
	}
	vault := newTestVault(t)
	handle := startRun(t, vault, func(o *Options) {
		// Let the intent watcher observe disappearance first. Symlink
		// resolution changing must not cause a silent startup cancellation.
		o.RootPoll = time.Second
	})
	handle.waitForManifest(t, vault.path)

	// The live runtime is still writing under .rhizome (heartbeats, log), so a
	// single RemoveAll can lose the race with a file it creates mid-walk; a
	// user's rm -rf behaves the same way. Keep removing until it is gone.
	require.Eventually(t, func() bool {
		_ = os.RemoveAll(vault.path)
		_, err := os.Stat(vault.path)
		return os.IsNotExist(err)
	}, 10*time.Second, 50*time.Millisecond)

	result := handle.awaitExit(t, 30*time.Second)
	require.NoError(t, result.err)
	// The runtime's own writes (manifest, heartbeat) can recreate the root
	// between the removal and the guard's check, so the database guard may
	// name the replaced database instead. Either reason is the exit under test.
	stderr := handle.stderr.String()
	require.True(t, strings.Contains(stderr, "vault root") || strings.Contains(stderr, "was replaced on disk"), stderr)
	registry, err := appruntime.NewRegistry()
	require.NoError(t, err)
	intents, err := registry.ListIntents(context.Background())
	require.NoError(t, err)
	require.Empty(t, intents, "removing the vault must not strand its startup intent")
}

func TestRunAttachesToALiveRuntimeInsteadOfStartingASecond(t *testing.T) {
	vault := newTestVault(t)
	// Only an attached owner is joined; a headless one is replaced.
	live := publishFakeLiveRuntime(t, vault, appruntime.ModeAttached)

	var stderr bytes.Buffer
	opened := make(chan string, 1)
	var listened bool
	err := Run(context.Background(), Options{
		VaultName:    vault.name,
		Host:         "127.0.0.1",
		CommandName:  "start",
		AttachIfLive: true,
		Open:         true,
		Stderr:       &stderr,
		Listen: func(network, address string) (net.Listener, error) {
			listened = true
			return net.Listen(network, address)
		},
		OpenBrowser: func(url string) error { opened <- url; return nil },
	})

	require.NoError(t, err, "attaching is a success, not a conflict")
	require.False(t, listened, "attaching must not start a second runtime")
	require.Equal(t, live.HTTPURL, <-opened)
	require.Contains(t, stderr.String(), "already serving this vault")
	require.Contains(t, stderr.String(), "999042")
}

func TestRunReportsAConflictAgainstALiveRuntime(t *testing.T) {
	vault := newTestVault(t)
	// Only an attached owner is a conflict; a headless one is replaced.
	live := publishFakeLiveRuntime(t, vault, appruntime.ModeAttached)

	var stderr bytes.Buffer
	err := Run(context.Background(), Options{
		VaultName:   vault.name,
		Host:        "127.0.0.1",
		CommandName: "serve",
		Stderr:      &stderr,
		Listen: func(network, address string) (net.Listener, error) {
			return nil, errors.New("serve must not listen against a live runtime")
		},
	})

	require.ErrorIs(t, err, ErrAlreadyRunning)
	require.Contains(t, stderr.String(), live.HTTPURL)
	require.Contains(t, stderr.String(), "999042")
}

// publishFakeLiveRuntime writes a manifest backed by a health endpoint that
// answers with the manifest's own run id and PID, which is what makes it live.
func publishFakeLiveRuntime(t *testing.T, vault testVault, mode appruntime.Mode) appruntime.InstanceManifest {
	t.Helper()
	manifest := appruntime.InstanceManifest{
		InstanceID: appruntime.InstanceID(vault.path),
		VaultName:  vault.name,
		VaultPath:  vault.path,
		PID:        999042,
		RunID:      "live-run",
		Mode:       mode,
		Ready:      true,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != appruntime.HealthPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(appruntime.Health{
			InstanceID: manifest.InstanceID,
			RunID:      manifest.RunID,
			PID:        manifest.PID,
			VaultPath:  vault.path,
			Mode:       manifest.Mode,
			Ready:      true,
		})
	}))
	t.Cleanup(server.Close)
	manifest.HTTPURL = server.URL
	require.NoError(t, appruntime.WriteManifest(vault.path, manifest))
	return manifest
}

func TestRunAttachedReplacesAHeadlessRuntime(t *testing.T) {
	// A human asking for the UI outranks the client-spawned runtime, whichever
	// command they used: the headless one is stopped and the attached one wins
	// election and publishes.
	for _, command := range []string{"serve", "start"} {
		t.Run(command, func(t *testing.T) {
			vault := newTestVault(t)
			headless := startRun(t, vault, func(o *Options) { o.Headless = true })
			first := headless.waitForManifest(t, vault.path)
			require.Equal(t, appruntime.ModeHeadless, first.Mode)

			attached := startRun(t, vault, func(o *Options) {
				o.CommandName = command
				o.AttachIfLive = command == "start"
			})
			result := headless.awaitExit(t, 30*time.Second)
			require.NoError(t, result.err, "the headless runtime exits on the replacement's shutdown request")
			second := attached.waitForManifest(t, vault.path)
			require.Equal(t, appruntime.ModeAttached, second.Mode)
			require.NotEqual(t, first.RunID, second.RunID)
			require.Contains(t, attached.stderr.String(), "replacing headless vault runtime")

			_ = attached.stop(t)
		})
	}
}

func TestRunReusesItsPortAfterACleanRestart(t *testing.T) {
	vault := newTestVault(t)
	reuse := func(opts *Options) { opts.ReuseDiscoveryPort = true }

	first := startRun(t, vault, reuse)
	port := first.waitForManifest(t, vault.path).HTTPPort
	require.NoError(t, first.stop(t).err)
	require.NoFileExists(t, appruntime.ManifestPath(vault.path))

	second := startRun(t, vault, reuse)
	require.Equal(t, port, second.waitForManifest(t, vault.path).HTTPPort)
	_ = second.stop(t)
}

func TestRunHeadlessSurvivesUnavailableDiagnosticsCapture(t *testing.T) {
	vault := newTestVault(t)
	require.NoError(t, os.WriteFile(filepath.Join(vault.path, ".rhizome", "diagnostics"), []byte("synthetic obstruction"), 0600))
	headless := startRun(t, vault, func(o *Options) { o.Headless = true })
	manifest := headless.waitForManifest(t, vault.path)
	require.Equal(t, appruntime.ModeHeadless, manifest.Mode)
	result := headless.stop(t)
	require.NoError(t, result.err, "unavailable diagnostics capture must not prevent runtime startup or shutdown")
}
