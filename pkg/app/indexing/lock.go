package indexing

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

// LockWaitOptions configures the interactive wait for the index lock.
type LockWaitOptions struct {
	// Role is recorded in the lock so another waiter can name this holder.
	Role string
	// RequestPriority asks a yielding holder to stop, which it does within a
	// second (SPEC-0104 US6).
	RequestPriority bool
	Debug           bool
	// Out receives the refreshed status line; defaults to stderr.
	Out io.Writer
	// Poll is how often the wait re-checks the lock and refreshes the status
	// line. Defaults to one second.
	Poll time.Duration
	// Limit bounds the wait. Zero means wait as long as the holder needs, which
	// is the interactive default; tests set it.
	Limit time.Duration
}

// LockRoleCLIIndex and friends name what a holder is doing (SPEC-0104 US6).
const (
	LockRoleCLIIndex = "cli/index"
)

// TryAcquireIndexLock acquires the vault's index lock, waiting for the current
// holder for as long as it needs.
func TryAcquireIndexLock(ctx context.Context, lockPath string, requestPriority bool, debug bool, out io.Writer) (func() error, error) {
	// Docs:
	// - [[indexing-workflow]]
	// - [[indexing-workflow#^spec-0036-us3-ac1]]
	// - [[indexing-workflow#^spec-0036-us3-ac2]]
	// - [Indexing pipeline - Concurrency + batching requirements](docs/reference/analysis/Indexing pipeline - Concurrency + batching requirements.md)
	return TryAcquireIndexLockWithOptions(ctx, lockPath, LockWaitOptions{
		Role:            LockRoleCLIIndex,
		RequestPriority: requestPriority,
		Debug:           debug,
		Out:             out,
	})
}

// TryAcquireIndexLockWithOptions is TryAcquireIndexLock with an explicit role,
// poll interval, and optional wait limit.
//
// There is no give-up timeout: a holder that is making progress is not an
// error, and the wait reports who holds the lock and for how long instead of
// failing with a PID. Cancelling ctx (Ctrl-C) returns ctx.Err() cleanly.
func TryAcquireIndexLockWithOptions(ctx context.Context, lockPath string, opts LockWaitOptions) (func() error, error) {
	out := opts.Out
	if out == nil {
		out = os.Stderr
	}
	poll := opts.Poll
	if poll <= 0 {
		poll = time.Second
	}
	acquire := indexlock.AcquireOptions{Role: opts.Role}

	release, acquired, err := indexlock.TryAcquireWithOptions(lockPath, acquire)
	if err != nil {
		return nil, fmt.Errorf("index lock error: %w", err)
	}
	if acquired {
		return heartbeatUntilRelease(ctx, lockPath, release, poll), nil
	}

	var priority *indexlock.PriorityRequest
	defer func() { priority.Close() }()

	waitStart := time.Now()
	printed := false
	for {
		if err := ctx.Err(); err != nil {
			finishStatusLine(out, &printed)
			return nil, err
		}
		if opts.RequestPriority && !priority.Active() {
			priority.Close()
			var priorityErr error
			priority, priorityErr = indexlock.RequestPriority(lockPath)
			if priorityErr != nil && opts.Debug {
				fmt.Fprintf(out, "Warning: failed to request priority: %v\n", priorityErr)
			}
		}
		writeHolderStatus(out, lockPath, &printed)

		if opts.Limit > 0 && time.Since(waitStart) >= opts.Limit {
			finishStatusLine(out, &printed)
			return nil, fmt.Errorf("index lock is held by %s; waited %s", holderSummary(lockPath), opts.Limit.Round(time.Second))
		}

		timer := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			finishStatusLine(out, &printed)
			return nil, ctx.Err()
		case <-timer.C:
		}

		release, acquired, err = indexlock.TryAcquireWithOptions(lockPath, acquire)
		if err != nil {
			finishStatusLine(out, &printed)
			return nil, fmt.Errorf("index lock error: %w", err)
		}
		if acquired {
			finishStatusLine(out, &printed)
			return heartbeatUntilRelease(ctx, lockPath, release, poll), nil
		}
	}
}

func heartbeatUntilRelease(ctx context.Context, lockPath string, release func() error, poll time.Duration) func() error {
	interval := 30 * time.Second
	if poll > 0 && poll < interval {
		interval = poll * 10
	}
	stopHeartbeat := indexlock.StartHeartbeat(ctx, lockPath, interval)
	return func() error {
		stopHeartbeat()
		return release()
	}
}

// writeHolderStatus refreshes one status line naming the holder, its age, and
// its last heartbeat, so a waiting user can see progress and decide.
func writeHolderStatus(out io.Writer, lockPath string, printed *bool) {
	fmt.Fprintf(out, "\rWaiting for the index lock: %s\033[K", holderSummary(lockPath))
	*printed = true
}

func finishStatusLine(out io.Writer, printed *bool) {
	if *printed {
		fmt.Fprintln(out)
		*printed = false
	}
}

func holderSummary(lockPath string) string {
	data, ok := indexlock.ReadLockData(lockPath)
	if !ok {
		return "another process (no readable lock data)"
	}
	role := data.Role
	if role == "" {
		role = "unknown"
	}
	summary := fmt.Sprintf("%s pid %d", role, data.PID)
	if started, err := time.Parse(time.RFC3339Nano, data.Started); err == nil {
		summary += fmt.Sprintf(", held %s", time.Since(started).Round(time.Second))
	}
	if heartbeat, ok := indexlock.LastHeartbeat(lockPath); ok {
		summary += fmt.Sprintf(", last heartbeat %s ago", time.Since(heartbeat).Round(time.Second))
	}
	return summary
}
