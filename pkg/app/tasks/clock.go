package tasks

import "time"

// Clock is what a task's due time is computed from.
type Clock struct {
	LastLocal  time.Time
	LastTagged *Tagged
}

// Last is the latest run that counts for the task's scope: the person's own
// run, or for a team task also the latest tagged commit on the primary ref.
func (c Clock) Last(def Definition) time.Time {
	last := c.LastLocal
	if def.Scope == ScopeTeam && c.LastTagged != nil && c.LastTagged.At.After(last) {
		last = c.LastTagged.At
	}
	return last
}

// NextDue is when the task next comes due; zero means it has never run and
// is due now.
func (c Clock) NextDue(def Definition) time.Time {
	last := c.Last(def)
	if last.IsZero() {
		return time.Time{}
	}
	return last.Add(def.Cadence)
}

// Due reports whether the task is due at now. A task missed while Rhizome
// was closed is due once; missed runs are not replayed.
func (c Clock) Due(def Definition, now time.Time) bool {
	next := c.NextDue(def)
	return next.IsZero() || !now.Before(next)
}
