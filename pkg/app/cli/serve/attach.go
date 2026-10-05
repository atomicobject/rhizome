package serve

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/vault/version"
)

// Attach and election-loss handling: what a serve invocation does when another
// runtime already owns the vault, and how this process identifies itself.

// attachToLiveRuntime handles the case where this vault already has a runtime.
// A headless runtime is replaced by either command: a human asking for the UI
// outranks the background runtime a client spawned, and an attached runtime
// never idle-exits under an open browser. Against an attached runtime,
// `rzm start` opens its UI and exits successfully, while `rzm serve` reports
// the winner and exits with the already-running code. handled is false when
// this process should proceed to election.
func attachToLiveRuntime(ctx context.Context, vaultPath string, stderr io.Writer, opts Options) (bool, error) {
	client, health, err := appruntime.LiveManifest(ctx, vaultPath)
	if err != nil || client == nil {
		return false, nil
	}
	manifest := client.Manifest
	if health.Mode == appruntime.ModeHeadless {
		fmt.Fprintf(stderr, "replacing headless vault runtime (pid %d)…\n", manifest.PID)
		if err := appruntime.RequestShutdown(ctx, client, appruntime.StopGrace); err != nil {
			if ctx.Err() != nil {
				return true, ctx.Err()
			}
			if killErr := appruntime.TerminateHeadless(vaultPath, manifest); killErr != nil {
				return true, fmt.Errorf("replace headless runtime pid %d: %w", manifest.PID, err)
			}
			if err := appruntime.WaitForExit(ctx, manifest, appruntime.StopGrace); err != nil {
				return true, err
			}
		}
		return false, nil
	}
	if !opts.AttachIfLive {
		fmt.Fprint(stderr, appruntime.FormatStartup(manifest, !health.Ready))
		fmt.Fprintf(stderr, "another Rhizome runtime (pid %d, %s) already serves this vault\n", manifest.PID, manifest.Mode)
		return true, ErrAlreadyRunning
	}
	fmt.Fprintf(stderr, "Rhizome is already serving this vault\n  pid:   %d\n  mode:  %s\n  url:   %s\n",
		manifest.PID, manifest.Mode, manifest.HTTPURL)
	if opts.Open && opts.OpenBrowser != nil {
		if err := opts.OpenBrowser(manifest.HTTPURL); err != nil {
			fmt.Fprintf(stderr, "warning: open browser failed: %v\n", err)
		}
	}
	return true, nil
}

// reportElectionLoss prints the winner's published manifest. It only reads:
// a loser must never touch the winner's manifest, lock, or log.
func reportElectionLoss(stderr io.Writer, vaultPath string) {
	if pid, live := appruntime.LegacyOwner(vaultPath); live {
		fmt.Fprintf(stderr, "an older Rhizome serve (pid %d) still owns this vault; stop it, then rerun\n", pid)
		return
	}
	manifest, err := appruntime.ReadManifest(vaultPath)
	if err != nil {
		if pid, live := appruntime.LockOwner(vaultPath); live {
			fmt.Fprintf(stderr, "another process (pid %d) holds %s but publishes no runtime manifest; if it is not a Rhizome runtime, stop it or remove the lock\n", pid, appruntime.LockPath(vaultPath))
			return
		}
		fmt.Fprintf(stderr, "another Rhizome runtime already owns this vault (%s)\n", appruntime.LockPath(vaultPath))
		return
	}
	fmt.Fprintf(stderr, "another Rhizome runtime already owns this vault\n  pid:   %d\n  mode:  %s\n  url:   %s\n  stop:  rzm stop\n",
		manifest.PID, manifest.Mode, manifest.HTTPURL)
}

// newManifest builds this runtime's identity. BuildID is computed once per
// process so replacing the binary on disk cannot change what a running runtime
// claims to be.
func newManifest(rt *bootstrap.LiveRuntime, vaultName string, mode appruntime.Mode, runID string) (appruntime.InstanceManifest, error) {
	var err error
	if runID == "" {
		runID, err = appruntime.NewRunID()
		if err != nil {
			return appruntime.InstanceManifest{}, err
		}
	}
	token, err := appruntime.NewControlToken()
	if err != nil {
		return appruntime.InstanceManifest{}, err
	}
	executable, _ := os.Executable()
	return appruntime.InstanceManifest{
		InstanceID:   appruntime.InstanceID(rt.VaultPath),
		VaultName:    vaultName,
		VaultPath:    rt.VaultPath,
		PID:          os.Getpid(),
		Version:      version.Version,
		RunID:        runID,
		Mode:         mode,
		BuildID:      ProcessBuildID(),
		Executable:   executable,
		ControlToken: token,
		StartedAt:    time.Now().UTC(),
		Ready:        false,
	}, nil
}

// ProcessBuildID identifies the code this process runs. It is computed once:
// `make build` replacing the executable must not change a running runtime's
// answer, or a client would see it flip identity mid-life.
func ProcessBuildID() string {
	buildIDOnce.Do(func() {
		executable, _ := os.Executable()
		buildIDValue = appruntime.BuildID(version.Version, executable)
	})
	return buildIDValue
}
