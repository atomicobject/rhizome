package taskdef

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const defaultLate = time.Hour

// Schedule is a task's wall-clock schedule (SPEC-0127 US9): one entry or a
// list of entries. Parsing validates it; an empty Schedule means the task
// uses `every`.
type Schedule []ScheduleEntry

// ScheduleEntry is one rule. Its slots are the listed times of day on the
// chosen days, in the entry's zone.
type ScheduleEntry struct {
	// Times are minutes after midnight, sorted and unique.
	Times []int
	// Days is indexed by time.Weekday.
	Days [7]bool
	// Timezone is the entry's IANA zone name, or "" for the person's or
	// host's zone; Location is it loaded.
	Timezone string
	Location *time.Location
	// Late is how long after a slot a missed run still starts.
	Late time.Duration
	// Text is the entry as status shows it, without its zone.
	Text string
}

var entryFields = []string{"at", "every", "between", "days", "timezone", "late"}

var dayNames = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// UnmarshalYAML parses and validates a schedule. Unknown fields are
// rejected, as the task file's decoder does for the fields it knows.
func (s *Schedule) UnmarshalYAML(node *yaml.Node) error {
	var nodes []*yaml.Node
	switch node.Kind {
	case yaml.MappingNode:
		nodes = []*yaml.Node{node}
	case yaml.SequenceNode:
		if len(node.Content) == 0 {
			return scheduleErr(node, "needs at least one entry")
		}
		nodes = node.Content
	default:
		return scheduleErr(node, "must be an entry (at: \"05:00\") or a list of entries; cron and RRULE syntax are not supported")
	}
	out := make(Schedule, 0, len(nodes))
	for _, n := range nodes {
		entry, err := parseEntry(n)
		if err != nil {
			return err
		}
		out = append(out, entry)
	}
	*s = out
	return nil
}

// ScheduleError is a schedule that failed to parse; it names the line.
type ScheduleError struct {
	Line    int
	Message string
}

func (e *ScheduleError) Error() string {
	return fmt.Sprintf("schedule (line %d): %s", e.Line, e.Message)
}

func scheduleErr(n *yaml.Node, format string, args ...any) error {
	return &ScheduleError{Line: n.Line, Message: fmt.Sprintf(format, args...)}
}

func parseEntry(n *yaml.Node) (ScheduleEntry, error) {
	if n.Kind != yaml.MappingNode {
		return ScheduleEntry{}, scheduleErr(n, "each entry must be a mapping with at, or every and between")
	}
	fields := map[string]*yaml.Node{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i].Value
		if !slices.Contains(entryFields, key) {
			return ScheduleEntry{}, scheduleErr(n.Content[i], "unknown field %q; an entry takes %s (cron and RRULE syntax are not supported)", key, strings.Join(entryFields, ", "))
		}
		if _, dup := fields[key]; dup {
			return ScheduleEntry{}, scheduleErr(n.Content[i], "%s is given twice", key)
		}
		fields[key] = n.Content[i+1]
	}
	var e ScheduleEntry
	var err error
	switch at, every := fields["at"], fields["every"]; {
	case at != nil && every != nil:
		return e, scheduleErr(n, "an entry has at or every, not both")
	case at != nil:
		if fields["between"] != nil {
			return e, scheduleErr(fields["between"], "between goes with every, not at")
		}
		if e.Times, err = parseAt(at); err != nil {
			return e, err
		}
	case every != nil:
		if e.Times, err = parseWindow(every, fields["between"]); err != nil {
			return e, err
		}
	default:
		return e, scheduleErr(n, "an entry needs at (\"05:00\" or a list) or every with between")
	}
	days, err := parseDays(fields["days"])
	if err != nil {
		return e, err
	}
	e.Days = days
	if tz := fields["timezone"]; tz != nil {
		if e.Location, err = loadZone(tz); err != nil {
			return e, err
		}
		e.Timezone = tz.Value
	}
	e.Late = defaultLate
	if late := fields["late"]; late != nil {
		if e.Late, err = ParseDuration(late.Value); err != nil {
			return e, scheduleErr(late, "late: %v", err)
		}
	}
	e.Text = entryText(e, fields)
	return e, nil
}

// parseAt reads "HH:MM" or a list of them.
func parseAt(n *yaml.Node) ([]int, error) {
	values, err := scalars(n, "at")
	if err != nil {
		return nil, err
	}
	var times []int
	for _, v := range values {
		m, err := parseTimeOfDay(v)
		if err != nil {
			return nil, scheduleErr(n, "at: %v", err)
		}
		times = append(times, m)
	}
	slices.Sort(times)
	return slices.Compact(times), nil
}

// parseWindow expands every + between into times of day, from the start
// through the end.
func parseWindow(every, between *yaml.Node) ([]int, error) {
	if between == nil {
		return nil, scheduleErr(every, "every needs between: [\"HH:MM\", \"HH:MM\"] in a schedule entry")
	}
	step, err := ParseDuration(every.Value)
	if err != nil {
		return nil, scheduleErr(every, "every: %v", err)
	}
	if step < time.Minute || step%time.Minute != 0 {
		return nil, scheduleErr(every, "every must be a whole number of minutes")
	}
	if between.Kind != yaml.SequenceNode || len(between.Content) != 2 {
		return nil, scheduleErr(between, "between must be a list of two times, like [\"08:00\", \"18:00\"]")
	}
	var bounds [2]int
	for i, b := range between.Content {
		if bounds[i], err = parseTimeOfDay(b.Value); err != nil {
			return nil, scheduleErr(b, "between: %v", err)
		}
	}
	if bounds[0] >= bounds[1] {
		return nil, scheduleErr(between, "between must start before it ends; windows that cross midnight are not supported")
	}
	var times []int
	for m := bounds[0]; m <= bounds[1]; m += int(step / time.Minute) {
		times = append(times, m)
	}
	return times, nil
}

// parseDays reads daily (the default), weekdays, weekends, one day name, or
// a list of day names.
func parseDays(n *yaml.Node) ([7]bool, error) {
	var days [7]bool
	if n == nil {
		return [7]bool{true, true, true, true, true, true, true}, nil
	}
	if n.Kind == yaml.ScalarNode {
		switch strings.ToLower(n.Value) {
		case "daily":
			return [7]bool{true, true, true, true, true, true, true}, nil
		case "weekdays":
			return [7]bool{false, true, true, true, true, true, false}, nil
		case "weekends":
			return [7]bool{true, false, false, false, false, false, true}, nil
		}
	}
	values, err := scalars(n, "days")
	if err != nil {
		return days, err
	}
	for _, v := range values {
		i := slices.Index(dayNames, strings.ToLower(v))
		if i < 0 {
			return days, scheduleErr(n, "days: %q is not daily, weekdays, weekends, or a day (mon, tue, wed, thu, fri, sat, sun)", v)
		}
		days[i] = true
	}
	return days, nil
}

func loadZone(n *yaml.Node) (*time.Location, error) {
	name := strings.TrimSpace(n.Value)
	if n.Kind != yaml.ScalarNode || name == "" || name == "Local" {
		return nil, scheduleErr(n, "timezone must be an IANA zone name, like America/Detroit")
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, scheduleErr(n, "timezone %q is not a known IANA zone name", name)
	}
	return loc, nil
}

// scalars reads one scalar or a list of scalars.
func scalars(n *yaml.Node, field string) ([]string, error) {
	switch n.Kind {
	case yaml.ScalarNode:
		return []string{n.Value}, nil
	case yaml.SequenceNode:
		if len(n.Content) == 0 {
			return nil, scheduleErr(n, "%s needs at least one value", field)
		}
		out := make([]string, 0, len(n.Content))
		for _, c := range n.Content {
			if c.Kind != yaml.ScalarNode {
				return nil, scheduleErr(c, "%s must be a value or a list of values", field)
			}
			out = append(out, c.Value)
		}
		return out, nil
	}
	return nil, scheduleErr(n, "%s must be a value or a list of values", field)
}

// parseTimeOfDay reads 24-hour "HH:MM" as minutes after midnight.
func parseTimeOfDay(s string) (int, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		hint := ""
		if strings.ContainsAny(s, "* =") {
			hint = "; cron and RRULE syntax are not supported"
		}
		return 0, fmt.Errorf("%q must be a 24-hour time like \"05:30\"%s", s, hint)
	}
	return t.Hour()*60 + t.Minute(), nil
}

// entryText describes an entry for status: "weekdays at 05:00, 13:00" or
// "daily every 2h 08:00–18:00".
func entryText(e ScheduleEntry, fields map[string]*yaml.Node) string {
	days := daysText(e.Days)
	if fields["at"] != nil {
		times := make([]string, len(e.Times))
		for i, m := range e.Times {
			times[i] = fmt.Sprintf("%02d:%02d", m/60, m%60)
		}
		return days + " at " + strings.Join(times, ", ")
	}
	between := fields["between"].Content
	return fmt.Sprintf("%s every %s %s–%s", days, fields["every"].Value, between[0].Value, between[1].Value)
}

func daysText(days [7]bool) string {
	switch days {
	case [7]bool{true, true, true, true, true, true, true}:
		return "daily"
	case [7]bool{false, true, true, true, true, true, false}:
		return "weekdays"
	case [7]bool{true, false, false, false, false, false, true}:
		return "weekends"
	}
	var names []string
	for i := range 7 {
		if d := (i + 1) % 7; days[d] { // Monday first
			names = append(names, dayNames[d])
		}
	}
	return strings.Join(names, ", ")
}

// String describes the schedule for status and announcements.
func (s Schedule) String() string {
	parts := make([]string, 0, len(s))
	for _, e := range s {
		text := e.Text
		if e.Timezone != "" {
			text += " (" + e.Timezone + ")"
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "; ")
}
