package tasks

import (
	"context"
	"time"
)

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
	now := s.opts.Now()
	if st.PausedAt(now) && st.PausedUntil.IsZero() {
		return time.Time{}, false, nil
	}
	if s.untrusted() != "" && st.PassError != nil && st.PassError.Code == PassUntrusted {
		return time.Time{}, false, nil
	}
	ref, _, defs, _, err := s.primary(ctx)
	if err != nil {
		return time.Time{}, false, err
	}
	tagged := s.lastTagged(ctx, ref, defs)
	zone, _ := s.personZone(m)
	var next time.Time
	found := false
	for _, def := range defs {
		d, blocked := taskDue(&st, def, tagged[def.ID], now, zone, s.opts.Config.DefaultMode)
		if blocked != "" {
			continue
		}
		at := d.due.At
		if d.due.Now && at.IsZero() {
			at = now
		}
		if !found || at.Before(next) {
			next, found = at, true
		}
	}
	if found && st.PausedAt(now) && next.Before(st.PausedUntil) {
		next = st.PausedUntil
	}
	return next, found, nil
}
