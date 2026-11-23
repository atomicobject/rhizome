package taskdef

import (
	"strings"
	"testing"
	"time"
)

func TestParseSchedule(t *testing.T) {
	parse := func(body string) (Definition, error) {
		return ParseDefinition(".rhizome/tasks/t.yml", []byte("name: T\nprompt: p\n"+body))
	}
	one, err := parse("schedule:\n  at: \"05:00\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(one.Schedule) != 1 || one.Schedule[0].Late != time.Hour || one.Schedule[0].Days != [7]bool{true, true, true, true, true, true, true} || one.CadenceText() != "daily at 05:00" {
		t.Fatalf("one entry with defaults: %+v %q", one.Schedule, one.CadenceText())
	}
	list, err := parse("schedule:\n  - at: [\"13:00\", \"05:00\", \"05:00\"]\n    days: weekdays\n    timezone: America/Detroit\n    late: 30m\n  - every: 2h\n    between: [\"08:00\", \"12:00\"]\n    days: [sat, SUN]\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := list.Schedule[0]; got.Late != 30*time.Minute || got.Location == nil || got.Location.String() != "America/Detroit" || len(got.Times) != 2 || got.Times[0] != 300 || got.Days[time.Saturday] || !got.Days[time.Monday] {
		t.Fatalf("first entry: %+v", got)
	}
	if got := list.Schedule[1]; len(got.Times) != 3 || got.Times[2] != 12*60 || !got.Days[time.Sunday] || got.Days[time.Monday] {
		t.Fatalf("window entry: %+v", got)
	}
	if want := "weekdays at 05:00, 13:00 (America/Detroit); weekends every 2h 08:00–12:00"; list.CadenceText() != want {
		t.Fatalf("CadenceText %q", list.CadenceText())
	}

	errorCases := map[string]struct{ body, want string }{
		"neither cadence":      {"", "every: is required (or give a schedule)"},
		"both cadences":        {"every: 1d\nschedule:\n  at: \"05:00\"\n", "every or schedule, not both"},
		"cron string":          {"schedule: \"0 5 * * *\"\n", "cron and RRULE syntax are not supported"},
		"rrule field":          {"schedule:\n  rrule: FREQ=DAILY\n", "unknown field \"rrule\""},
		"unknown entry field":  {"schedule:\n  at: \"05:00\"\n  hour: 5\n", "(line 5): unknown field \"hour\""},
		"bad time":             {"schedule:\n  at: \"25:00\"\n", "\"25:00\" must be a 24-hour time"},
		"cron in at":           {"schedule:\n  at: \"0 5 * * *\"\n", "cron and RRULE"},
		"at and every":         {"schedule:\n  at: \"05:00\"\n  every: 1h\n  between: [\"08:00\", \"09:00\"]\n", "at or every, not both"},
		"every without window": {"schedule:\n  every: 1h\n", "every needs between"},
		"between with at":      {"schedule:\n  at: \"05:00\"\n  between: [\"08:00\", \"09:00\"]\n", "between goes with every"},
		"crossing midnight":    {"schedule:\n  every: 1h\n  between: [\"22:00\", \"02:00\"]\n", "cross midnight"},
		"partial minutes":      {"schedule:\n  every: 90s\n  between: [\"08:00\", \"09:00\"]\n", "whole number of minutes"},
		"bad day":              {"schedule:\n  at: \"05:00\"\n  days: [monday]\n", "\"monday\" is not daily"},
		"bad zone":             {"schedule:\n  at: \"05:00\"\n  timezone: Mars/Olympus\n", "not a known IANA zone"},
		"bad late":             {"schedule:\n  at: \"05:00\"\n  late: soon\n", "late: invalid duration"},
		"empty list":           {"schedule: []\n", "at least one entry"},
		"no times":             {"schedule:\n  days: daily\n", "needs at"},
	}
	for name, tc := range errorCases {
		_, err := parse(tc.body)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error containing %q", name, err, tc.want)
		}
	}
}
