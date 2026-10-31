package diagnostics

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	evidence "github.com/atomicobject/rhizome/pkg/diagnostics"
)

// Percentiles from fewer samples are noise, not evidence (SPEC-0116).
const minP95Samples = 20

const (
	runtimeBurstWindow  = 60 * time.Second
	runtimeBurstStarts  = 3 // more than this many starts in the window is a burst
	unfinishedQuietTime = 60 * time.Second
	maxFlagOperations   = 10
)

// PerfSummary aggregates terminal events. Every reported operation also emits
// a terminal event, so events alone cover reported and event-only work.
type PerfSummary struct {
	Groups []PerfGroup  `json:"groups"`
	Flags  []PerfFlag   `json:"flags"`
	Web    []WebSummary `json:"web,omitempty"`
}

type PerfGroup struct {
	Kind      string       `json:"kind"`
	Name      string       `json:"name"`
	Count     int          `json:"count"`
	Errors    int          `json:"errors"`
	Canceled  int          `json:"canceled"`
	TotalMS   int64        `json:"total_ms"`
	MedianMS  int64        `json:"median_ms"`
	P95MS     *int64       `json:"p95_ms,omitempty"`
	MaxMS     int64        `json:"max_ms"`
	QueueWait *WaitSummary `json:"queue_wait,omitempty"`
	LockWait  *WaitSummary `json:"lock_wait,omitempty"`
}

type WaitSummary struct {
	MedianMS int64 `json:"median_ms"`
	MaxMS    int64 `json:"max_ms"`
}

// PerfFlag is an observation worth investigating, never a diagnosis.
type PerfFlag struct {
	Kind         string    `json:"kind"`
	Observation  string    `json:"observation"`
	Count        int       `json:"count"`
	FirstAt      time.Time `json:"first_at"`
	LastAt       time.Time `json:"last_at"`
	OperationIDs []string  `json:"operation_ids,omitempty"`
}

type perfSamples struct {
	group             PerfGroup
	durations         []int64
	queueWaits, locks []int64
}

type perfStart struct {
	at                  time.Time
	process, name, kind string
}

// lifetimeStart reports a start whose operation lasts as long as a process or
// connection: a runtime, or an event stream. Its missing finish is normal.
func lifetimeStart(event evidence.Event) bool {
	if event.Subsystem == "runtime" && event.Name == "startup.started" {
		return true
	}
	stream, _ := event.Attributes["stream"].(bool)
	return stream
}

type perfWarning struct {
	flag PerfFlag
	seen map[string]bool
}

type perfAggregator struct {
	groups        map[[2]string]*perfSamples
	started       map[string]perfStart
	finished      map[string]bool
	lastSeen      map[string]time.Time
	runtimeStarts []timedOperation
	warnings      map[string]*perfWarning
	web           *webAggregator
}

type timedOperation struct {
	at time.Time
	id string
}

func newPerfAggregator() *perfAggregator {
	return &perfAggregator{groups: map[[2]string]*perfSamples{}, started: map[string]perfStart{},
		finished: map[string]bool{}, lastSeen: map[string]time.Time{}, warnings: map[string]*perfWarning{}, web: newWebAggregator()}
}

func (a *perfAggregator) visit(event evidence.Event) {
	if event.Time.After(a.lastSeen[event.ProcessID]) {
		a.lastSeen[event.ProcessID] = event.Time
	}
	var level slog.Level
	if level.UnmarshalText([]byte(event.Level)) == nil && level >= slog.LevelWarn {
		a.warn(event)
	}
	switch {
	case event.Name == webClientEvent:
		a.web.visit(event)
	case strings.HasSuffix(event.Name, ".started") && event.OperationID != "":
		if !lifetimeStart(event) {
			a.started[event.OperationID] = perfStart{at: event.Time, process: event.ProcessID, name: event.Name, kind: event.Subsystem}
		}
		// Starts count when they begin, including runtimes still running or crashed.
		if event.Subsystem == "runtime" && event.Name == "startup.started" {
			a.runtimeStarts = append(a.runtimeStarts, timedOperation{at: event.Time, id: event.OperationID})
		}
	case strings.HasSuffix(event.Name, ".finished"):
		if event.OperationID != "" {
			a.finished[event.OperationID] = true
		}
		a.terminal(event)
	}
}

func (a *perfAggregator) terminal(event evidence.Event) {
	duration, ok := attrInt(event.Attributes, "duration_ms")
	if !ok {
		return
	}
	kind, name := attrString(event.Attributes, "kind"), attrString(event.Attributes, "trigger")
	// Agent requests carry the transport as trigger and the operation as tool.
	if tool := attrString(event.Attributes, "tool"); tool != "" {
		name = tool + " (" + name + ")"
	}
	if jobKind := attrString(event.Attributes, "job_kind"); jobKind != "" {
		kind, name = "indexing-job", jobKind
	}
	if kind == "" {
		kind = event.Subsystem
	}
	if name == "" {
		name = event.Name
	}
	key := [2]string{kind, name}
	samples := a.groups[key]
	if samples == nil {
		samples = &perfSamples{group: PerfGroup{Kind: kind, Name: name}}
		a.groups[key] = samples
	}
	samples.group.Count++
	samples.group.TotalMS += duration
	samples.durations = append(samples.durations, duration)
	switch attrString(event.Attributes, "status") {
	case "error":
		samples.group.Errors++
	case "canceled", "preempted":
		samples.group.Canceled++
	}
	if wait, ok := attrInt(event.Attributes, "queue_wait_ms"); ok {
		samples.queueWaits = append(samples.queueWaits, wait)
	}
	if wait, ok := attrInt(event.Attributes, "lock_wait_ms"); ok {
		samples.locks = append(samples.locks, wait)
	}
}

func (a *perfAggregator) warn(event evidence.Event) {
	key := event.Level + " " + event.Subsystem + " " + event.Name
	warning := a.warnings[key]
	if warning == nil {
		kind := "error_events"
		if event.Level == "WARN" {
			kind = "warning_events"
		}
		warning = &perfWarning{flag: PerfFlag{Kind: kind, FirstAt: event.Time, LastAt: event.Time}, seen: map[string]bool{}}
		a.warnings[key] = warning
	}
	warning.flag.Observation = fmt.Sprintf("%s event %s/%s was recorded", event.Level, event.Subsystem, event.Name)
	if event.Name == "integrity.failed" {
		warning.flag.Kind = "index_integrity"
		warning.flag.Observation = "Index integrity check failed; inspect the index before trusting search results."
		if table := attrString(event.Attributes, "table"); table != "" {
			warning.flag.Observation = "Index integrity check failed for table " + table + "; inspect the index before trusting search results."
		}
	}
	warning.flag.Count++
	warning.flag.FirstAt = earliest(warning.flag.FirstAt, event.Time)
	warning.flag.LastAt = latest(warning.flag.LastAt, event.Time)
	if event.OperationID != "" && !warning.seen[event.OperationID] && len(warning.flag.OperationIDs) < maxFlagOperations {
		warning.seen[event.OperationID] = true
		warning.flag.OperationIDs = append(warning.flag.OperationIDs, event.OperationID)
	}
}

// summary takes the scan's per-process contiguous coverage, which bounds where
// a missing terminal record is evidence rather than an unread one.
func (a *perfAggregator) summary(contiguous map[string]time.Time) PerfSummary {
	out := PerfSummary{Groups: []PerfGroup{}, Flags: []PerfFlag{}}
	for _, samples := range a.groups {
		group := samples.group
		sorted := sortedCopy(samples.durations)
		group.MedianMS, group.MaxMS = percentile(sorted, 50), sorted[len(sorted)-1]
		if len(sorted) >= minP95Samples {
			p95 := percentile(sorted, 95)
			group.P95MS = &p95
		}
		group.QueueWait, group.LockWait = waits(samples.queueWaits), waits(samples.locks)
		out.Groups = append(out.Groups, group)
	}
	sort.Slice(out.Groups, func(i, j int) bool {
		if out.Groups[i].TotalMS != out.Groups[j].TotalMS {
			return out.Groups[i].TotalMS > out.Groups[j].TotalMS
		}
		return out.Groups[i].Kind+out.Groups[i].Name < out.Groups[j].Kind+out.Groups[j].Name
	})
	out.Flags = append(out.Flags, runtimeBursts(a.runtimeStarts)...)
	if flag, ok := a.unfinished(contiguous); ok {
		out.Flags = append(out.Flags, flag)
	}
	for _, warning := range a.warnings {
		out.Flags = append(out.Flags, warning.flag)
	}
	out.Web = a.web.summaries()
	out.Flags = append(out.Flags, a.web.flags()...)
	sort.SliceStable(out.Flags, func(i, j int) bool { return out.Flags[i].FirstAt.Before(out.Flags[j].FirstAt) })
	return out
}

// runtimeBursts marks every start inside a window that holds too many starts,
// then reports each run of marked starts as one burst.
func runtimeBursts(starts []timedOperation) []PerfFlag {
	sort.Slice(starts, func(i, j int) bool { return starts[i].at.Before(starts[j].at) })
	inBurst := make([]bool, len(starts))
	first := 0
	for last := range starts {
		for starts[last].at.Sub(starts[first].at) > runtimeBurstWindow {
			first++
		}
		if last-first+1 > runtimeBurstStarts {
			for k := first; k <= last; k++ {
				inBurst[k] = true
			}
		}
	}
	var flags []PerfFlag
	var current []timedOperation
	for k, start := range starts {
		if !inBurst[k] || len(current) > 0 && start.at.Sub(current[len(current)-1].at) > runtimeBurstWindow {
			if len(current) > 0 {
				flags = append(flags, burstFlag(current))
			}
			current = nil
		}
		if inBurst[k] {
			current = append(current, start)
		}
	}
	if len(current) > 0 {
		flags = append(flags, burstFlag(current))
	}
	return flags
}

func burstFlag(starts []timedOperation) PerfFlag {
	flag := PerfFlag{Kind: "runtime_start_burst", Count: len(starts), FirstAt: starts[0].at, LastAt: starts[len(starts)-1].at}
	flag.Observation = fmt.Sprintf("%d runtimes started for this vault within %s; check what spawned them and whether runtime election held.", len(starts), flag.LastAt.Sub(flag.FirstAt).Round(time.Second))
	for _, start := range starts {
		if len(flag.OperationIDs) < maxFlagOperations && start.id != "" {
			flag.OperationIDs = append(flag.OperationIDs, start.id)
		}
	}
	return flag
}

// unfinished flags started operations whose process kept recording for a while
// without a terminal record. A process that stopped recording may still be
// running or may have crashed; that evidence stays unknown, not flagged. So is
// a start before its process's contiguous coverage, whose terminal record may
// sit in a segment head or record the scan did not read.
func (a *perfAggregator) unfinished(contiguous map[string]time.Time) (PerfFlag, bool) {
	flag := PerfFlag{Kind: "unfinished_operations"}
	ids := make([]string, 0)
	for id, start := range a.started {
		if a.finished[id] || a.lastSeen[start.process].Sub(start.at) < unfinishedQuietTime || start.at.Before(contiguous[start.process]) {
			continue
		}
		if flag.Count == 0 || start.at.Before(flag.FirstAt) {
			flag.FirstAt = start.at
		}
		flag.LastAt = latest(flag.LastAt, start.at)
		flag.Count++
		ids = append(ids, id)
	}
	if flag.Count == 0 {
		return flag, false
	}
	sort.Strings(ids)
	flag.OperationIDs = ids[:min(len(ids), maxFlagOperations)]
	flag.Observation = fmt.Sprintf("%d operations started without a terminal record while their process kept recording; they may have hung, been abandoned, or lost their finish event.", flag.Count)
	return flag, true
}

func waits(values []int64) *WaitSummary {
	if len(values) == 0 {
		return nil
	}
	sorted := sortedCopy(values)
	return &WaitSummary{MedianMS: percentile(sorted, 50), MaxMS: sorted[len(sorted)-1]}
}

func sortedCopy(values []int64) []int64 {
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted
}

// percentile uses the nearest-rank method on sorted, nonempty input.
func percentile(sorted []int64, p int) int64 {
	rank := (p*len(sorted) + 99) / 100
	return sorted[max(rank, 1)-1]
}

func attrInt(attributes map[string]any, key string) (int64, bool) {
	switch value := attributes[key].(type) {
	case float64:
		return int64(value), true
	case int64:
		return value, true
	case int:
		return int64(value), true
	}
	return 0, false
}

func attrString(attributes map[string]any, key string) string {
	value, _ := attributes[key].(string)
	return value
}

func earliest(a, b time.Time) time.Time {
	if a.IsZero() || b.Before(a) {
		return b
	}
	return a
}

func latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
