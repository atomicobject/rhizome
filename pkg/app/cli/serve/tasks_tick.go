package serve

import (
	"context"
	"log/slog"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
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
	// tasksRetryAfter spaces out dispatches after a run-due that exited
	// without recording a run, as when that pass found nothing it could run
	// or failed to start: another at once would repeat it every check.
	tasksRetryAfter = 5 * time.Minute
)

// taskDispatch decides, on each check, whether to start run-due.
type taskDispatch struct {
	hosts *web.TaskHosts
	open  func(ctx context.Context) (web.TasksAPI, error)
	start func() (*appruntime.Detached, error)
	now   func() time.Time
	// child is the last run-due this check started, at startedAt; exitedAt
	// is when a check first saw it gone.
	child     *appruntime.Detached
	startedAt time.Time
	exitedAt  time.Time
	// lastErr is the last error recorded, so a lasting one is recorded once.
	lastErr string
}

// check starts run-due when a host holds the runtime, it is not drained, no
// run holds the repository's run lock, a task is due, and the last run-due
// it started has exited having recorded a run, or long enough ago. The host
// registry rechecks the host and drain under its lock as it starts.
func (d *taskDispatch) check(ctx context.Context) {
	if !d.hosts.Hosting() {
		return
	}
	api, err := d.open(ctx)
	if err == nil && api.RunLockHeld() {
		// That run's pass handles what is due.
		return
	}
	var (
		due time.Time
		ok  bool
	)
	if err == nil {
		due, ok, err = api.NextDueAt(ctx)
	}
	if err != nil {
		if err.Error() != d.lastErr {
			d.lastErr = err.Error()
			recordServeDiagnostic(ctx, slog.LevelWarn, "tasks.check_failed", err)
		}
		return
	}
	d.lastErr = ""
	now := d.now()
	if !ok || due.After(now) || d.waiting(ctx, api, now) {
		return
	}
	child, err := d.hosts.Dispatch(d.start)
	if err != nil {
		recordServeDiagnostic(ctx, slog.LevelWarn, "tasks.start_failed", err)
		return
	}
	if child != nil {
		d.child, d.startedAt, d.exitedAt = child, now, time.Time{}
	}
}

// waiting reports whether the last run-due this check started is still
// running, or exited less than tasksRetryAfter ago without recording a run.
// It does not compare due times, which can move without any run.
func (d *taskDispatch) waiting(ctx context.Context, api web.TasksAPI, now time.Time) bool {
	if d.child == nil {
		return false
	}
	if d.child.Alive() {
		return true
	}
	if d.exitedAt.IsZero() {
		d.exitedAt = now
	}
	if now.Sub(d.exitedAt) >= tasksRetryAfter {
		return false
	}
	live, err := api.LiveState(ctx)
	if err != nil {
		return true
	}
	for _, r := range live.Recent {
		if !r.At.Before(d.startedAt) {
			return false
		}
	}
	return true
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
