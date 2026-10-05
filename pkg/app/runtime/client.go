package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Client talks to one live runtime. Construct it through LiveManifest or Ensure.
type Client struct {
	Manifest InstanceManifest
	HTTP     *http.Client
}

// URL joins a control path onto the runtime's base URL.
func (c *Client) URL(path string) string { return c.Manifest.HTTPURL + path }

// NewRequest builds an authorized request against the runtime.
func (c *Client) NewRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.URL(path), payload)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	SetBearerToken(req, c.Manifest.ControlToken)
	SetDiagnosticIdentity(req, ctx)
	return req, nil
}

// Discovery never follows redirects: a manifest grants authority only to its
// recorded listener, not another address supplied by an HTTP response.
var probeHTTPClient = &http.Client{
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
}

// Probe asks the process at the manifest's address for its health and verifies
// it is the process the manifest describes (same run id and PID). A stale
// manifest whose port was reused by something else fails here rather than
// being trusted.
//
// Error classes: ErrNoRuntime when nothing accepts connections (the manifest
// is written only after the listener is up, so a refused connection means the
// writer is gone); ErrManifestMismatch when a different process answers;
// ErrRuntimeUnresponsive when the address accepts but does not answer
// correctly within ProbeTimeout (booting, wedged, or health not yet wired).
func Probe(ctx context.Context, manifest InstanceManifest) (Health, error) {
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifest.HTTPURL+HealthPath, nil)
	if err != nil {
		return Health{}, err
	}
	resp, err := probeHTTPClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Health{}, fmt.Errorf("%w: %v", ErrRuntimeUnresponsive, err)
		}
		return Health{}, fmt.Errorf("%w: %v", ErrNoRuntime, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Health{}, fmt.Errorf("%w: health returned %d", ErrRuntimeUnresponsive, resp.StatusCode)
	}
	var health Health
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return Health{}, fmt.Errorf("%w: %v", ErrManifestMismatch, err)
	}
	if health.RunID != manifest.RunID || health.PID != manifest.PID {
		return Health{}, ErrManifestMismatch
	}
	return health, nil
}

// LiveManifest returns a client for the vault's runtime when the manifest
// exists and its probe confirms the same run id and PID. It never spawns.
// ErrNoRuntime means "nothing usable is there" (missing manifest, nobody
// listening, or another process answering); ErrRuntimeUnresponsive means the
// manifest's PID is alive and its address accepts connections but health did
// not answer correctly, so callers wait rather than spawn.
func LiveManifest(ctx context.Context, vaultPath string) (*Client, Health, error) {
	manifest, err := ReadManifest(vaultPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, Health{}, ErrNoRuntime
		}
		// A torn or corrupt manifest is not a runtime; the next owner
		// overwrites it, so callers may replace rather than wait forever.
		return nil, Health{}, fmt.Errorf("%w: %v", ErrNoRuntime, err)
	}
	health, err := Probe(ctx, manifest)
	if err != nil {
		if errors.Is(err, ErrRuntimeUnresponsive) && pidExists(manifest.PID) {
			return nil, Health{}, fmt.Errorf("%w (pid %d)", ErrRuntimeUnresponsive, manifest.PID)
		}
		return nil, Health{}, ErrNoRuntime
	}
	return &Client{Manifest: manifest, HTTP: &http.Client{Timeout: 0}}, health, nil
}

// EnsureOptions controls how a client finds or starts a runtime.
type EnsureOptions struct {
	VaultPath string
	// Executable is the binary to spawn; empty means os.Executable().
	Executable string
	// BuildID is the caller's own build; a headless runtime with another build is replaced.
	BuildID string
	// Replace shuts down a live headless runtime once even when its build
	// matches, so the caller gets a fresh process. An attached one is an error.
	Replace bool
	// Autostart false only attaches; ErrAutostartDisabled when nothing is live.
	Autostart bool
	// Wait blocks until the manifest is probe-ready (up to Budget). False
	// returns immediately after triggering a spawn or observing an elected
	// owner, for callers that only need freshness eventually.
	Wait   bool
	Budget time.Duration
	// Logf receives progress lines ("starting vault runtime…"); nil discards.
	Logf func(format string, args ...any)
}

// EnsureResult reports what Ensure did.
type EnsureResult struct {
	Client  *Client
	Health  Health
	Spawned bool
}
