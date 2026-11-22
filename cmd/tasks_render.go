package cmd

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/tasks"
)

func renderTaskStatus(w io.Writer, r tasks.Report) {
	if r.Untrusted != "" {
		fmt.Fprintf(w, "Not running: %s\n", r.Untrusted)
	}
	if r.RefError != "" {
		fmt.Fprintf(w, "No scheduled tasks run: %s\n", r.RefError)
	} else {
		fetched := "never fetched"
		if !r.FetchedAt.IsZero() {
			fetched = "fetched " + ago(r.FetchedAt)
		}
		fmt.Fprintf(w, "Primary ref %s at %s (%s)\n", r.Ref, shortCommit(r.Commit), fetched)
	}
	if r.PassError != nil {
		fmt.Fprintf(w, "The last automatic pass failed %s: %s\n", ago(r.PassError.At), r.PassError.Message)
	}
	if r.Fetch != nil && r.Fetch.Error != "" {
		fmt.Fprintf(w, "The last fetch failed %s, so tasks use the last fetched primary ref: %s\n", ago(r.Fetch.At), r.Fetch.Error)
	}
	if r.MergeWarning != "" {
		fmt.Fprintf(w, "Warning: %s.\n", r.MergeWarning)
	}
	switch {
	case r.Paused && !r.PausedUntil.IsZero():
		fmt.Fprintf(w, "Paused: background work is off until %s, or `rzm tasks resume`.\n", r.PausedUntil.Local().Format("Mon Jan 2 15:04"))
	case r.Paused:
		fmt.Fprintln(w, "Paused: background work is off until `rzm tasks resume`.")
	}
	if !r.RunHere && r.RefError == "" {
		fmt.Fprintln(w, "Scheduled tasks do not run on this machine; `rzm tasks machine on` turns them on.")
	}
	if len(r.Tasks) == 0 {
		fmt.Fprintf(w, "No tasks are declared in %s or a module's tasks folder.\n", tasks.TasksDir)
	}
	for _, t := range r.Tasks {
		fmt.Fprintf(w, "\n%s (%s)\n", t.Name, t.ID)
		if t.Local != nil {
			fmt.Fprintf(w, "  local %s (%s)\n", t.Local.State, t.Local.Path)
			if t.Local.Error != "" {
				fmt.Fprintf(w, "  local error: %s\n", t.Local.Error)
			}
		}
		if !t.Scheduled {
			fmt.Fprintln(w, "  not scheduled (local test only)")
			continue
		}
		cadence := "every " + t.Every
		if t.Schedule != "" {
			cadence = t.Schedule
		}
		fmt.Fprintf(w, "  %s, %s scope, %s mode (%s)\n", cadence, t.Scope, t.Mode, t.ModeSource)
		last := "never"
		if t.LastRun != nil {
			last = fmt.Sprintf("%s by you (%s)", ago(t.LastRun.At), t.LastRun.Outcome)
		}
		if t.LastTagged != nil && (t.LastRun == nil || t.LastTagged.At.After(t.LastRun.At)) {
			last = fmt.Sprintf("%s by %s (%s)", ago(t.LastTagged.At), t.LastTagged.Author, shortCommit(t.LastTagged.Commit))
		}
		fmt.Fprintf(w, "  last run %s\n", last)
		switch {
		case t.Item != nil:
			fmt.Fprintf(w, "  waiting: item %s is %s\n", t.Item.ID, t.Item.State)
		case t.Due:
			fmt.Fprintln(w, "  due now")
		default:
			fmt.Fprintf(w, "  next due %s\n", slotText(t.NextAt, t.Timezone))
		}
		if reason := blockedText[t.BlockedBy]; reason != "" && !shownAbove[t.BlockedBy] {
			fmt.Fprintf(w, "  not running: %s\n", reason)
		}
		if t.Tokens > 0 {
			fmt.Fprintf(w, "  %d tokens in the last 30 days\n", t.Tokens)
		}
	}
	for _, p := range r.Problems {
		fmt.Fprintf(w, "\nProblem in %s: %s\n", p.Path, p.Message)
	}
}

// blockedText explains each status code; repository-wide codes are
// printed once above the tasks.
var blockedText = map[string]string{
	tasks.BlockedNotRunHere:  "tasks do not run on this machine",
	tasks.BlockedNoHost:      "no Rhizome Desktop host is running",
	tasks.BlockedUntrusted:   "the repository is not trusted",
	tasks.BlockedPaused:      "background work is paused",
	tasks.BlockedOff:         "the task is off",
	tasks.BlockedOpenItem:    "an open item is waiting for you",
	tasks.BlockedWrongBranch: "the checkout is not on the primary branch",
	tasks.BlockedRequires:    "a requirement is missing",
	tasks.BlockedFetchFailed: "the primary ref could not be fetched",
}

// shownAbove are codes the repository lines or the waiting line already
// explain.
var shownAbove = map[string]bool{tasks.BlockedNotRunHere: true, tasks.BlockedUntrusted: true, tasks.BlockedPaused: true, tasks.BlockedOpenItem: true}

// slotText formats a due time in the task's zone, naming the zone when it
// is not this machine's.
func slotText(at time.Time, zone string) string {
	if loc, err := time.LoadLocation(zone); zone != "" && err == nil {
		if local := at.Local(); local.Format("-0700 MST") != at.In(loc).Format("-0700 MST") {
			return at.In(loc).Format("Mon Jan 2 15:04") + " " + zone
		}
	}
	return at.Local().Format("Mon Jan 2 15:04")
}

func renderInbox(w io.Writer, items []tasks.Item) {
	if len(items) == 0 {
		fmt.Fprintln(w, "Nothing needs you.")
		return
	}
	for _, it := range items {
		state := string(it.State)
		if it.Kind == tasks.KindAnnouncement {
			state = "new task"
		}
		fmt.Fprintf(w, "[%s] %s  %s  (%s)\n", it.ID, it.Task, state, ago(it.Created))
		for _, line := range []string{it.Summary, it.Reason, firstMessageLine(it.Message)} {
			if line != "" {
				fmt.Fprintf(w, "    %s\n", line)
			}
		}
		if it.State == tasks.StateReady {
			fmt.Fprintf(w, "    %d files changed on %s in %s\n", len(it.Changed), it.Branch, it.Worktree)
		}
		fmt.Fprintf(w, "    %s\n", itemActions(it))
	}
}

func itemActions(it tasks.Item) string {
	switch {
	case it.Kind == tasks.KindAnnouncement:
		return "rzm tasks done " + it.ID
	case it.State == tasks.StateDue:
		return fmt.Sprintf("rzm tasks run|start|snooze|skip %s", it.ID)
	case it.State == tasks.StateReady:
		return fmt.Sprintf("review the worktree, then: rzm tasks done|discard %s", it.ID)
	case it.State == tasks.StateAttention:
		return fmt.Sprintf("rzm tasks run|start|discard|done %s", it.ID)
	default:
		return "rzm tasks done " + it.ID
	}
}

func renderRunSummary(w io.Writer, s tasks.RunSummary) {
	if s.Skipped != "" {
		fmt.Fprintf(w, "Skipped: %s\n", s.Skipped)
	}
	for _, o := range s.Outcomes {
		renderOutcome(w, o)
	}
	if s.Skipped == "" && len(s.Outcomes) == 0 {
		fmt.Fprintln(w, "No task was due.")
	}
	for _, p := range s.Problems {
		fmt.Fprintf(w, "Problem in %s: %s\n", p.Path, p.Message)
	}
}

func renderOutcome(w io.Writer, o tasks.Outcome) {
	line := fmt.Sprintf("%s: %s", o.Task, o.Outcome)
	if o.Item != "" {
		line += " (item " + o.Item + ")"
	}
	if o.Reason != "" {
		line += ": " + o.Reason
	}
	fmt.Fprintln(w, line)
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}

func shortCommit(c string) string {
	if len(c) > 8 {
		return c[:8]
	}
	return c
}

func firstMessageLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
