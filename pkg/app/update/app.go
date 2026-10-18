package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/appbundle"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
)

const desktopUpdateFeed = "https://github.com/atomicobject/rhizome/releases/latest/download/latest.json"
const desktopDownloadURL = "https://github.com/atomicobject/rhizome/releases/latest"

type appUpdateDependencies struct {
	feedURL string
	launch  func(context.Context, string, ...string) error
	read    func(string) (appbundle.Info, error)
	wait    func(context.Context, time.Duration) error
	timeout time.Duration
}

func defaultAppUpdateDependencies(env vaultconfig.Env) appUpdateDependencies {
	feedURL, _ := env.Lookup("RZM_DESKTOP_UPDATE_FEED")
	return appUpdateDependencies{
		feedURL: firstNonEmpty(strings.TrimSpace(feedURL), desktopUpdateFeed),
		launch: func(ctx context.Context, name string, args ...string) error {
			out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
			if err != nil {
				return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
			}
			return nil
		},
		read: appbundle.Read,
		wait: func(ctx context.Context, duration time.Duration) error {
			timer := time.NewTimer(duration)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
		timeout: 5 * time.Minute,
	}
}

// updateApp delegates application mutation to the application's verified updater.
// A failed hand-off is retained alongside the independent repository outcome.
func updateApp(ctx context.Context, opts Options, bundle appbundle.Info) (TargetResult, error) {
	outcome := TargetResult{Role: UpdateTargetGlobal, Path: bundle.Root, Version: NormalizeVersion(bundle.Version)}
	failed := func(stage string, err error) (TargetResult, error) {
		return outcome, fmt.Errorf("global target %s not updated (%s stage): %w; download %s", bundle.Root, stage, err, desktopDownloadURL)
	}
	if bundle.Dev {
		return failed("app", fmt.Errorf("the Dev app is updated with make desktop-install-dev"))
	}
	if !bundle.Rhizome() {
		return failed("app", fmt.Errorf("%s is not Rhizome Desktop (%s)", bundle.Root, bundle.Identifier))
	}
	deps := defaultAppUpdateDependencies(vaultconfig.FromContext(ctx))
	if opts.appDeps != nil {
		deps = *opts.appDeps
	}
	ctx, cancel := context.WithTimeout(ctx, deps.timeout)
	defer cancel()
	available, err := fetchAppVersion(ctx, opts.HTTPClient, deps.feedURL)
	if err != nil {
		return failed("feed", err)
	}
	comparison, comparable := CompareVersions(available, bundle.Version)
	if !comparable {
		return failed("feed", fmt.Errorf("cannot compare app versions %q and %q", available, bundle.Version))
	}
	if comparison <= 0 {
		fmt.Fprintf(opts.Stdout, "Global app %s is up to date (Rhizome %s)\n", bundle.Root, outcome.Version)
		return outcome, nil
	}
	if err := deps.launch(ctx, "open", "-n", "-a", bundle.Root, "--args", "--install-update"); err != nil {
		return failed("launch", err)
	}
	for elapsed := time.Duration(0); elapsed < deps.timeout; elapsed += time.Second {
		if err := deps.wait(ctx, time.Second); err != nil {
			return failed("wait", err)
		}
		current, err := deps.read(bundle.Root)
		// The updater may briefly move the bundle while replacing it.
		if err == nil && current.Version != bundle.Version {
			comparison, comparable := CompareVersions(current.Version, bundle.Version)
			if !comparable || comparison <= 0 {
				return failed("wait", fmt.Errorf("app changed to unexpected version %q", current.Version))
			}
			outcome.Version = NormalizeVersion(current.Version)
			outcome.BinaryUpdated = true
			return outcome, nil
		}
	}
	return failed("wait", fmt.Errorf("timed out waiting for the app update after %s", deps.timeout))
}

func fetchAppVersion(ctx context.Context, client *http.Client, endpoint string) (string, error) {
	if client == nil {
		client = defaultHTTPClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "rhizome-updater")
	response, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("app update feed returned HTTP %d", response.StatusCode)
	}
	var feed struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&feed); err != nil {
		return "", err
	}
	if strings.TrimSpace(feed.Version) == "" {
		return "", fmt.Errorf("app update feed missing version")
	}
	return feed.Version, nil
}
