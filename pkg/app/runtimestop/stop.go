// Package runtimestop stops vault runtimes for `rzm stop` and the desktop app.
package runtimestop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

// exitPollInterval is how often `rzm stop` re-checks that a runtime's PID is
// gone. Stopping is interactive, so it is deliberately fast.
const exitPollInterval = 100 * time.Millisecond

// StopOptions configures `rzm stop`.
type StopOptions struct {
	// VaultPath is the vault whose runtime to stop; ignored when All is set.
	VaultPath string
	// All stops every runtime in the global instance registry.
	All bool
	// Grace bounds how long a headless runtime may ignore graceful shutdown
	// before it is terminated. Zero uses appruntime.StopGrace.
	Grace time.Duration
	Out   io.Writer
}

// Process operations are indirected so tests can drive a runtime that ignores
// shutdown without spawning one. Production uses the shared platform-aware
// liveness check and the headless-only terminator from pkg/app/runtime.
var (
	processIsAlive     = indexlock.PIDExists
	processTerminateFn = appruntime.TerminateHeadless
)

// ErrStopIncomplete reports that at least one runtime did not stop. An attached
// runtime that ignores shutdown is reported rather than killed: a human owns
// that terminal and gets to decide (SPEC-0104 US5).
var ErrStopIncomplete = errors.New("one or more runtimes did not stop")

// Stop shuts down the vault's runtime, or every registered runtime with --all.
func Stop(ctx context.Context, opts StopOptions) error {
	out := opts.Out
	if out == nil {
		out = io.Discard
	}
	grace := opts.Grace
	if grace <= 0 {
		grace = appruntime.StopGrace
	}

	listCtx, cancelList := context.WithTimeout(ctx, grace)
	defer cancelList()
	targets, discoveryErr := stopTargets(listCtx, opts)
	if discoveryErr != nil && len(targets) == 0 {
		return discoveryErr
	}
	if len(targets) == 0 {
		fmt.Fprintln(out, "no running Rhizome runtime found")
		return nil
	}

	failed := discoveryErr != nil
	if discoveryErr != nil {
		fmt.Fprintf(out, "runtime discovery incomplete: %v\n", discoveryErr)
	}
	for _, vaultPath := range targets {
		if err := stopOne(ctx, vaultPath, grace, out); err != nil {
			fmt.Fprintf(out, "%s: %v\n", vaultPath, err)
			failed = true
		}
	}
	if failed {
		return ErrStopIncomplete
	}
	return nil
}

func stopTargets(ctx context.Context, opts StopOptions) ([]string, error) {
	if !opts.All {
		if opts.VaultPath == "" {
			return nil, errors.New("stop requires a vault")
		}
		return []string{opts.VaultPath}, nil
	}
	registry, err := appruntime.NewRegistry()
	if err != nil {
		return nil, err
	}
	instances, err := registry.List()
	if err != nil {
		return nil, err
	}
	intents, discoveryErr := registry.ListIntents(ctx)
	paths := make([]string, 0, len(instances)+len(intents))
	seen := make(map[string]bool)
	for _, instance := range instances {
		key := appruntime.InstanceID(instance.VaultPath)
		if !seen[key] {
			paths = append(paths, instance.VaultPath)
			seen[key] = true
		}
	}
	for _, intent := range intents {
		key := appruntime.InstanceID(intent.VaultPath)
		if !seen[key] {
			paths = append(paths, intent.VaultPath)
			seen[key] = true
		}
	}
	return paths, discoveryErr
}

func stopOne(ctx context.Context, vaultPath string, grace time.Duration, out io.Writer) error {
	deadline := time.Now().Add(grace)
	gateCtx, cancelGate := context.WithDeadline(ctx, deadline)
	defer cancelGate()
	registry, err := appruntime.NewRegistry()
	if err != nil {
		return err
	}
	var client *appruntime.Client
	var targetToken string
	var targetPID int
	var unresponsive *appruntime.InstanceManifest
	for {
		intent, pending, err := registry.StartIntentFor(gateCtx, vaultPath)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				if unresponsive != nil && indexlock.PIDExists(unresponsive.PID) {
					return terminateUnresponsive(ctx, vaultPath, *unresponsive, grace, out)
				}
				if pid, live := appruntime.LockOwner(vaultPath); live {
					return fmt.Errorf("runtime pid %d still holds the vault without a responding runtime manifest after %s", pid, grace)
				}
				return fmt.Errorf("runtime startup has not acknowledged stop within %s", grace)
			}
			return fmt.Errorf("read runtime startup: %w", err)
		}
		if pending && targetToken == "" {
			targetToken, targetPID = intent.Token, intent.OwnerPID
			_, cancelled, cancelErr := registry.CancelIntent(gateCtx, vaultPath, targetToken)
			if cancelErr != nil {
				return fmt.Errorf("cancel runtime startup: %w", cancelErr)
			}
			if !cancelled {
				if owner, live := appruntime.LockOwner(vaultPath); !live || owner != targetPID {
					fmt.Fprintf(out, "stopped %s startup\n", vaultPath)
					return nil
				}
			}
		}
		if pending && targetToken != "" && intent.Token != targetToken {
			// A fresh start owns a new token. This invocation stopped the
			// original owner and must not cancel its successor.
			fmt.Fprintf(out, "stopped %s startup\n", vaultPath)
			return nil
		}
		client, _, err = appruntime.LiveManifest(ctx, vaultPath)
		if err == nil && client != nil {
			if targetToken != "" && client.Manifest.RunID != targetToken {
				fmt.Fprintf(out, "stopped %s startup\n", vaultPath)
				return nil
			}
			break
		}
		if errors.Is(err, appruntime.ErrRuntimeUnresponsive) {
			// Publication precedes Serve by a small window, and a slow owner
			// may be draining. Give cancellation its full grace before escalation.
			if manifest, readErr := appruntime.ReadManifest(vaultPath); readErr == nil {
				if targetToken != "" && manifest.RunID != targetToken {
					fmt.Fprintf(out, "stopped %s startup\n", vaultPath)
					return nil
				}
				unresponsive = &manifest
			}
		} else {
			unresponsive = nil
		}
		// Startup owns runtime.lock before it can publish a manifest. Shutdown
		// removes the manifest before releasing that lock. Neither window is
		// evidence that the runtime has stopped.
		pid, live := appruntime.LockOwner(vaultPath)
		spawnBusy := liveSpawnLease(vaultPath)
		if pending && intent.Cancelled && !live && !spawnBusy && (intent.OwnerPID == 0 || !indexlock.PIDExists(intent.OwnerPID)) {
			_ = registry.ClearIntent(ctx, vaultPath, intent.Token)
			fmt.Fprintf(out, "stopped %s startup\n", vaultPath)
			return nil
		}
		if !live && !pending && !spawnBusy {
			if targetToken != "" {
				fmt.Fprintf(out, "stopped %s startup\n", vaultPath)
			} else {
				fmt.Fprintf(out, "%s: no running runtime\n", vaultPath)
			}
			return nil
		}
		if time.Now().After(deadline) {
			if unresponsive != nil && indexlock.PIDExists(unresponsive.PID) {
				return terminateUnresponsive(ctx, vaultPath, *unresponsive, grace, out)
			}
			if live {
				return fmt.Errorf("runtime pid %d still holds the vault without a responding runtime manifest after %s", pid, grace)
			}
			return fmt.Errorf("runtime startup has not acknowledged stop within %s", grace)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(exitPollInterval):
		}
	}
	manifest := client.Manifest
	fmt.Fprintf(out, "stopping %s (pid %d, %s)\n", vaultPath, manifest.PID, manifest.Mode)

	// RequestShutdown waits for the listener to go away. The process is the
	// ground truth: a PID that exited within the grace period was graceful even
	// if its listener lingered, and one that is still alive gets the
	// mode-dependent policy below.
	// A runtime releases runtime.lock as the last step of a graceful exit, so a
	// lock that named this PID and no longer does is as good as the PID being
	// gone. The PID alone is not enough: an exited runtime whose parent has not
	// reaped it yet (a supervisor, an IDE task runner) still exists.
	lockNamesRuntime := func() bool {
		owner, _ := appruntime.LockOwner(vaultPath)
		return owner == manifest.PID
	}
	var released func() bool
	if lockNamesRuntime() {
		released = func() bool { return !lockNamesRuntime() }
	}
	if err := appruntime.RequestShutdown(ctx, client, grace); err != nil && ctx.Err() == nil {
		fmt.Fprintf(out, "%s: graceful shutdown pending: %v\n", vaultPath, err)
	}
	if waitForExit(ctx, manifest.PID, grace, released) {
		fmt.Fprintf(out, "stopped %s (pid %d)\n", vaultPath, manifest.PID)
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return terminateUnresponsive(ctx, vaultPath, manifest, grace, out)
}

func liveSpawnLease(vaultPath string) bool {
	path := appruntime.SpawnLockPath(vaultPath)
	data, ok := indexlock.ReadLockData(path)
	if ok {
		return data.PID > 0 && indexlock.PIDExists(data.PID)
	}
	_, err := os.Stat(path)
	return err == nil
}

// terminateUnresponsive applies the mode-dependent policy for a runtime that
// outlived its grace period.
func terminateUnresponsive(ctx context.Context, vaultPath string, manifest appruntime.InstanceManifest, grace time.Duration, out io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if manifest.Mode != appruntime.ModeHeadless {
		return fmt.Errorf("attached runtime (pid %d) did not stop within %s; stop it in its own terminal", manifest.PID, grace)
	}
	fmt.Fprintf(out, "terminating headless runtime (pid %d) after %s\n", manifest.PID, grace)
	if err := processTerminateFn(vaultPath, manifest); err != nil {
		return fmt.Errorf("terminate pid %d: %w", manifest.PID, err)
	}
	if !waitForExit(ctx, manifest.PID, grace, nil) {
		if err := ctx.Err(); err != nil {
			return err
		}
		return fmt.Errorf("pid %d is still running after termination", manifest.PID)
	}
	fmt.Fprintf(out, "terminated pid %d\n", manifest.PID)
	return nil
}

// waitForExit reports whether pid disappeared, or released reported true,
// within the deadline. released may be nil.
func waitForExit(ctx context.Context, pid int, within time.Duration, released func() bool) bool {
	deadline := time.Now().Add(within)
	for {
		if ctx.Err() != nil {
			return false
		}
		if !processIsAlive(pid) || (released != nil && released()) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		if !sleepStop(ctx, exitPollInterval) {
			return false
		}
	}
}

func sleepStop(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
