package serve

import (
	"errors"
	"io"
	"net"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
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
	BackgroundIndexer IndexJobRunner
	// RegisterHooks installs the lane- and code-mode-owned control hooks.
	RegisterHooks func(*ControlHooks, *bootstrap.LiveRuntime)

	// IdleTimeout overrides the configured headless idle timeout. Tests set it
	// short; zero resolves from `runtime.idleTimeout`.
	IdleTimeout time.Duration
	// IdlePoll and RootPoll override the lifecycle poll intervals for tests.
	IdlePoll time.Duration
	RootPoll time.Duration

	// RunTasks starts a pass over the vault's scheduled tasks; nil turns
	// the task interval off. TasksFirstDelay and TasksInterval override the
	// timing for tests.
	RunTasks        func(vaultPath string) error
	TasksFirstDelay time.Duration
	TasksInterval   time.Duration
}
