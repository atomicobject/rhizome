package tasks

import (
	"cmp"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/taskdef"
)

// Clock is what a task's due time is computed from.
type Clock struct {
	// LastLocal is when the person's latest run finished; LastStart is when
	// it started (zero falls back to LastLocal).
	LastLocal  time.Time
	LastStart  time.Time
	LastTagged *Tagged
	// Zone is the person's schedule zone for entries without their own;
	// nil means the host's.
	Zone *time.Location
}

// clockFor builds a task's clock from its latest local run.
func clockFor(last RunRecord, tagged *Tagged, zone *time.Location) Clock {
	return Clock{LastLocal: last.At, LastStart: cmp.Or(last.StartedAt, last.At), LastTagged: tagged, Zone: zone}
}

// Last is the latest run that counts for the task's scope: the person's own
// run, or for a team task also the latest tagged commit on the primary ref.
func (c Clock) Last(def Definition) time.Time {
	return c.latest(def, c.LastLocal)
}

func (c Clock) latest(def Definition, local time.Time) time.Time {
	if def.Scope == ScopeTeam && c.LastTagged != nil && c.LastTagged.At.After(local) {
		return c.LastTagged.At
	}
	return local
}

// Due is a task's due state at one moment.
type Due struct {
	// Now reports whether the task is due.
	Now bool
	// At is when it came due (a zero At is an `every` task that never ran),
	// or when it next comes due.
	At time.Time
	// Deadline orders due tasks, nearest first: a slot's end of `late`, or
	// an `every` task's due time.
	Deadline time.Time
	// Zone names the zone of the slot At belongs to; empty for `every`.
	Zone string
}

// Next reports whether the task is due at now, and when it came or comes
// due. An `every` task is due when its latest counting run is older than
// its cadence. A scheduled task is due when a slot has passed, no counting
// run started at or after it, and now is within the slot's `late`; missed
// slots collapse into that one run (SPEC-0127 US9).
func (c Clock) Next(def Definition, now time.Time) Due {
	if len(def.Schedule) == 0 {
		last := c.Last(def)
		if last.IsZero() {
			return Due{Now: true}
		}
		at := last.Add(def.Cadence)
		return Due{Now: !now.Before(at), At: at, Deadline: at}
	}
	last := c.latest(def, cmp.Or(c.LastStart, c.LastLocal))
	var due, next Due
	for _, e := range def.Schedule {
		loc := cmp.Or(e.Location, c.Zone, time.Local)
		if slot, ok := latestSlot(e, loc, now); ok && slot.After(last) && now.Sub(slot) <= e.Late {
			if d := slot.Add(e.Late); !due.Now || d.Before(due.Deadline) {
				due = Due{Now: true, At: slot, Deadline: d, Zone: zoneName(loc)}
			}
		}
		if slot := nextSlot(e, loc, now); next.At.IsZero() || slot.Before(next.At) {
			next = Due{At: slot, Deadline: slot.Add(e.Late), Zone: zoneName(loc)}
		}
	}
	if due.Now {
		return due
	}
	return next
}

// Due reports whether the task is due at now.
func (c Clock) Due(def Definition, now time.Time) bool { return c.Next(def, now).Now }

// slotSearchDays covers a weekly `days` list from any day.
const slotSearchDays = 8

// latestSlot is the entry's latest slot at or before now.
func latestSlot(e taskdef.ScheduleEntry, loc *time.Location, now time.Time) (time.Time, bool) {
	y, m, d := now.In(loc).Date()
	for back := 0; back <= slotSearchDays; back++ {
		if !e.Days[time.Date(y, m, d-back, 12, 0, 0, 0, loc).Weekday()] {
			continue
		}
		for i := len(e.Times) - 1; i >= 0; i-- {
			if slot := slotTime(y, m, d-back, e.Times[i], loc); !slot.After(now) {
				return slot, true
			}
		}
	}
	return time.Time{}, false
}

// nextSlot is the entry's first slot after now.
func nextSlot(e taskdef.ScheduleEntry, loc *time.Location, now time.Time) time.Time {
	y, m, d := now.In(loc).Date()
	for ahead := 0; ahead <= slotSearchDays; ahead++ {
		if !e.Days[time.Date(y, m, d+ahead, 12, 0, 0, 0, loc).Weekday()] {
			continue
		}
		for _, minutes := range e.Times {
			if slot := slotTime(y, m, d+ahead, minutes, loc); slot.After(now) {
				return slot
			}
		}
	}
	return time.Time{} // unreachable: parsing requires a day and a time
}

// slotTime is a time of day on a date in loc. A time inside a
// daylight-saving gap becomes the first instant after the gap. A time in a
// repeated hour is one slot at its first occurrence, so a run that started
// at either occurrence satisfies it; time.Date alone picks either one,
// depending on the zone.
func slotTime(y int, m time.Month, d, minutes int, loc *time.Location) time.Time {
	t := time.Date(y, m, d, minutes/60, minutes%60, 0, 0, loc)
	want := time.Date(y, m, d, minutes/60, minutes%60, 0, 0, time.UTC)
	if wallClock(t).Equal(want) {
		// Clocks went back at the start of t's zone period: the same wall
		// time may also exist that much earlier, before the change.
		if start, _ := t.ZoneBounds(); !start.IsZero() {
			_, before := start.Add(-time.Second).Zone()
			_, after := t.Zone()
			if earlier := t.Add(-time.Duration(before-after) * time.Second); before > after && earlier.Before(start) && wallClock(earlier).Equal(want) {
				return earlier
			}
		}
		return t
	}
	// time.Date picked an offset that moved the wall clock: the time does
	// not exist that day. The gap ends where the zone t fell in ends (t
	// before the gap) or starts (t after it).
	start, end := t.ZoneBounds()
	if wallClock(t).Before(want) && !end.IsZero() {
		return end
	}
	return start
}

// wallClock is t's date and time of day in its own zone, as a UTC time.
func wallClock(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC)
}

// zoneName is a location's IANA name, reading the host's from TZ or
// /etc/localtime because time.Local is named "Local".
func zoneName(loc *time.Location) string {
	if loc != time.Local {
		return loc.String()
	}
	return hostZoneName()
}

var hostZoneName = sync.OnceValue(func() string {
	if tz, ok := os.LookupEnv("TZ"); ok {
		if tz = strings.TrimPrefix(tz, ":"); tz != "" {
			return tz
		}
		return "UTC"
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if _, name, ok := strings.Cut(target, "zoneinfo/"); ok {
			return name
		}
	}
	return "Local"
})
