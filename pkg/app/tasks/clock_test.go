package tasks

import (
	"testing"
	"time"
)

func scheduled(t *testing.T, schedule string) Definition {
	t.Helper()
	def, err := ParseDefinition(".rhizome/tasks/t.yml", []byte("name: T\nprompt: p\nschedule:\n"+schedule))
	if err != nil {
		t.Fatal(err)
	}
	return def
}

func TestScheduleSlots(t *testing.T) {
	detroit, err := time.LoadLocation("America/Detroit")
	if err != nil {
		t.Skip("needs the America/Detroit zone")
	}
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, detroit)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	const zone = "  timezone: America/Detroit\n"
	cases := []struct {
		name      string
		schedule  string
		lastStart string // "" = never ran
		now       string
		due       bool
		at        string // the slot it is due for, or the next slot
		instant   time.Time
	}{
		{name: "slot passed, never ran", schedule: "  at: \"05:00\"\n" + zone, now: "2026-11-23 05:00", due: true, at: "2026-11-23 05:00"},
		{name: "before the slot", schedule: "  at: \"05:00\"\n" + zone, now: "2026-11-23 04:59", at: "2026-11-23 05:00"},
		{name: "ran at the slot", schedule: "  at: \"05:00\"\n" + zone, lastStart: "2026-11-23 05:00", now: "2026-11-23 05:30", at: "2026-11-24 05:00"},
		{name: "run started before the slot does not count", schedule: "  at: \"05:00\"\n" + zone, lastStart: "2026-11-23 04:59", now: "2026-11-23 05:10", due: true, at: "2026-11-23 05:00"},
		{name: "past late is skipped", schedule: "  at: \"05:00\"\n" + zone, now: "2026-11-23 06:01", at: "2026-11-24 05:00"},
		{name: "custom late", schedule: "  at: \"05:00\"\n  late: 3h\n" + zone, now: "2026-11-23 07:59", due: true, at: "2026-11-23 05:00"},
		{name: "weekdays skip Saturday", schedule: "  at: \"05:00\"\n  days: weekdays\n" + zone, now: "2026-11-21 05:10", at: "2026-11-23 05:00"},
		{name: "weekends", schedule: "  at: \"05:00\"\n  days: weekends\n" + zone, now: "2026-11-21 05:10", due: true, at: "2026-11-21 05:00"},
		{name: "day list", schedule: "  at: \"05:00\"\n  days: [mon, thu]\n" + zone, now: "2026-11-24 12:00", at: "2026-11-26 05:00"},
		{name: "list of times, missed slots collapse to the latest", schedule: "  at: [\"05:00\", \"09:00\", \"13:00\"]\n  late: 12h\n" + zone, lastStart: "2026-11-22 13:00", now: "2026-11-23 13:30", due: true, at: "2026-11-23 13:00"},
		{name: "list of times, next", schedule: "  at: [\"13:00\", \"05:00\"]\n" + zone, lastStart: "2026-11-23 05:00", now: "2026-11-23 06:00", at: "2026-11-23 13:00"},
		{name: "between window", schedule: "  every: 2h\n  between: [\"08:00\", \"18:00\"]\n" + zone, lastStart: "2026-11-23 10:00", now: "2026-11-23 12:05", due: true, at: "2026-11-23 12:00"},
		{name: "between window ends", schedule: "  every: 2h\n  between: [\"08:00\", \"17:00\"]\n" + zone, lastStart: "2026-11-23 16:00", now: "2026-11-23 17:30", at: "2026-11-24 08:00"},
		// 2026-03-08: 02:00 EST jumps to 03:00 EDT.
		{name: "spring-forward gap runs after the gap", schedule: "  at: \"02:30\"\n" + zone, lastStart: "2026-03-07 02:30", now: "2026-03-08 03:01", due: true, instant: time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC)},
		{name: "spring-forward gap is next", schedule: "  at: \"02:30\"\n" + zone, lastStart: "2026-03-07 02:30", now: "2026-03-08 01:00", instant: time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC)},
		// 2026-11-01: 02:00 EDT falls back to 01:00 EST; 01:30 happens twice.
		{name: "repeated hour runs at its first occurrence", schedule: "  at: \"01:30\"\n" + zone, lastStart: "2026-10-31 01:30", now: "2026-11-01 01:31", due: true, instant: time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := scheduled(t, tc.schedule)
			var c Clock
			if tc.lastStart != "" {
				c.LastStart = at(tc.lastStart)
				c.LastLocal = c.LastStart.Add(time.Minute)
			}
			got := c.Next(def, at(tc.now))
			want := tc.instant
			if want.IsZero() {
				want = at(tc.at)
			}
			if got.Now != tc.due || !got.At.Equal(want) {
				t.Fatalf("Next = due %v at %v, want due %v at %v", got.Now, got.At.In(detroit), tc.due, want.In(detroit))
			}
			if got.Zone != "America/Detroit" {
				t.Fatalf("zone %q", got.Zone)
			}
		})
	}

	t.Run("repeated hour runs once", func(t *testing.T) {
		def := scheduled(t, "  at: \"01:30\"\n"+zone)
		first := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)  // 01:30 EDT
		second := time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC) // 01:30 EST
		c := Clock{LastStart: first, LastLocal: first}
		if got := c.Next(def, second.Add(time.Minute)); got.Now {
			t.Fatalf("due again in the repeated hour at %v", got.At)
		}
	})
}

// A wall-clock time in a repeated hour is one slot, identified by its date
// and time in the zone: a run that started at either occurrence satisfies
// it, and it comes due at the first.
func TestRepeatedHourIsOneSlot(t *testing.T) {
	cases := []struct {
		zone, at      string
		first, second time.Time
	}{
		// 03:00 CEST falls back to 02:00 CET.
		{"Europe/Berlin", "02:30", time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC), time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC)},
		// 02:00 EDT falls back to 01:00 EST.
		{"America/Detroit", "01:30", time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC), time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		t.Run(tc.zone, func(t *testing.T) {
			if _, err := time.LoadLocation(tc.zone); err != nil {
				t.Skip("needs zone data")
			}
			def := scheduled(t, "  at: \""+tc.at+"\"\n  late: 2h\n  timezone: "+tc.zone+"\n")
			previous := tc.first.AddDate(0, 0, -1)
			if got := (Clock{LastStart: previous}).Next(def, tc.first.Add(time.Minute)); !got.Now || !got.At.Equal(tc.first) {
				t.Fatalf("first occurrence: %+v", got)
			}
			for _, started := range []time.Time{tc.first.Add(5 * time.Minute), tc.second.Add(5 * time.Minute)} {
				c := Clock{LastStart: started}
				if got := c.Next(def, tc.second.Add(10*time.Minute)); got.Now {
					t.Fatalf("a run started at %v did not satisfy the slot: %+v", started, got)
				}
			}
		})
	}
}

func TestScheduleZonePrecedence(t *testing.T) {
	tokyo, err1 := time.LoadLocation("Asia/Tokyo")
	detroit, err2 := time.LoadLocation("America/Detroit")
	if err1 != nil || err2 != nil {
		t.Skip("needs zone data")
	}
	now := time.Date(2026, 11, 23, 20, 30, 0, 0, time.UTC) // 05:30 Tokyo, 15:30 Detroit
	entry := scheduled(t, "  at: \"05:00\"\n  timezone: Asia/Tokyo\n")
	bare := scheduled(t, "  at: \"05:00\"\n")

	if got := (Clock{Zone: detroit}).Next(entry, now); !got.Now || got.Zone != "Asia/Tokyo" {
		t.Fatalf("the entry's zone wins: %+v", got)
	}
	if got := (Clock{Zone: tokyo}).Next(bare, now); !got.Now || got.Zone != "Asia/Tokyo" {
		t.Fatalf("the person's zone applies: %+v", got)
	}
	if got := (Clock{Zone: detroit}).Next(bare, now); got.Now || !got.At.Equal(time.Date(2026, 11, 24, 5, 0, 0, 0, detroit)) {
		t.Fatalf("Detroit's 05:00 is next: %+v", got)
	}
	if got := (Clock{}).Next(bare, now); got.At.Location() != time.Local {
		t.Fatalf("no zone falls back to the host's: %+v", got)
	}
}

func TestEveryClockUnchanged(t *testing.T) {
	def, err := ParseDefinition(".rhizome/tasks/t.yml", []byte("name: T\nprompt: p\nevery: 1h\n"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	if got := (Clock{}).Next(def, now); !got.Now || !got.At.IsZero() {
		t.Fatalf("never ran: %+v", got)
	}
	c := Clock{LastLocal: now.Add(-30 * time.Minute), LastStart: now.Add(-3 * time.Hour)}
	if got := c.Next(def, now); got.Now || !got.At.Equal(now.Add(30*time.Minute)) {
		t.Fatalf("every counts from the finish: %+v", got)
	}
}
