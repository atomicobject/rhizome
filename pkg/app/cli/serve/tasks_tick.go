package serve

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Scheduled tasks (SPEC-0126 US3): while a runtime is up, it asks for due
// tasks on an interval. The work runs in a separate `rzm tasks run-due`
// process, so it never counts as client activity, survives the runtime's
// idle exit, and is deduplicated across runtimes by the repository's run
// lock.
const (
	defaultTasksInterval   = 15 * time.Minute
	defaultTasksFirstDelay = time.Minute
)

// runTasksTicker calls runTasks after a short delay and then on every
// interval, while the vault declares tasks.
func runTasksTicker(ctx context.Context, vaultPath string, first, interval time.Duration, runTasks func(vaultPath string) error) {
	timer := time.NewTimer(first)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		// Only a checkout that has task files asks; run-due reads the
		// declarations themselves from the primary ref.
		if _, err := os.Stat(filepath.Join(vaultPath, ".rhizome", "tasks")); err == nil {
			if err := runTasks(vaultPath); err != nil {
				recordServeDiagnostic(ctx, slog.LevelWarn, "tasks.start_failed", err)
			}
		}
		timer.Reset(interval)
	}
}
