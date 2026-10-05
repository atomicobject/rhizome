package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

const (
	installLockName    = "install.lock"
	defaultLockTimeout = 30 * time.Second
	defaultLockPoll    = 250 * time.Millisecond
)

// EnsureOptions controls a self-healing pinned repo-binary install.
type EnsureOptions struct {
	// CfgDir is the repo root containing .rhizome/config.yml.
	CfgDir string
	// Config is the parsed repo config carrying the rhizome.version pin.
	Config *obsidian.LocalConfig
	// ManifestURL optionally overrides the latest GitHub Release API URL. When
	// empty, RZM_UPDATE_MANIFEST_URL and the public repository apply, matching
	// `rzm update`.
	ManifestURL string
	// Progress receives optional human-readable progress lines (stderr in the
	// CLI). The normal success path stays silent so the bootstrap notice
	// remains a single line.
	Progress io.Writer
	// HTTPClient overrides the HTTP client used for release and artifact
	// fetches.
	HTTPClient *http.Client
	// LockTimeout bounds how long to wait on a held install lock.
	LockTimeout time.Duration
	// LockPoll is the sleep between lock acquisition attempts.
	LockPoll time.Duration
}

// EnsurePinnedBinary installs the exact repo-pinned Rhizome version to the
// repo-local binary path using the same release metadata, SHA256, smoke-test, and
// atomic-replace safety as `rzm update --pinned`. It never prompts and never
// rewrites the pin. Concurrent invocations coordinate through
// .rhizome/install.lock; waiters that observe a matching install finish
// without reinstalling.
func EnsurePinnedBinary(ctx context.Context, opts EnsureOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(opts.CfgDir) == "" {
		return errors.New("repo config directory is required")
	}
	if opts.Config == nil {
		return errors.New("repo config is required")
	}
	pin := NormalizeVersion(opts.Config.Rhizome.Version)
	if pin == "" || pin == "latest" {
		return errors.New("rhizome.version pin is required in .rhizome/config.yml")
	}
	if opts.Progress == nil {
		opts.Progress = io.Discard
	}
	if strings.TrimSpace(opts.ManifestURL) == "" {
		opts.ManifestURL = os.Getenv("RZM_UPDATE_MANIFEST_URL")
	}
	if opts.LockTimeout <= 0 {
		opts.LockTimeout = defaultLockTimeout
	}
	if opts.LockPoll <= 0 {
		opts.LockPoll = defaultLockPoll
	}

	target, _, err := targetPath("", opts.CfgDir, opts.Config)
	if err != nil {
		return err
	}
	if pinnedBinaryCurrent(target, pin) {
		return nil
	}

	release, satisfied, err := acquireInstallLock(ctx, opts, target, pin)
	if err != nil {
		return err
	}
	if satisfied {
		return nil
	}
	defer func() { _ = release() }()

	// Another process may have finished the install while we waited.
	if pinnedBinaryCurrent(target, pin) {
		return nil
	}

	manifest, err := fetchPinnedManifest(opts.HTTPClient, opts.ManifestURL, pin)
	if err != nil {
		return err
	}
	artifact, err := manifest.ArtifactFor(CurrentPlatformKey())
	if err != nil {
		return stageErr(StageManifest, err)
	}
	if err := installArtifact(ctx, opts.HTTPClient, artifact, target); err != nil {
		return err
	}
	if err := WriteVersionMarker(target, pin); err != nil {
		return stageErr(StageReplace, err)
	}
	return nil
}

// pinnedBinaryCurrent reports whether the repo-local binary exists and its
// version marker matches the pin.
func pinnedBinaryCurrent(target, pin string) bool {
	if _, err := os.Stat(target); err != nil {
		return false
	}
	marker, ok := ReadVersionMarker(target)
	return ok && marker == pin
}

// acquireInstallLock acquires .rhizome/install.lock, polling while it is held.
// satisfied is true when another process installed a matching binary while we
// waited, in which case no lock is held and no install is needed.
func acquireInstallLock(ctx context.Context, opts EnsureOptions, target, pin string) (release func() error, satisfied bool, err error) {
	lockPath := filepath.Join(opts.CfgDir, ".rhizome", installLockName)
	deadline := time.Now().Add(opts.LockTimeout)
	waited := false
	for {
		release, acquired, err := indexlock.TryAcquire(lockPath)
		if err != nil {
			return nil, false, err
		}
		if acquired {
			return release, false, nil
		}
		if pinnedBinaryCurrent(target, pin) {
			return nil, true, nil
		}
		if time.Now().After(deadline) {
			return nil, false, fmt.Errorf("another rzm process holds %s; wait for it to finish or run `rzm update --pinned`", lockPath)
		}
		if !waited {
			waited = true
			fmt.Fprintf(opts.Progress, "rzm: waiting for another rzm process to finish installing Rhizome %s…\n", pin)
		}
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-time.After(opts.LockPoll):
		}
	}
}

// fetchPinnedManifest resolves the exact release directly so pinned bootstrap
// does not spend a GitHub API request discovering a latest version it will not use.
func fetchPinnedManifest(client *http.Client, manifestURL, version string) (Manifest, error) {
	manifest, err := FetchVersionManifest(client, manifestURL, version)
	if err != nil {
		return Manifest{}, stageErr(StageManifest, err)
	}
	return manifest, nil
}
