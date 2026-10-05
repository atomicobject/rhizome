package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/logging"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

// ensurePoll is how often Ensure re-probes while waiting for a spawn, a lease
// holder, or an unresponsive runtime.
const ensurePoll = 200 * time.Millisecond

// replacedBuilds remembers, per vault root, the build id this process last
// replaced for a build mismatch. Replacing the same build twice means two
// different binaries are alternating on one vault and neither can win; the
// second replacement warns rather than pretending it converged.
var replacedBuilds sync.Map

// Ensure finds the vault's live runtime or starts a headless one, obeying the
// spawn lease so concurrent callers never race two runtimes into existence.
//
// It attaches to a probe-verified manifest; waits (never spawns) while a
// manifest's PID is alive but unresponsive; and spawns only under
// SpawnLockPath. A headless runtime whose BuildID differs from opts.BuildID is
// shut down and replaced; an attached one returns ErrAttachedMismatch. With
// Autostart false and nothing live it returns ErrAutostartDisabled. With Wait
// false it returns as soon as a spawn was triggered or an elected owner was
// observed, with no client.
func Ensure(ctx context.Context, opts EnsureOptions) (result EnsureResult, resultErr error) {
	op := diagnostics.NewOperation("runtime.ensure", "client")
	parent := diagnostics.OperationFromContext(ctx)
	op.ParentID = parent.ID
	if parent.TraceID != "" {
		op.TraceID = parent.TraceID
	}
	ctx = diagnostics.WithOperation(ctx, op)
	polls, unresponsive, leaseWaits := 0, 0, 0
	replacement := false
	if recorder := diagnostics.FromContext(ctx); recorder != nil {
		recorder.Event(ctx, slog.LevelInfo, "runtime", "ensure.started", "", slog.Bool("wait", opts.Wait), slog.Bool("autostart", opts.Autostart))
	}
	defer func() {
		panicked := recover()
		status, reason := "success", ""
		if resultErr != nil {
			status = "error"
			reason = runtimeDiagnosticReason(resultErr)
			if errors.Is(resultErr, context.Canceled) {
				status = "canceled"
			}
		}
		if panicked != nil {
			status = "error"
			reason = "handler_panicked"
		}
		logging.CompleteQuiet(ctx, op, status, reason, map[string]any{"spawned": result.Spawned, "attached": result.Client != nil, "polls": polls, "unresponsive_probes": unresponsive, "spawn_lease_waits": leaseWaits, "replaced_headless": replacement, "wait": opts.Wait, "autostart": opts.Autostart})
		if panicked != nil {
			panic(panicked)
		}
	}()

	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	budget := opts.Budget
	if budget <= 0 {
		budget = SpawnBudget
	}
	deadline := time.Now().Add(budget)
	spawned := false

	for {
		polls++
		client, health, err := LiveManifest(ctx, opts.VaultPath)
		switch {
		case err == nil:
			if !opts.Replace && !buildMismatch(opts.BuildID, health.BuildID) {
				logf("attached to runtime pid %d", health.PID)
				return EnsureResult{Client: client, Health: health, Spawned: spawned}, nil
			}
			if health.Mode == ModeAttached {
				return EnsureResult{}, fmt.Errorf("%w (pid %d); stop it first (`rzm stop`)", ErrAttachedMismatch, health.PID)
			}
			if err := replaceHeadless(ctx, client, health, opts, logf); err != nil {
				return EnsureResult{}, err
			}
			opts.Replace = false
			replacement = true
		case errors.Is(err, ErrRuntimeUnresponsive):
			unresponsive++
			// The manifest's PID is alive and listening: wait it out rather
			// than spawn a competitor, and report the PID if it never answers.
			if time.Now().After(deadline) {
				return EnsureResult{}, err
			}
			if !sleepCtx(ctx, ensurePoll) {
				return EnsureResult{}, ctx.Err()
			}
			continue
		case errors.Is(err, ErrNoRuntime):
			if legacyErr := LegacyOwnerError(opts.VaultPath); legacyErr != nil {
				// A pre-upgrade serve owns the vault; spawning would only lose
				// election after burning the budget.
				return EnsureResult{}, legacyErr
			}
			// A new runtime holds this lock before it can publish a manifest,
			// and keeps it until shutdown finishes. Wait for its outcome instead
			// of starting a child that will lose election. Callers that only
			// request eventual warmth can return immediately.
			if pid, live := LockOwner(opts.VaultPath); live {
				if !opts.Wait {
					return EnsureResult{}, nil
				}
				if time.Now().After(deadline) {
					return EnsureResult{}, fmt.Errorf("%w: runtime pid %d has no responding manifest within %s", ErrNoRuntime, pid, budget)
				}
				if !sleepCtx(ctx, ensurePoll) {
					return EnsureResult{}, ctx.Err()
				}
				continue
			}
			if !opts.Autostart {
				return EnsureResult{}, ErrAutostartDisabled
			}
			started, err := trySpawn(ctx, opts, logf, deadline, &leaseWaits)
			if err != nil {
				return EnsureResult{}, err
			}
			if started {
				spawned = true
				if !opts.Wait {
					return EnsureResult{Spawned: true}, nil
				}
			}
		default:
			return EnsureResult{}, err
		}
		if time.Now().After(deadline) {
			return EnsureResult{}, fmt.Errorf("%w: no runtime became ready for %s within %s", ErrNoRuntime, opts.VaultPath, budget)
		}
	}
}

func buildMismatch(want, got string) bool {
	return want != "" && got != "" && want != got
}

// trySpawn takes the spawn lease and starts one headless runtime. Losing the
// lease means wait and re-probe, never spawn. The lease is held until the
// manifest probes ready or the budget runs out, so the next caller re-probes
// instead of spawning a competitor.
func trySpawn(ctx context.Context, opts EnsureOptions, logf func(string, ...any), deadline time.Time, leaseWaits *int) (bool, error) {
	release, acquired, err := indexlock.TryAcquire(SpawnLockPath(opts.VaultPath))
	if err != nil || !acquired {
		(*leaseWaits)++
		// A foreign or held lease is not an error for us: another client is
		// spawning, or we cannot judge its owner. Wait and re-probe.
		if !sleepCtx(ctx, ensurePoll) {
			return false, ctx.Err()
		}
		return false, nil
	}
	held := true
	defer func() {
		if held {
			_ = release()
		}
	}()

	// Someone may have spawned and released between our probe and this lease.
	if _, _, err := LiveManifest(ctx, opts.VaultPath); err == nil {
		return false, nil
	}
	if _, live := LockOwner(opts.VaultPath); live {
		return false, nil
	}

	executable := opts.Executable
	if executable == "" {
		if executable, err = os.Executable(); err != nil {
			return false, fmt.Errorf("resolve executable for vault runtime: %w", err)
		}
	}
	logf("starting vault runtime…")
	registry, err := NewRegistry()
	if err != nil {
		return false, fmt.Errorf("register pending runtime: %w", err)
	}
	token, err := NewRunID()
	if err != nil {
		return false, err
	}
	created, err := registry.CreateSpawnIntent(ctx, opts.VaultPath, token)
	if err != nil {
		return false, fmt.Errorf("register pending runtime: %w", err)
	}
	if !created {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		_ = registry.ClearIntent(context.Background(), opts.VaultPath, token)
		return false, err
	}
	pid, err := spawnHeadlessWithToken(executable, opts.VaultPath, token)
	if err != nil {
		_ = registry.ClearIntent(context.Background(), opts.VaultPath, token)
		return false, fmt.Errorf("start vault runtime: %w", err)
	}
	logf("started vault runtime pid %d", pid)

	if !opts.Wait {
		// The caller does not block on readiness, but the lease still must
		// outlive the child's boot. This process may exit first; the lease then
		// carries a dead PID and the next TryAcquire reclaims it.
		held = false
		go func() {
			defer func() { _ = release() }()
			_ = waitProbeReady(context.WithoutCancel(ctx), opts.VaultPath, deadline)
		}()
		return true, nil
	}
	if err := waitProbeReady(ctx, opts.VaultPath, deadline); err != nil {
		return false, err
	}
	return true, nil
}

// waitProbeReady polls until the vault has a probe-verified manifest or the
// budget runs out. The child was released after Start, so its exit is only
// observable as a manifest that never appears.
func waitProbeReady(ctx context.Context, vaultPath string, deadline time.Time) error {
	gateCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	registry, err := NewRegistry()
	if err != nil {
		return err
	}
	for time.Now().Before(deadline) {
		if _, _, err := LiveManifest(ctx, vaultPath); err == nil {
			return nil
		}
		intent, found, err := registry.StartIntentFor(gateCtx, vaultPath)
		if err != nil {
			return err
		}
		// Another start may own the intent: an attached serve that won
		// election replaces the spawn token with its own, and its manifest
		// will satisfy this wait. Only a stop's cancellation or a vanished
		// intent ends the wait early.
		if !found || intent.Cancelled {
			return ErrStartCancelled
		}
		if !sleepCtx(ctx, ensurePoll) {
			return ctx.Err()
		}
	}
	return fmt.Errorf("%w: no runtime became ready within spawn budget", ErrNoRuntime)
}

func replaceHeadless(ctx context.Context, client *Client, health Health, opts EnsureOptions, logf func(string, ...any)) error {
	if previous, ok := replacedBuilds.Load(opts.VaultPath); ok && previous == health.BuildID {
		logf("warning: build-mismatch ping-pong at %s: runtime build %s has already been replaced once by this process; two different rzm binaries are competing for this vault", opts.VaultPath, health.BuildID)
	}
	replacedBuilds.Store(opts.VaultPath, health.BuildID)
	logf("replacing headless runtime pid %d (build %s, want %s)…", health.PID, health.BuildID, opts.BuildID)
	if err := RequestShutdown(ctx, client, StopGrace); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if killErr := TerminateHeadless(opts.VaultPath, client.Manifest); killErr != nil {
			return fmt.Errorf("stop headless runtime pid %d: %w", health.PID, err)
		}
		if err := WaitForExit(ctx, client.Manifest, StopGrace); err != nil {
			return err
		}
	}
	return nil
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func runtimeDiagnosticReason(err error) string {
	switch {
	case errors.Is(err, ErrAutostartDisabled):
		return "autostart_disabled"
	case errors.Is(err, ErrAttachedMismatch):
		return "attached_build_mismatch"
	case errors.Is(err, ErrRuntimeUnresponsive):
		return "runtime_unresponsive"
	case errors.Is(err, ErrNoRuntime):
		return "runtime_absent"
	case errors.Is(err, ErrStartCancelled):
		return "startup_canceled"
	}
	return logging.ClassifyError(err)
}
