// Command team-api runs the Atomic team API gateway.
//
//	team-api serve --config <file>     the deployed gateway (Google sign-in)
//	team-api dev --data-dir <dir> ...  the whole local stack on one loopback
//	                                   listener: gateway, development sign-in,
//	                                   and fake or live providers
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/config"
	"github.com/atomicobject/rhizome/services/team-api/internal/core"
	"github.com/atomicobject/rhizome/services/team-api/internal/identity"
	"github.com/atomicobject/rhizome/services/team-api/internal/traffic"
)

// version is set with -ldflags "-X main.version=..." in release images.
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve(ctx, os.Args[2:])
	case "dev":
		err = dev(ctx, os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "team-api:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: team-api serve [--config <file>] | team-api dev --data-dir <dir> [--listen 127.0.0.1:8788] [--upstreams fake|live] [--keys-from-keychain] [--idle-exit 1h]")
	os.Exit(2)
}

func serve(ctx context.Context, args []string) error {
	// Operational messages use JSON in the deployed process too. Do not log config.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	path := fs.String("config", "", "configuration file")
	_ = fs.Parse(args)
	var cfg config.Config
	var err error
	if *path != "" {
		cfg, err = config.Load(*path)
	} else {
		cfg, err = config.LoadEnvironment()
	}
	if err != nil {
		return err
	}
	keys := providerKeys{Voyage: os.Getenv("VOYAGE_API_KEY"), TypeSafe: os.Getenv("TYPESAFE_API_KEY")}
	return run(ctx, cfg, keys, http.NewServeMux(), 0, nil)
}

// run composes the gateway on mux and serves it until ctx ends, or until no
// request arrives for idleExit when that is positive. ready, when set,
// receives the bound address once the listener is open.
func run(ctx context.Context, cfg config.Config, keys providerKeys, mux *http.ServeMux, idleExit time.Duration, ready chan<- string) error {
	if err := keys.check(); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	if cfg.State.Backend != "dynamodb" {
		unlock, err := lockDataDir(cfg.DataDir)
		if err != nil {
			return err
		}
		defer unlock()
	}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	defer ln.Close()
	st, l, closeState, err := openState(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeState()
	// Keep the lease alive through graceful draining, then stop both workers
	// before closing the backing store.
	serving, cancelServing := context.WithCancel(ctx)
	defer cancelServing()
	stopMaintenance := maintainLedger(l, cancelServing)
	defer stopMaintenance()
	if err := l.Recover(ctx); err != nil {
		return err
	}
	ctx = serving
	diagRoot := cfg.DataDir
	if cfg.DiagnosticsDir != "" {
		diagRoot = cfg.DiagnosticsDir
	}
	opts := diagnostics.Options{Role: "team-api", Version: version}
	var rec *diagnostics.Recorder
	if cfg.DiagnosticsOutput == "stdout" {
		rec = diagnostics.OpenStream(os.Stdout, opts)
	} else {
		rec, err = diagnostics.Open(diagRoot, opts)
	}
	if err != nil {
		slog.Warn("diagnostics unavailable", "error", err)
	}
	defer rec.Close()

	// Identity reads usage from the traffic ledger, and traffic reads the
	// runtime configuration from identity; the ledger is filled in below.
	usage := &laterUsage{}
	id, err := identity.New(cfg, st, usage, rec, time.Now)
	if err != nil {
		return err
	}
	traf, err := traffic.New(trafficConfig(cfg, keys), l, id.Runtime(), rec, time.Now)
	if err != nil {
		return err
	}
	usage.UsageReader = traf
	id.Routes(mux)
	paid := http.NewServeMux()
	traf.Routes(paid)
	mux.Handle("/v1/providers/", id.Middleware(paid))
	mux.HandleFunc("GET "+wire.RouteHealth, func(w http.ResponseWriter, r *http.Request) {
		if ctx.Err() != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	idle := newIdleTracker()
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           idle.wrap(requestDiagnostics(rec, trustedForwardedAddress(cfg.TrustedProxyCIDRs, mux))),
		ReadHeaderTimeout: 5 * time.Second,
		// A backstop for requests rejected before their handler reads the
		// body (for example an unauthenticated logout): Go drains the body
		// after the handler returns, and this bounds that read. Routes that
		// read bodies set their own shorter deadline; once a body is read
		// the deadline is cleared, so long provider calls are unaffected.
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 300 * time.Second,
		IdleTimeout:  360 * time.Second,
	}
	// Serve returns as soon as Shutdown closes the listener; run waits for
	// Shutdown itself so in-flight paid calls finish and settle before the
	// store closes and the data directory is released.
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		if idleExit > 0 {
			idle.wait(ctx, idleExit)
		} else {
			<-ctx.Done()
		}
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	rec.Event(ctx, slog.LevelInfo, "teamapi", "teamapi.listening", "gateway ready")
	if ready != nil {
		ready <- ln.Addr().String()
	}
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-stopped
	return nil
}

type laterUsage struct{ core.UsageReader }

type providerKeys struct{ Voyage, TypeSafe string }

func (k providerKeys) check() error {
	if k.Voyage == "" || k.TypeSafe == "" {
		return errors.New("both VOYAGE_API_KEY and TYPESAFE_API_KEY must be set for the gateway process")
	}
	return nil
}

// trafficConfig takes the provider keys and, in development, the upstream
// overrides. The per-person slot limit is combined across providers, so it is
// the larger of the configured per-provider concurrency values that sessions
// advertise; those values are client hints within it. Global slots, waits,
// and charges keep the traffic defaults.
func trafficConfig(cfg config.Config, keys providerKeys) traffic.Config {
	return traffic.Config{
		VoyageKey:       keys.Voyage,
		TypeSafeKey:     keys.TypeSafe,
		VoyageBaseURL:   cfg.Upstreams.Voyage,
		TypeSafeBaseURL: cfg.Upstreams.TypeSafe,
		PersonSlots:     max(cfg.Concurrency.VoyageConcurrency, cfg.Concurrency.TypeSafeConcurrency),
	}
}
