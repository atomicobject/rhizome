package tasks

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

// DefaultStopWait is how long Stop lets a run exit on its own before it
// kills the run's processes.
const DefaultStopWait = 10 * time.Second

// Stop ends the run holding the repository's run lock (SPEC-0104 US7). It
// marks the run so whichever process records it records it as stopped, asks
// the holder's process group to exit, kills the holder's whole process tree
// after wait, and records the run if the holder died first. It reports
// false when no live process holds the lock, or when this process does (an
// in-process action that finishes on its own).
func (s *Service) Stop(ctx context.Context, wait time.Duration) (bool, error) {
	holder, ok := indexlock.ReadLockData(s.runLockPath())
	if !ok || !s.RunLockHeld() || holder.PID == os.Getpid() {
		return false, nil
	}
	if err := s.store.Update(ctx, func(st *State) error {
		if st.Running != nil {
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
