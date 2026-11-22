package serve

import (
	"context"
	"log/slog"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/web"
)

// Scheduled tasks (SPEC-0104 US7, SPEC-0127 US3): while a Rhizome Desktop
// host stream holds the runtime and it has not been drained, the runtime
// checks the wall clock every 30 seconds and starts `rzm tasks run-due
// --host` when a task is due. Go timers pause while macOS sleeps, so short
// ticks compared against the clock catch up within a tick of waking. The
// work runs in a separate process, so it never counts as client activity,
// survives the runtime's exit, and is deduplicated across runtimes by the
// repository's run lock.
const (
	tasksCheckInterval = 30 * time.Second
	// tasksRetryAfter spaces out dispatches for a due time an earlier
	// run-due did not move, as when that pass found nothing it could run.
	// ponytail: a fixed wait; tie it to run-due's outcome if it ever matters.
	tasksRetryAfter = 5 * time.Minute
)

// taskDispatch decides, on each check, whether to start run-due.
type taskDispatch struct {
	hosts *web.TaskHosts
	open  func(ctx context.Context) (web.TasksAPI, error)
	start func() error
	now   func() time.Time
	// last is when run-due was last started; lastErr is the last error
	// recorded, so a lasting one is recorded once.
	last    time.Time
	lastErr string
}

// check starts run-due when a host holds the runtime, it is not drained, no
// run holds the repository's run lock, and a task is due that the last
// dispatch did not already start.
func (d *taskDispatch) check(ctx context.Context) {
	if !d.hosts.Dispatching() {
		return
	}
	due, ok, err := d.nextDue(ctx)
	if err != nil {
		if err.Error() != d.lastErr {
			d.lastErr = err.Error()
			recordServeDiagnostic(ctx, slog.LevelWarn, "tasks.check_failed", err)
		}
		return
	}
	d.lastErr = ""
	now := d.now()
	if !ok || due.After(now) {
		return
	}
	if !d.last.IsZero() && !due.After(d.last) && now.Sub(d.last) < tasksRetryAfter {
		return
	}
	if err := d.start(); err != nil {
		recordServeDiagnostic(ctx, slog.LevelWarn, "tasks.start_failed", err)
		return
	}
	d.last = now
}

// nextDue reports the earliest due time, and nothing due while a run holds
// the run lock: that run's pass handles what is due.
func (d *taskDispatch) nextDue(ctx context.Context) (time.Time, bool, error) {
	api, err := d.open(ctx)
	if err != nil {
		return time.Time{}, false, err
	}
	if api.RunLockHeld() {
		return time.Time{}, false, nil
	}
	return api.NextDueAt(ctx)
}

// runTaskChecks runs d.check every interval until ctx ends.
func runTaskChecks(ctx context.Context, interval time.Duration, d *taskDispatch) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.check(ctx)
		}
	}
}
