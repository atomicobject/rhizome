package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

// DefaultStopWait is how long Stop lets a run exit on its own before it
// kills the run's processes.
const DefaultStopWait = 10 * time.Second

// Stop ends the run holding the repository's run lock (SPEC-0104 US7). One
// read of the lock's holder decides everything: whether it is alive, which
// process group to signal, and which run to mark, so a holder that changes
// meanwhile is never signalled for another's run, and a lock copied with
// the checkout never signals the original's process. It marks the holder's
// run so whichever process records it records it as stopped, asks the
// holder's process group to exit, kills the holder's whole process tree
// after wait, and records the run if the holder died first. A Running left
// by an earlier process is not the holder's and is recorded as interrupted.
// It reports false when no live process holds the lock, or when this
// process does (an in-process action that finishes on its own).
func (s *Service) Stop(ctx context.Context, wait time.Duration) (bool, error) {
	holder, live := liveRunLockHolder(s.runLockPath())
	if !live || holder.PID <= 0 || holder.PID == os.Getpid() {
		return false, nil
	}
	key := holderKey(holder)
	if err := s.store.Update(ctx, func(st *State) error {
		if st.Running != nil && st.Running.Holder == key {
			st.Stopping = st.Running.ID
		}
		return nil
	}); err != nil {
		return false, err
	}
	if err := stopProcessTree(ctx, holder.PID, wait); err != nil {
		_ = s.store.Update(context.WithoutCancel(ctx), func(st *State) error {
			st.Stopping = ""
			return nil
		})
		return false, err
	}
	release, err := s.lockRun(ctx, time.Second)
	if errors.Is(err, errLocked) {
		// A new pass took the lock; it settles the stopped run itself.
		return true, nil
	}
	if err != nil {
		return true, err
	}
	defer func() { _ = release() }()
	return true, s.finalizeInterrupted(ctx)
}

// StopProcess asks pid's process group to exit and, after wait, kills it
// and everything it started, as Stop does for the run lock's holder. The
// runtime uses it for a run it started that has not taken the lock yet.
func StopProcess(ctx context.Context, pid int, wait time.Duration) error {
	return stopProcessTree(ctx, pid, wait)
}

// holderKey names one acquisition of the run lock: the holder's PID and
// when it took the lock, as indexlock records them.
func holderKey(d indexlock.LockData) string { return fmt.Sprintf("%d@%s", d.PID, d.Started) }

// lockHolder is this process's key for the run lock it holds, or "".
func (s *Service) lockHolder() string {
	if d, ok := indexlock.ReadLockData(s.runLockPath()); ok && d.PID == os.Getpid() {
		return holderKey(d)
	}
	return ""
}

// waitGone polls until pid has exited, for up to d.
func waitGone(ctx context.Context, pid int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for indexlock.PIDExists(pid) {
		if !time.Now().Before(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(lockPollTime):
		}
	}
	return true
}
