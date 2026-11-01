package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/config"
	"github.com/atomicobject/rhizome/services/team-api/internal/devidp"
	"github.com/atomicobject/rhizome/services/team-api/internal/fakeupstream"
)

const (
	devClientID   = "rhizome-dev"
	devIdPPath    = "/dev/idp"
	devVoyagePath = "/dev/upstream/voyage"
	devTSPath     = "/dev/upstream/typesafe"
	// developmentKeychainService is where scripts/secrets/run.py
	// sync-keychain stores the development provider keys.
	developmentKeychainService = "rhizome-development"
)

// dev runs the gateway, the development identity provider, and (with
// --upstreams fake) fake providers on one loopback listener. Live upstreams
// use the development keys from the environment or the login keychain.
func dev(ctx context.Context, args []string) error {
	return devWithReady(ctx, args, nil)
}

func devWithReady(ctx context.Context, args []string, ready chan<- string) error {
	fs := flag.NewFlagSet("dev", flag.ExitOnError)
	listen := fs.String("listen", "127.0.0.1:8788", "loopback address")
	dataDir := fs.String("data-dir", "", "directory for the SQLite file and diagnostics")
	upstreams := fs.String("upstreams", "fake", "fake or live")
	fromKeychain := fs.Bool("keys-from-keychain", false, "read live keys from the rhizome-development keychain items")
	idleExit := fs.Duration("idle-exit", 0, "exit after this long without requests (0 never)")
	_ = fs.Parse(args)
	if *dataDir == "" {
		return errors.New("--data-dir is required")
	}
	origin := "http://" + *listen
	raw := map[string]any{
		"mode":             config.ModeDev,
		"listen":           *listen,
		"data_dir":         *dataDir,
		"issuer":           origin + devIdPPath,
		"client_id":        devClientID,
		"hd_hint":          "atomicobject.com",
		"accepted_domains": []string{"atomicobject.com"},
		"admins":           []string{"dev-admin@atomicobject.com"},
		"eligibility_mode": wire.EligibilityAll,
		// Generous local limits; the admin API can lower them.
		"person_limits": wire.DailyLimits{Requests: 100_000, InputBytes: 20 << 30, ChargeCredits: 1 << 40},
		"global_limits": wire.DailyLimits{Requests: 1_000_000, InputBytes: 200 << 30, ChargeCredits: 1 << 44},
	}
	var keys providerKeys
	switch *upstreams {
	case "fake":
		raw["upstreams"] = config.Upstreams{Voyage: origin + devVoyagePath, TypeSafe: origin + devTSPath}
		key := syntheticKey()
		keys = providerKeys{Voyage: key, TypeSafe: key}
	case "live":
		var err error
		if keys, err = liveKeys(*fromKeychain); err != nil {
			return err
		}
	default:
		return fmt.Errorf("--upstreams must be fake or live, got %q", *upstreams)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	cfg, err := config.Parse(data)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	idp, err := devidp.New(devidp.Config{Issuer: cfg.Issuer, ClientID: devClientID, Identities: devidp.DefaultIdentities()})
	if err != nil {
		return err
	}
	mux.Handle(devIdPPath+"/", idp.Handler())
	if *upstreams == "fake" {
		mux.Handle(devVoyagePath+"/", http.StripPrefix(devVoyagePath, fakeupstream.NewVoyage(fakeupstream.Options{Key: keys.Voyage})))
		mux.Handle(devTSPath+"/", http.StripPrefix(devTSPath, fakeupstream.NewTypeSafe(fakeupstream.Options{Key: keys.TypeSafe})))
	}
	return run(ctx, cfg, keys, mux, *idleExit, ready)
}

func syntheticKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "fake-" + hex.EncodeToString(b)
}

// liveKeys takes the development keys from this process's environment (as
// `scripts/secrets/run.py development` provides them) or, with fromKeychain,
// from the login keychain items that run.py's sync-keychain writes.
func liveKeys(fromKeychain bool) (providerKeys, error) {
	keys := providerKeys{Voyage: os.Getenv("VOYAGE_API_KEY"), TypeSafe: os.Getenv("TYPESAFE_API_KEY")}
	if fromKeychain {
		for name, dst := range map[string]*string{"VOYAGE_API_KEY": &keys.Voyage, "TYPESAFE_API_KEY": &keys.TypeSafe} {
			if *dst != "" {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			out, err := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", developmentKeychainService, "-a", name, "-w").Output()
			cancel()
			if err != nil {
				return keys, fmt.Errorf("%s is not in the %s keychain items; run ./scripts/with-secrets sync-keychain", name, developmentKeychainService)
			}
			*dst = strings.TrimSpace(string(out))
		}
	}
	return keys, keys.check()
}

// idleTracker records the last request so a development gateway started on
// demand can exit when nobody uses it.
type idleTracker struct{ last atomic.Int64 }

func newIdleTracker() *idleTracker {
	t := &idleTracker{}
	t.last.Store(time.Now().UnixNano())
	return t
}

func (t *idleTracker) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.last.Store(time.Now().UnixNano())
		next.ServeHTTP(w, r)
		t.last.Store(time.Now().UnixNano())
	})
}

func (t *idleTracker) wait(ctx context.Context, idle time.Duration) {
	tick := time.NewTicker(idle / 10)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if time.Since(time.Unix(0, t.last.Load())) > idle {
				return
			}
		}
	}
}
