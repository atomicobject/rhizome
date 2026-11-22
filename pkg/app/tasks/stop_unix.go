//go:build !windows

package tasks

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"

	"github.com/atomicobject/rhizome/pkg/proctree"
)

// killWait bounds the wait for a killed run to disappear.
const killWait = 5 * time.Second

// stopProcessTree sends SIGTERM to the holder's process group (a detached
// run-due leads its own session and group; a run started from a terminal
// gets the signal alone), waits up to wait, then kills the holder and
// everything it started, including processes that moved to sessions of
// their own, as a check timeout does.
func stopProcessTree(ctx context.Context, pid int, wait time.Duration) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	// Record descendants while they are still linked to the holder, so one
	// that outlives it is still found.
	tree := proctree.Track(process, proctree.DefaultInterval)
	defer tree.Stop()
	target := pid
	if pgid, err := syscall.Getpgid(pid); err == nil && pgid == pid {
		target = -pid
	}
	if err := syscall.Kill(target, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	if !waitGone(ctx, pid, wait) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	_ = tree.Kill()
	waitGone(ctx, pid, killWait)
	return nil
}
