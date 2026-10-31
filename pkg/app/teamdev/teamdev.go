// Package teamdev starts the local team API gateway that development builds
// of rzm use. A development build signed in to a loopback origin calls Ensure
// before its first paid call; Ensure starts `team-api dev` from beside the rzm
// executable when nothing answers, with live upstreams and the development
// keys from the login keychain. It never starts fake upstreams, so fake
// vectors cannot enter a real index.
package teamdev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

// DefaultOrigin is where development builds expect the local gateway.
const DefaultOrigin = "http://127.0.0.1:8787"

const startTimeout = 15 * time.Second

// DataDir is the per-user directory holding the local gateway's SQLite file,
// diagnostics, and log. It is resolved from the real OS account.
func DataDir() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return filepath.Join(u.HomeDir, ".config", "rhizome", "team-api-dev"), nil
}

// Ensure returns nil when a live gateway answers at origin, starting one first
// if origin is loopback and nothing answers. It refuses a running gateway with
// fake upstreams, such as a leftover `make team-api-dev` on the same port.
// Non-loopback origins are left alone.
func Ensure(ctx context.Context, origin string) error {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || !loopback(u.Hostname()) {
		return nil
	}
	if healthy(ctx, origin) {
		return checkLive(ctx, origin)
	}
	dir, err := DataDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	unlock, err := lock(filepath.Join(dir, "ensure.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	if healthy(ctx, origin) { // another rzm started it while we waited
		return checkLive(ctx, origin)
	}
	if err := start(u.Host, dir); err != nil {
		return err
	}
	deadline := time.Now().Add(startTimeout)
	for time.Now().Before(deadline) {
		if healthy(ctx, origin) {
			return checkLive(ctx, origin)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("local team API gateway did not start within %s; see %s", startTimeout, filepath.Join(dir, "team-api.log"))
}

func start(listen, dir string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	bin := filepath.Join(filepath.Dir(self), "team-api")
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("local team API gateway not found at %s; run make build: %w", bin, err)
	}
	logFile, err := os.OpenFile(filepath.Join(dir, "team-api.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := exec.Command(bin, "dev", "--listen", listen, "--data-dir", dir, "--upstreams", "live", "--keys-from-keychain", "--idle-exit", "1h")
	// The gateway gets no inherited environment: repository dotenv values and
	// provider keys in this process must not reach it.
	cmd.Env = []string{"HOME=" + os.Getenv("HOME"), "PATH=/usr/bin:/bin:/usr/sbin:/sbin", "TMPDIR=" + os.TempDir()}
	cmd.Stdout, cmd.Stderr = logFile, logFile
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func checkLive(ctx context.Context, origin string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+wire.RouteMeta, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("local team API gateway at %s: %w", origin, err)
	}
	defer resp.Body.Close()
	var meta wire.Meta
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&meta); err != nil {
		return fmt.Errorf("local team API gateway at %s returned unreadable metadata: %w", origin, err)
	}
	if meta.Upstreams == "fake" {
		return fmt.Errorf("the gateway at %s uses fake providers, which would put fake vectors in a real index; stop it (for example `make team-api-dev`) so rzm can start a live one", origin)
	}
	return nil
}

var client = &http.Client{Transport: &http.Transport{Proxy: nil}}

func healthy(ctx context.Context, origin string) bool {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+wire.RouteHealth, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

var errUnsupported = errors.New("starting the local team API gateway is supported on macOS and Linux only")
