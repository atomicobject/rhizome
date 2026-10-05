package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

// runIndexThroughRuntime submits the index job to the vault runtime and renders
// its events locally. handled false means the caller should index in this
// process; the reason was already printed. restart asks the outer command to
// restart the old runtime after in-process indexing and cleanup finish.
func runIndexThroughRuntime(cmd *cobra.Command, vaultDef obsidian.VaultDefinition) (handled bool, restart bool, err error) {
	ctx := cmd.Context()
	stderr := cmd.ErrOrStderr()

	opts, err := vaultRuntimeEnsureOptions(vaultDef.BasePath(), true, stderr)
	if err != nil {
		return inProcessFallback(stderr, err, false)
	}
	if !opts.Autostart {
		// Attaching to an already-running runtime is still allowed; only
		// starting one is not.
		if _, _, liveErr := appruntime.LiveManifest(ctx, opts.VaultPath); liveErr != nil {
			return inProcessFallback(stderr, appruntime.ErrAutostartDisabled, false)
		}
	}

	client, job, err := appruntime.EnsureIndexJob(ctx, opts)
	if err != nil {
		if ctx.Err() != nil {
			return true, false, silentExitError{code: 130}
		}
		if runtimeStillOwnsVault(err) {
			// Another process still holds the vault open. Indexing here would
			// wait behind its lock or write a database it has open; the
			// message names the owner and the remedy instead.
			return true, false, err
		}
		return inProcessFallback(stderr, err, false)
	}
	if job.Joined {
		fmt.Fprintf(stderr, "joining index already running (job %s)\n", job.JobID)
	}
	err = renderRuntimeIndexJob(ctx, cmd, client, job.JobID)
	switch {
	case errors.Is(err, errRuntimeDatabaseReplaced):
		// The runtime is shutting down over a file it no longer owns; index
		// here, then bring a fresh runtime back for the new database.
		_ = appruntime.WaitForExit(ctx, client.Manifest, appruntime.StopGrace)
		return inProcessFallback(stderr, lane.ErrDatabaseReplaced, true)
	case err != nil && ctx.Err() == nil && runtimeGone(ctx, client):
		// The runtime exited before the job's events could be read (for
		// example its database guard stopped it). Its lane released the index
		// lock on the way out, so indexing here is safe.
		return inProcessFallback(stderr, fmt.Errorf("vault runtime pid %d exited during the job: %w", client.Manifest.PID, err), true)
	}
	return true, false, err
}

// runtimeStillOwnsVault reports ensure failures that prove another process
// still owns the vault, which in-process indexing must not race.
func runtimeStillOwnsVault(err error) bool {
	return errors.Is(err, appruntime.ErrRuntimeUnresponsive) ||
		errors.Is(err, appruntime.ErrAttachedMismatch) ||
		errors.Is(err, appruntime.ErrLegacyRuntime)
}

// runtimeGone reports that nothing answers at the client's runtime anymore.
func runtimeGone(ctx context.Context, client *appruntime.Client) bool {
	probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), appruntime.StopGrace)
	defer cancel()
	return appruntime.WaitForExit(probeCtx, client.Manifest, appruntime.StopGrace) == nil
}

// errRuntimeDatabaseReplaced is the CLI-side classification of a job that
// failed with lane.ErrDatabaseReplaced.
var errRuntimeDatabaseReplaced = errors.New("runtime index database replaced")

func inProcessFallback(out io.Writer, reason error, restart bool) (bool, bool, error) {
	fmt.Fprintf(out, "Indexing in this process: %v\n", reason)
	return false, restart, nil
}

// renderRuntimeIndexJob streams one job's events onto the usual progress bar
// and maps its outcome onto the CLI exit classes.
func renderRuntimeIndexJob(ctx context.Context, cmd *cobra.Command, client *appruntime.Client, jobID string) error {
	stderr := cmd.ErrOrStderr()
	bar := newCLIProgressBar(stderr)
	defer bar.Close()
	logOut := bar.WrapWriter(stderr)

	// The stream must survive the interrupt that cancels the job so the
	// terminal event still arrives and the lane is left clean.
	streamCtx, stopStream := context.WithCancel(context.WithoutCancel(ctx))
	defer stopStream()
	interrupted := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			close(interrupted)
			cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), appruntime.ProbeTimeout)
			defer cancel()
			if err := appruntime.CancelIndexJob(cancelCtx, client, jobID); err != nil {
				stopStream()
				return
			}
			// Acknowledging cancellation does not prove the job finished. Give
			// it time to drain, but a stuck worker must not trap this terminal.
			timer := time.NewTimer(appruntime.StopGrace)
			defer timer.Stop()
			select {
			case <-streamCtx.Done():
			case <-timer.C:
				stopStream()
			}
		case <-streamCtx.Done():
		}
	}()

	var done lane.Event
	err := appruntime.StreamIndexJob(streamCtx, client, jobID, func(event lane.Event) error {
		switch event.Type {
		case lane.EventProgress:
			bar.SetLabel(event.Label)
			bar.Update(int(event.Done), int(event.Total))
		case lane.EventLog:
			fmt.Fprintln(logOut, event.Line)
		case lane.EventDone:
			done = event
		}
		return nil
	})
	bar.Close()
	if err != nil {
		if wasInterrupted(interrupted) {
			fmt.Fprintln(stderr, "Interrupted; the runtime did not confirm the index job finished. Check `rzm index --status`.")
			return silentExitError{code: 130}
		}
		return err
	}
	if done.Type == lane.EventDone && !done.OK && strings.Contains(done.Error, lane.ErrDatabaseReplaced.Error()) {
		return errRuntimeDatabaseReplaced
	}
	if indexTimings {
		if summary := summaryText(done.Summary); summary != "" {
			fmt.Fprintln(stderr, summary)
		}
	}
	switch done.Outcome {
	case lane.OutcomeOK:
		return nil
	case lane.OutcomeCancelled:
		return silentExitError{code: 130}
	default:
		if done.Error != "" {
			return errors.New(done.Error)
		}
		return fmt.Errorf("indexing failed in the vault runtime (job %s)", jobID)
	}
}

func wasInterrupted(interrupted <-chan struct{}) bool {
	select {
	case <-interrupted:
		return true
	default:
		return false
	}
}

// summaryText renders the job's timing summary, which the lane may send as a
// JSON string or as a structured object.
func summaryText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return string(raw)
}

// stopRuntimeForRebuild clears the way for replacing the database in this
// process: the database is never unlinked while a runtime holds it open. It
// holds both the spawn lease and runtime election lock so neither client
// spawning nor a direct serve can open the database during replacement. The
// caller releases both after the replacement and in-process rebuild finish.
// It returns whether a runtime should be restarted afterwards.
func stopRuntimeForRebuild(ctx context.Context, vaultPath string, out io.Writer) (restart bool, release func(), err error) {
	spawnRelease, err := holdSpawnLease(ctx, vaultPath)
	if err != nil {
		return false, nil, err
	}
	defer func() {
		if release == nil {
			spawnRelease()
		}
	}()
	deadline := time.Now().Add(appruntime.StopGrace)
	for {
		if err := ctx.Err(); err != nil {
			return false, nil, err
		}
		client, health, liveErr := appruntime.LiveManifest(ctx, vaultPath)
		switch {
		case errors.Is(liveErr, appruntime.ErrRuntimeUnresponsive):
			return false, nil, fmt.Errorf("%w; stop it first (`rzm stop`)", liveErr)
		case liveErr == nil && health.Mode == appruntime.ModeAttached:
			return false, nil, fmt.Errorf("a vault runtime (pid %d) has this index open; stop it first (`rzm stop`)", health.PID)
		case liveErr == nil:
			fmt.Fprintf(out, "Stopping headless vault runtime (pid %d) before rebuild…\n", health.PID)
			if err := appruntime.RequestShutdown(ctx, client, appruntime.StopGrace); err != nil {
				if ctx.Err() != nil {
					return false, nil, ctx.Err()
				}
				if killErr := appruntime.TerminateHeadless(vaultPath, client.Manifest); killErr != nil {
					return false, nil, fmt.Errorf("stop vault runtime pid %d: %w", health.PID, err)
				}
				if err := appruntime.WaitForExit(ctx, client.Manifest, appruntime.StopGrace); err != nil {
					return false, nil, err
				}
			}
			restart = true
		}
		electionRelease, acquired, err := indexlock.TryAcquireWithOptions(appruntime.LockPath(vaultPath), indexlock.AcquireOptions{
			Role: "cli/database-replacement", ProcessLifetime: true,
		})
		if err != nil {
			return false, nil, fmt.Errorf("runtime election lock: %w", err)
		}
		if acquired {
			return restart, func() {
				_ = electionRelease()
				spawnRelease()
			}, nil
		}
		if time.Now().After(deadline) {
			return false, nil, fmt.Errorf("a vault runtime is starting or stopping for %s; retry after it settles", vaultPath)
		}
		select {
		case <-ctx.Done():
			return false, nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// holdSpawnLease takes the vault's spawn lease, waiting out a client that is
// mid-spawn, so nothing starts a runtime while the caller replaces the database.
func holdSpawnLease(ctx context.Context, vaultPath string) (func(), error) {
	deadline := time.Now().Add(appruntime.SpawnBudget)
	for {
		release, acquired, err := indexlock.TryAcquire(appruntime.SpawnLockPath(vaultPath))
		if err != nil {
			return nil, fmt.Errorf("spawn lease: %w", err)
		}
		if acquired {
			return func() { _ = release() }, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("another process is starting a runtime for %s; retry shortly", vaultPath)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// restartRuntimeAfterRebuild brings the runtime back without waiting for it.
func restartRuntimeAfterRebuild(ctx context.Context, vaultDef obsidian.VaultDefinition, out io.Writer) {
	if _, _, err := ensureVaultRuntime(context.WithoutCancel(ctx), vaultDef, false, out); err != nil && !errors.Is(err, appruntime.ErrAutostartDisabled) {
		fmt.Fprintf(out, "Could not restart the vault runtime: %v\n", err)
	}
}
