package serve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/app/web"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/logging"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// ErrAlreadyRunning reports that another runtime already owns this vault. The
// command layer turns it into appruntime.ExitCodeAlreadyRunning; the message
// naming the winner has already been printed.
var ErrAlreadyRunning = errors.New("another Rhizome runtime already owns this vault")

const (
	registryHeartbeatInterval = 15 * time.Second
	httpShutdownGrace         = 5 * time.Second
)

// Options configures one serve invocation. The function-valued fields are the
// seams the Cobra adapter and tests supply.
type Options struct {
	VaultName          string
	Host               string
	Port               int
	ReuseDiscoveryPort bool
	Open               bool
	Headless           bool
	// AttachIfLive makes this invocation join a live attached runtime (open its
	// UI and exit successfully) instead of reporting a conflict. `rzm start`
	// sets it. A headless runtime is replaced either way.
	AttachIfLive      bool
	Debug             bool
	CommandName       string
	HTMLContentOrigin string
	ApplicationOrigin string

	// Listen binds the HTTP listener; nil uses net.Listen.
	Listen func(network, address string) (net.Listener, error)
	// OpenBrowser opens the UI; nil disables browser opening.
	OpenBrowser func(url string) error
	// Stderr receives human-facing startup and shutdown lines. A headless
	// runtime redirects it to the runtime log.
	Stderr io.Writer
	// FinalizeDiagnostics completes the command-owned recorder while this
	// process still owns the vault. It runs after runtime resources drain.
	FinalizeDiagnostics func(error)
	// BackgroundIndexer performs the boot catch-up index.
	BackgroundIndexer bootstrap.BackgroundIndexer
	// RegisterHooks installs the lane- and code-mode-owned control hooks.
	RegisterHooks func(*ControlHooks, *bootstrap.LiveRuntime)

	// IdleTimeout overrides the configured headless idle timeout. Tests set it
	// short; zero resolves from `runtime.idleTimeout`.
	IdleTimeout time.Duration
	// IdlePoll and RootPoll override the lifecycle poll intervals for tests.
	IdlePoll time.Duration
	RootPoll time.Duration
}

// Run is the vault runtime process (SPEC-0104). It elects an owner before it
// binds anything, serves the UI, API, and loopback control routes, publishes
// the manifest only once it is reachable, and removes it before releasing the
// election lock.
func Run(ctx context.Context, opts Options) (resultErr error) {
	vaultDef, vaultName, err := resolveVault(opts.VaultName)
	if err != nil {
		return err
	}
	vaultPath := vaultDef.BasePath()
	// Keep registry lookups stable if the original path traverses a symlink
	// that disappears while this runtime is alive.
	intentVaultPath := paths.ResolveSymlinks(vaultPath).String()
	registry, err := appruntime.NewRegistry()
	if err != nil {
		return fmt.Errorf("open runtime registry: %w", err)
	}
	spawnToken := ""
	if opts.Headless {
		spawnToken = os.Getenv(appruntime.SpawnTokenEnv)
	}
	intentToken := spawnToken
	if intentToken == "" {
		intentToken, err = appruntime.NewRunID()
		if err != nil {
			return err
		}
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), appruntime.StopGrace)
		defer cancel()
		_ = registry.ClearIntent(cleanupCtx, intentVaultPath, intentToken)
		_ = registry.ClearEarlyRegistration(cleanupCtx, intentVaultPath, intentToken)
	}()
	if spawnToken != "" {
		intent, found, err := registry.StartIntentFor(ctx, intentVaultPath)
		if err != nil || !found || intent.Token != spawnToken || intent.Cancelled {
			return nil
		}
	}

	stderr := opts.Stderr
	if stderr == nil {
		stderr = io.Discard
	}

	if opts.Headless {
		if recorder := diagnostics.FromContext(ctx); recorder != nil {
			// The command recorder predates process capture and may hold inherited
			// native stderr. Keep structured history without bypassing raw bounds.
			defer recorder.SetStderr(io.Discard)()
		}
	}

	// A live runtime already owns this vault: attach instead of racing it.
	if handled, err := attachToLiveRuntime(ctx, vaultPath, stderr, opts); handled {
		return err
	}

	var ownedRecorder *diagnostics.Recorder
	if diagnostics.FromContext(ctx) == nil {
		options, diagnosticErr := diagnostics.LoadOptions(vaultPath)
		if diagnosticErr != nil {
			fmt.Fprintf(stderr, "WARN [diagnostics] configuration unavailable (%s); using bounded defaults\n", logging.ClassifyError(diagnosticErr))
		}
		options.Role = "runtime"
		options.Stderr = stderr
		if opts.Headless {
			options.Stderr = io.Discard
		}
		recorder, openErr := diagnostics.Open(vaultPath, options)
		if openErr != nil {
			fmt.Fprintf(stderr, "WARN [diagnostics] persistence unavailable (%s)\n", logging.ClassifyError(openErr))
		}
		if recorder != nil {
			ctx = diagnostics.WithRecorder(ctx, recorder)
			ownedRecorder = recorder
			defer recorder.Close()
		}
	}
	op := diagnostics.NewOperation("runtime.serve", "start")
	parent := diagnostics.OperationFromContext(ctx)
	op.ParentID = parent.ID
	if parent.TraceID != "" {
		op.TraceID = parent.TraceID
	}
	ctx = diagnostics.WithOperation(ctx, op)
	if recorder := diagnostics.FromContext(ctx); recorder != nil {
		recorder.Event(ctx, slog.LevelInfo, "runtime", "startup.started", "", slog.Bool("headless", opts.Headless))
	}
	var capture *LogCapture
	var stopped bool
	var stoppedURL string
	var boundaryPanicked bool
	var finalizeOnce sync.Once
	restoreDiagnosticsLog := logging.InstallStandard(ctx)
	finalizeDiagnostics := func() {
		finalizeOnce.Do(func() {
			// Headless human output belongs to the raw sink, which must finish
			// draining before another process may write this vault's files.
			if opts.Headless && stopped {
				fmt.Fprintf(stderr, "Rhizome serve stopped\n  url:   %s\n", stoppedURL)
			}
			if capture != nil {
				_ = capture.Close()
			}
			status, reason := "success", ""
			finalErr := resultErr
			if finalErr != nil {
				status, reason = "error", logging.ClassifyError(finalErr)
				if errors.Is(finalErr, context.Canceled) || errors.Is(finalErr, appruntime.ErrStartCancelled) {
					status, reason = "canceled", "canceled"
					finalErr = context.Canceled
				}
				if errors.Is(finalErr, ErrAlreadyRunning) {
					status, reason = "skipped", "runtime_already_owned"
				}
			}
			if boundaryPanicked {
				status, reason = "error", "handler_panicked"
				finalErr = errors.New("diagnostic boundary panicked")
			}
			logging.CompleteQuiet(ctx, op, status, reason, map[string]any{"headless": opts.Headless})
			restoreDiagnosticsLog()
			if opts.FinalizeDiagnostics != nil {
				opts.FinalizeDiagnostics(finalErr)
			}
			if ownedRecorder != nil {
				_ = ownedRecorder.Close()
			}
		})
	}
	defer func() {
		panicked := recover()
		if panicked != nil {
			boundaryPanicked = true
		}
		finalizeDiagnostics()
		if panicked != nil {
			panic(panicked)
		}
	}()
	serveCtx, stopServe := context.WithCancel(ctx)
	defer stopServe()
	var intentWatcherDone chan struct{}
	defer func() {
		stopServe()
		if intentWatcherDone != nil {
			<-intentWatcherDone
		}
	}()

	readiness := NewReadinessCoordinator(opts.BackgroundIndexer)
	defer func() {
		stopServe()
		readiness.Drain()
	}()
	rt, err := bootstrap.NewLiveRuntime(serveCtx, bootstrap.LiveOptions{
		VaultName:              vaultName,
		Debug:                  opts.Debug,
		BackgroundIndexer:      readiness.BackgroundIndexer,
		BeforeOwnershipRelease: finalizeDiagnostics,
		// The write-stability barrier belongs to the owner: a process that
		// loses election must never recover another runtime's journals.
		OnElected: func() (barrierErr error) {
			defer func() {
				if panicked := recover(); panicked != nil {
					boundaryPanicked = true
					panic(panicked)
				}
				resultErr = barrierErr
			}()
			early := appruntime.InstanceManifest{
				InstanceID: appruntime.InstanceID(vaultPath), VaultName: vaultName,
				VaultPath: vaultPath, PID: os.Getpid(), RunID: intentToken,
				Mode: appruntime.ModeAttached,
			}
			if opts.Headless {
				early.Mode = appruntime.ModeHeadless
			}
			if err := registry.ElectIntent(serveCtx, intentVaultPath, intentToken, spawnToken != "", early); err != nil {
				return err
			}
			intentWatcherDone = make(chan struct{})
			go watchStartIntent(serveCtx, registry, intentVaultPath, intentToken, stopServe, intentWatcherDone)
			return RecoverInterruptedWrites(serveCtx, vaultDef)
		},
	})
	if err != nil {
		// Publish the failure so an already-scheduled background callback exits.
		readiness.BindLive(serveCtx, nil)
		if errors.Is(err, appruntime.ErrStartCancelled) || (errors.Is(err, context.Canceled) && ctx.Err() == nil) {
			if opts.Headless {
				return nil
			}
			return appruntime.ErrStartCancelled
		}
		return err
	}
	// Election is synchronous inside NewLiveRuntime, so a loser exits before it
	// can bind a port, write a manifest, or touch the winner's files.
	if recorder := diagnostics.FromContext(ctx); recorder != nil {
		recorder.Event(ctx, slog.LevelInfo, "runtime", "runtime.election", "", slog.Bool("won", rt.ElectionWon()), slog.Bool("headless", opts.Headless))
	}
	if !rt.ElectionWon() {
		_ = rt.Close()
		readiness.BindLive(serveCtx, nil)
		reportElectionLoss(stderr, vaultPath)
		return ErrAlreadyRunning
	}
	defer func() {
		panicked := recover()
		if panicked != nil {
			boundaryPanicked = true
		}
		stopServe()
		readiness.Drain()
		_ = rt.Close()
		// Attached callers observe this completion line after ownership was
		// released; it never writes the detached runtime's capture files.
		if !opts.Headless && stopped {
			fmt.Fprintf(stderr, "Rhizome serve stopped\n  url:   %s\n", stoppedURL)
		}
		if panicked != nil {
			panic(panicked)
		}
	}()

	mode := appruntime.ModeAttached
	if opts.Headless {
		// Only the vault's owner may write the diagnostic output sink, so output
		// capture starts after election, never before it.
		mode = appruntime.ModeHeadless
		capture, err = captureProcessOutput(serveCtx, appruntime.LogPath(vaultPath), LogMaxBytes)
		if err != nil {
			recordServeDiagnostic(serveCtx, slog.LevelWarn, "output_capture.unavailable", err)
			// Detached children still have inherited native stderr (or null on
			// Windows). Losing diagnostic capture must not lose runtime availability.
			capture = nil
			stderr = io.Discard
		} else {
			stderr = capture.Writer()
		}
	}

	webRuntime := readiness.BindLive(serveCtx, rt)

	manifest, err := newManifest(rt, vaultName, mode, intentToken)
	if err != nil {
		return err
	}
	state := newManifestState(manifest)

	idle := NewIdleTracker()
	installDatabaseGuard(rt, vaultPath, indexDatabasePath(vaultPath), func(reason string) {
		fmt.Fprintf(stderr, "serve: shutting down: %s\n", reason)
		stopServe()
	})
	hooks := ControlHooks{}
	if opts.RegisterHooks != nil {
		opts.RegisterHooks(&hooks, rt)
	}
	control := BuildRuntimeControl(ControlDeps{
		Context:  serveCtx,
		Manifest: state.Snapshot,
		Live:     rt,
		Idle:     idle,
		Shutdown: func(string) { stopServe() },
		Hooks:    hooks,
	})

	srv, err := web.NewServer(serveCtx, web.Config{
		Vault:                     rt.Vault,
		VaultDef:                  rt.VaultDef,
		VaultPath:                 rt.VaultPath,
		Cache:                     rt.Cache(),
		Runtime:                   webRuntime,
		NoteMetadata:              rt.NoteMetadataIndexer(),
		NotePathOwned:             rt.NotePathOwned,
		EditsRecovered:            true,
		HTMLContentOriginTemplate: opts.HTMLContentOrigin,
		ApplicationOrigin:         opts.ApplicationOrigin,
		RuntimeControl:            control,
	}, web.Assets())
	if err != nil {
		return err
	}
	readiness.BindServer(srv)
	defer func() { _ = srv.Close() }()
	rt.SetGlobalEventSink(readiness.GlobalEventSink())

	listener, err := bindListener(vaultPath, opts)
	if err != nil {
		return err
	}
	defer listener.Close()

	actualPort := listener.Addr().(*net.TCPAddr).Port
	// Best effort: a missing record only costs the next start a stable URL.
	_ = appruntime.WriteLastPort(vaultPath, actualPort)
	url := ServeURL(opts.Host, actualPort)
	published := state.SetAddress(opts.Host, actualPort, url)
	if recorder := diagnostics.FromContext(ctx); recorder != nil {
		recorder.Event(ctx, slog.LevelInfo, "runtime", "runtime.listening", "", slog.String("mode", string(mode)), slog.Int("port", actualPort))
	}

	_ = registry.PruneStale()
	if err := registry.Write(published); err != nil {
		return fmt.Errorf("register serve instance: %w", err)
	}
	defer func() { _ = registry.Remove(published.InstanceID) }()

	// The manifest is the promise that this address answers, so publish it only
	// now, and remove it before rt.Close releases the election lock (defers run
	// last-registered-first, and rt.Close was deferred above).
	if err := registry.PublishManifestWithIntent(serveCtx, intentVaultPath, intentToken, published); err != nil {
		// Without a manifest no client can find or stop this runtime, and
		// every spawn attempt loses election to it. Give the vault back.
		if errors.Is(err, appruntime.ErrStartCancelled) {
			if opts.Headless {
				return nil
			}
			return err
		}
		return fmt.Errorf("publish runtime manifest: %w", err)
	}
	defer func() {
		if err := appruntime.RemoveManifestIfOwner(vaultPath, published.PID); err != nil {
			fmt.Fprintf(stderr, "warning: failed to remove runtime manifest: %v\n", err)
		}
	}()
	var bg sync.WaitGroup
	bg.Add(1)
	go func() {
		defer bg.Done()
		readiness.Bridge(serveCtx)
	}()
	defer func() {
		stopServe()
		bg.Wait()
		readiness.Drain()
	}()

	fmt.Fprint(stderr, appruntime.FormatStartup(published, true))
	if opts.Open && opts.OpenBrowser != nil && !opts.Headless {
		bg.Add(1)
		go func() {
			defer bg.Done()
			timer := time.NewTimer(150 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-serveCtx.Done():
				return
			case <-timer.C:
			}
			if err := opts.OpenBrowser(url); err != nil {
				recordServeDiagnostic(serveCtx, slog.LevelWarn, "browser.open_failed", err)
			}
		}()
	}

	bg.Add(1)
	go func() {
		defer bg.Done()
		heartbeatRegistry(serveCtx, registry, state)
	}()
	bg.Add(1)
	go func() {
		defer bg.Done()
		waitForReady(serveCtx, stderr, registry, rt, state, func(reason string) {
			fmt.Fprintf(stderr, "Rhizome serve stopping: %s\n", reason)
			stopServe()
		})
	}()
	bg.Add(1)
	go func() {
		defer bg.Done()
		watchVaultRoot(serveCtx, vaultPath, opts.RootPoll, func(reason string) {
			fmt.Fprintf(stderr, "Rhizome serve stopping: %s\n", reason)
			stopServe()
		})
	}()
	if opts.Headless {
		timeout, err := resolveIdleTimeout(rt.LocalCfg, opts.IdleTimeout)
		if err != nil {
			return err
		}
		bg.Add(1)
		go func() {
			defer bg.Done()
			watchIdleExit(serveCtx, idle, rt, idleExitConfig{timeout: timeout, poll: opts.IdlePoll}, func(reason string) {
				fmt.Fprintf(stderr, "Rhizome serve stopping: %s\n", reason)
				stopServe()
			})
		}()
	}

	handler := newDrainingHandler(srv.Handler())
	httpSrv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return serveCtx },
	}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-serveCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), httpShutdownGrace)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			_ = httpSrv.Close()
		}
		handler.Drain()
	}()
	defer func() {
		stopServe()
		<-shutdownDone
	}()

	if err := httpSrv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	stoppedURL = url
	stopped = true
	return nil
}

func resolveVault(name string) (obsidian.VaultDefinition, string, error) {
	if name == "" {
		defaultName, err := (&obsidian.Vault{}).DefaultName()
		if err != nil {
			return obsidian.VaultDefinition{}, "", err
		}
		name = defaultName
	}
	def, err := (&obsidian.Vault{Name: name}).Definition()
	if err != nil {
		return obsidian.VaultDefinition{}, "", err
	}
	return def, name, nil
}

var (
	buildIDOnce  sync.Once
	buildIDValue string
)

func resolveIdleTimeout(cfg *obsidian.LocalConfig, override time.Duration) (time.Duration, error) {
	if override > 0 {
		return override, nil
	}
	return appruntime.IdleTimeout(cfg)
}

func bindListener(vaultPath string, opts Options) (net.Listener, error) {
	listen := opts.Listen
	if listen == nil {
		listen = net.Listen
	}
	port := ResolvePort(vaultPath, opts.Port, opts.ReuseDiscoveryPort)
	allowFallback := opts.Port == 0 && opts.ReuseDiscoveryPort
	listener, err := listen("tcp", fmt.Sprintf("%s:%d", opts.Host, port))
	if err == nil {
		return listener, nil
	}
	if !allowFallback || port == 0 || !IsAddrInUse(err) {
		return nil, err
	}
	return listen("tcp", fmt.Sprintf("%s:0", opts.Host))
}

func heartbeatRegistry(ctx context.Context, registry *appruntime.Registry, state *manifestState) {
	if registry == nil {
		return
	}
	ticker := time.NewTicker(registryHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = registry.Write(state.Snapshot())
		}
	}
}

func waitForReady(ctx context.Context, out io.Writer, registry *appruntime.Registry, rt *bootstrap.LiveRuntime, state *manifestState, fail func(reason string)) {
	// A vault with embeddings turned off is fully usable; only a semantic
	// capability that was asked for and failed blocks readiness.
	if err := rt.WaitForSemantic(ctx); err != nil && !errors.Is(err, bootstrap.ErrEmbeddingsDisabled) {
		if !errors.Is(err, context.Canceled) {
			fmt.Fprintf(out, "Rhizome readiness blocked\n  stage: semantic\n  error: %v\n", err)
		}
		return
	}
	if err := rt.WaitForCodeIndex(ctx); err != nil {
		if !errors.Is(err, context.Canceled) {
			fmt.Fprintf(out, "Rhizome readiness blocked\n  stage: code-index\n  error: %v\n", err)
		}
		return
	}
	manifest := state.SetReady(true)
	if registry != nil {
		_ = registry.Write(manifest)
	}
	// The Ready flag is informational (health serves readiness live), and on
	// Windows a rewrite can lose a sharing race with a concurrent reader, so
	// retry briefly and then warn rather than stop a working runtime.
	var writeErr error
	for attempt := 0; attempt < 5; attempt++ {
		if writeErr = appruntime.WriteManifest(rt.VaultPath, manifest); writeErr == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if writeErr != nil {
		fmt.Fprintf(out, "warning: could not record readiness in the runtime manifest: %v\n", writeErr)
	}
	fmt.Fprintf(out, "Rhizome ready\n  url:   %s\n", manifest.HTTPURL)
}

// ServeURL renders the address a user can open.
func ServeURL(host string, port int) string {
	return "http://" + net.JoinHostPort(DisplayHost(host), strconv.Itoa(port))
}

// DisplayHost turns a wildcard bind address into one a browser can reach.
func DisplayHost(host string) string {
	if host == "" || host == "0.0.0.0" || host == "::" {
		return "127.0.0.1"
	}
	return host
}

// ResolvePort reuses the last port this worktree served on, so a restarted
// runtime keeps its URL stable for open browser tabs.
func ResolvePort(vaultPath string, requestedPort int, reuseDiscoveryPort bool) int {
	if requestedPort != 0 || !reuseDiscoveryPort {
		return requestedPort
	}
	return appruntime.ReadLastPort(vaultPath)
}

// IsAddrInUse reports whether a listen error means the port is taken.
func IsAddrInUse(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "address already in use")
}
