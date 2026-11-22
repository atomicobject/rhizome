package tasks

import (
	"context"
	"sync"
	"time"
)

// dueInputsCache keeps the definitions and tags for one primary-ref commit,
// so the runtime's 30-second check and the host stream run two git
// commands, not one per task file and tag, while the ref stays put.
type dueInputsCache struct {
	mu     sync.Mutex
	commit string
	defs   []Definition
	tagged map[string]*Tagged
}

// dueInputs returns the task definitions and their latest tagged commits at
// the primary ref. It reads Git only: no network, no writes.
func (s *Service) dueInputs(ctx context.Context) ([]Definition, map[string]*Tagged, error) {
	ref, commit, err := s.opts.Repo.ResolvePrimary(ctx, s.opts.Config.PrimaryRef)
	if err != nil {
		return nil, nil, err
	}
	c := &s.due
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.commit != commit {
		defs, _, err := LoadDefinitions(ctx, s.opts.Repo, commit)
		if err != nil {
			return nil, nil, err
		}
		c.commit, c.defs, c.tagged = commit, defs, s.lastTagged(ctx, ref, defs)
	}
	return c.defs, c.tagged, nil
}

// NextDueAt is the earliest time an enabled task comes due (SPEC-0104 US7):
// not off, not blocked by an open item, and not before a timed pause lifts.
// A task that never ran is due from when it was first seen. ok is false
// when nothing will come due: no tasks, an untrusted repository, or an
// open-ended pause. It runs no network and writes nothing, so the runtime
// calls it every 30 seconds.
func (s *Service) NextDueAt(ctx context.Context) (time.Time, bool, error) {
	st, err := s.store.Load()
	if err != nil {
		return time.Time{}, false, err
	}
	defs, tagged, err := s.dueInputs(ctx)
	if err != nil {
		return time.Time{}, false, err
	}
	due, ok := s.earliestDue(&st, defs, tagged)
	return due.at, ok, nil
}

type dueTask struct {
	def Definition
	at  time.Time
}

// earliestDue skips what RunDue skips, so a due time always leads to a run.
func (s *Service) earliestDue(st *State, defs []Definition, tagged map[string]*Tagged) (dueTask, bool) {
	now := s.opts.Now()
	if s.untrusted() != "" || (st.PausedAt(now) && st.PausedUntil == nil) {
		return dueTask{}, false
	}
	var best dueTask
	found := false
	for _, def := range defs {
		if mode, _ := st.ResolveMode(def.ID, s.opts.Config.DefaultMode); mode == ModeOff {
			continue
		}
		if it := st.ActiveTaskItem(def.ID); it != nil && it.State != StateAttention && it.State != StateDue {
			continue
		}
		last, _ := st.LastRun(def.ID)
		at := (Clock{LastLocal: last.At, LastTagged: tagged[def.ID]}).NextDue(def)
		if at.IsZero() {
			at = st.Seen[def.ID]
		}
		if !found || at.Before(best.at) {
			best, found = dueTask{def: def, at: at}, true
		}
	}
	if found && st.PausedAt(now) && best.at.Before(*st.PausedUntil) {
		best.at = *st.PausedUntil
	}
	return best, found
}

// RunLockHeld reports whether a live process holds the repository's run
// lock, so a check, run, or test is in progress.
func (s *Service) RunLockHeld() bool { return runLockHeld(s.runLockPath()) }
