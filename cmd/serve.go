package cmd

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"runtime"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	appserve "github.com/atomicobject/rhizome/pkg/app/cli/serve"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

var (
	serveListen    = net.Listen
	openBrowserCmd = openBrowser
)

type serveCommandOptions struct {
	host               string
	port               int
	reuseDiscoveryPort bool
	open               bool
	headless           bool
	commandName        string
	htmlContentOrigin  string
	applicationOrigin  string
}

// newServeCommand adapts Cobra to the vault runtime in pkg/app/cli/serve.
// Both commands take over from a headless runtime. Against a live attached
// runtime `start` opens its UI, while `serve` reports a conflict and exits with
// appruntime.ExitCodeAlreadyRunning (SPEC-0104 US5).
func newServeCommand(use, short string, defaultOpen bool) *cobra.Command {
	opts := &serveCommandOptions{
		host:        "127.0.0.1",
		port:        0,
		open:        defaultOpen,
		commandName: use,
	}

	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			err := appserve.Run(cmd.Context(), appserve.Options{
				VaultName:           vaultName,
				Host:                opts.host,
				Port:                opts.port,
				ReuseDiscoveryPort:  opts.reuseDiscoveryPort,
				Open:                opts.open && !opts.headless,
				Headless:            opts.headless,
				AttachIfLive:        use == "start",
				Debug:               debug,
				CommandName:         opts.commandName,
				HTMLContentOrigin:   opts.htmlContentOrigin,
				ApplicationOrigin:   opts.applicationOrigin,
				Listen:              func(network, address string) (net.Listener, error) { return serveListen(network, address) },
				OpenBrowser:         func(url string) error { return openBrowserCmd(url) },
				Stderr:              cmd.ErrOrStderr(),
				FinalizeDiagnostics: diagnosticCommandFinalizer(cmd.Context()),
				BackgroundIndexer: func(ctx context.Context, noteMetadata notemeta.Indexer, vaultPath string, vaultDef obsidian.VaultDefinition, dbg bool) error {
					return runBackgroundIndex(ctx, noteMetadata, vaultPath, vaultDef, dbg, "serve")
				},
				RegisterHooks: func(hooks *appserve.ControlHooks, rt *bootstrap.LiveRuntime) {
					registerIndexJobHooks(hooks, rt)
					registerAgentOpHooks(hooks, rt)
				},
			})
			if errors.Is(err, appserve.ErrAlreadyRunning) {
				return silentExitError{code: appruntime.ExitCodeAlreadyRunning}
			}
			return err
		},
	}

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.Flags().StringVarP(&vaultName, "vault", "v", "", "vault name (uses default if unset)")
	cmd.Flags().StringVar(&opts.host, "host", opts.host, "host interface to bind")
	cmd.Flags().IntVar(&opts.port, "port", opts.port, "port to bind (0 selects a free port)")
	cmd.Flags().BoolVar(&opts.reuseDiscoveryPort, "reuse-discovery-port", true, "when port=0, reuse the last port this worktree served on (.rhizome/runtime-port), falling back to a random port if it is taken (--reuse-discovery-port=false to opt out)")
	cmd.Flags().BoolVar(&opts.open, "open", opts.open, "open the UI in the default browser")
	cmd.Flags().BoolVar(&opts.headless, "headless", false, "run detached with no browser, logging to .rhizome/diagnostics and exiting when idle")
	cmd.Flags().StringVar(&opts.htmlContentOrigin, "html-content-origin", "", "separate HTML content origin, for example https://{token}.content.example.com (loopback defaults to {token}.localhost)")
	cmd.Flags().StringVar(&opts.applicationOrigin, "application-origin", "", "externally reachable HTTP(S) application origin for non-loopback access; also required with --html-content-origin")
	return cmd
}

var serveCmd = newServeCommand("serve", "Run Rhizome freshness services and HTTP UI/API", false)
var startCmd = newServeCommand("start", "Run Rhizome freshness services and HTTP UI/API, opening the browser by default", true)

func init() {
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(startCmd)
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
