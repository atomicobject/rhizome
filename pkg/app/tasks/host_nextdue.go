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

// NextDueAt is the earliest time any enabled task is or comes due, for the
// runtime host's 30-second check (SPEC-0104 US7): at or before now means
// `rzm tasks run-due --host` has work. It reads the local primary ref and
// task state only, with no network and no Git writes. ok is false when
// nothing will come due here: tasks do not run on this machine, work is
// paused until resumed, every task is off or blocked by an open item, or an
// untrusted repository was already reported.
func (s *Service) NextDueAt(ctx context.Context) (time.Time, bool, error) {
	m, err := s.MachineSettings()
	if err != nil || !m.Repository.RunHere {
		return time.Time{}, false, err
	}
	st, err := s.store.Load()
	if err != nil {
		return time.Time{}, false, err
	}
	if s.untrusted() != "" && st.PassError != nil && st.PassError.Code == PassUntrusted {
		return time.Time{}, false, nil
	}
	_, at, ok, err := s.nextDue(ctx, &st, m)
	return at, ok, err
}

// nextDue is the task that comes due first and when, skipping what RunDue
// skips, so a due time always leads to a run. A timed pause moves it to when
// the pause lifts.
func (s *Service) nextDue(ctx context.Context, st *State, m MachineSettings) (Definition, time.Time, bool, error) {
	now := s.opts.Now()
	if st.PausedAt(now) && st.PausedUntil.IsZero() {
		return Definition{}, time.Time{}, false, nil
	}
	defs, tagged, err := s.dueInputs(ctx)
	if err != nil {
		return Definition{}, time.Time{}, false, err
	}
	zone, _ := s.personZone(m)
	var (
		best  Definition
		next  time.Time
		found bool
	)
	for _, def := range defs {
		d, blocked := taskDue(st, def, tagged[def.ID], now, zone, s.opts.Config.DefaultMode)
		if blocked != "" {
			continue
		}
		at := d.due.At
		if d.due.Now && at.IsZero() {
			at = now
		}
		if !found || at.Before(next) {
			best, next, found = def, at, true
		}
	}
	if found && st.PausedAt(now) && next.Before(st.PausedUntil) {
		next = st.PausedUntil
	}
	return best, next, found, nil
}

// RunLockHeld reports whether a live process holds the repository's run
// lock, so a check, run, or test is in progress.
func (s *Service) RunLockHeld() bool { return runLockHeld(s.runLockPath()) }
