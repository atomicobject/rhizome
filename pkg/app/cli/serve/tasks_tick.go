package serve

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Scheduled tasks (SPEC-0127 US3): while a runtime is up, it asks for due
// tasks on an interval. The work runs in a separate `rzm tasks run-due`
// process, so it never counts as client activity, survives the runtime's
// idle exit, and is deduplicated across runtimes by the repository's run
// lock.
const (
	defaultTasksInterval   = 15 * time.Minute
	defaultTasksFirstDelay = time.Minute
)

// inGitCheckout reports whether dir or a parent holds a .git entry.
func inGitCheckout(dir string) bool {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// runTasksTicker calls runTasks after a short delay and then on every
// interval, for a vault that is a Git checkout.
func runTasksTicker(ctx context.Context, vaultPath string, first, interval time.Duration, runTasks func(vaultPath string) error) {
	timer := time.NewTimer(first)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		// Any vault in a Git checkout asks: run-due reads the declarations
		// from the primary ref, which an older branch's checkout may not show.
		if inGitCheckout(vaultPath) {
			if err := runTasks(vaultPath); err != nil {
				recordServeDiagnostic(ctx, slog.LevelWarn, "tasks.start_failed", err)
			}
		}
		timer.Reset(interval)
	}
}
