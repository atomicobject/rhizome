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
	"path/filepath"
	"syscall"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/config"
	"github.com/atomicobject/rhizome/services/team-api/internal/core"
	"github.com/atomicobject/rhizome/services/team-api/internal/identity"
	"github.com/atomicobject/rhizome/services/team-api/internal/store"
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
	fmt.Fprintln(os.Stderr, "usage: team-api serve --config <file> | team-api dev --data-dir <dir> [--listen 127.0.0.1:8788] [--upstreams fake|live] [--keys-from-keychain] [--idle-exit 1h]")
	os.Exit(2)
}

func serve(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	path := fs.String("config", "", "configuration file")
	_ = fs.Parse(args)
	if *path == "" {
		return errors.New("--config is required")
	}
	cfg, err := config.Load(*path)
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
	db, err := store.Open(filepath.Join(cfg.DataDir, "team-api.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	diagRoot := cfg.DataDir
	if cfg.DiagnosticsDir != "" {
		diagRoot = cfg.DiagnosticsDir
	}
	rec, err := diagnostics.Open(diagRoot, diagnostics.Options{Role: "team-api", Version: version})
	if err != nil {
		slog.Warn("diagnostics unavailable", "error", err)
	}
	defer rec.Close()

	// Identity reads usage from the traffic ledger, and traffic reads the
	// runtime configuration from identity; the ledger is filled in below.
	usage := &laterUsage{}
	id, err := identity.New(cfg, db, usage, rec, time.Now)
	if err != nil {
		return err
	}
	traf, err := traffic.New(trafficConfig(cfg, keys), db, id.Runtime(), rec, time.Now)
	if err != nil {
		return err
	}
	usage.UsageReader = traf
	id.Routes(mux)
	paid := http.NewServeMux()
	traf.Routes(paid)
	mux.Handle("/v1/providers/", id.Middleware(paid))
	mux.HandleFunc("GET "+wire.RouteHealth, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	idle := newIdleTracker()
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           idle.wrap(mux),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      180 * time.Second,
		IdleTimeout:       360 * time.Second,
	}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	go func() {
		if idleExit > 0 {
			idle.wait(ctx, idleExit)
		} else {
			<-ctx.Done()
		}
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	slog.Info("team-api listening", "addr", ln.Addr().String(), "mode", cfg.Mode, "data_dir", cfg.DataDir)
	if ready != nil {
		ready <- ln.Addr().String()
	}
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
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
