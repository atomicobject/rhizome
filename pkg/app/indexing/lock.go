package indexing

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
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

// TryAcquireIndexLock waits for an owned writer lease for this vault.
func TryAcquireIndexLock(ctx context.Context, vaultRoot string, opts LockWaitOptions) (*indexlock.Lease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lockPath := repoconfig.IndexLockPath(vaultRoot)
	if opts.Role == "" {
		opts.Role = LockRoleCLIIndex
	}
	out := opts.Out
	if out == nil {
		out = os.Stderr
	}
	poll := opts.Poll
	if poll <= 0 {
		poll = time.Second
	}
	acquire := indexlock.AcquireOptions{Role: opts.Role}

	lease, acquired, err := indexlock.TryAcquireLease(vaultRoot, acquire)
	if err != nil {
		return nil, fmt.Errorf("index lock error: %w", err)
	}
	if acquired {
		heartbeatUntilRelease(ctx, lease, poll)
		return lease, nil
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

		lease, acquired, err = indexlock.TryAcquireLease(vaultRoot, acquire)
		if err != nil {
			finishStatusLine(out, &printed)
			return nil, fmt.Errorf("index lock error: %w", err)
		}
		if acquired {
			finishStatusLine(out, &printed)
			heartbeatUntilRelease(ctx, lease, poll)
			return lease, nil
		}
	}
}

func heartbeatUntilRelease(ctx context.Context, lease *indexlock.Lease, poll time.Duration) {
	interval := 30 * time.Second
	if poll > 0 && poll < interval {
		interval = poll * 10
	}
	lease.StartHeartbeat(ctx, interval)
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
