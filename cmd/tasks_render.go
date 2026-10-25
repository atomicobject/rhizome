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
		fmt.Fprintf(w, "No tasks run: %s\n", r.RefError)
		return
	}
	fetched := "never fetched"
	if !r.FetchedAt.IsZero() {
		fetched = "fetched " + ago(r.FetchedAt)
	}
	fmt.Fprintf(w, "Primary ref %s at %s (%s)\n", r.Ref, shortCommit(r.Commit), fetched)
	if r.Paused {
		fmt.Fprintln(w, "Paused: background work is off until `rzm tasks resume`.")
	}
	if len(r.Tasks) == 0 {
		fmt.Fprintf(w, "No tasks are declared in %s.\n", tasks.TasksDir)
	}
	for _, t := range r.Tasks {
		fmt.Fprintf(w, "\n%s (%s)\n", t.Name, t.ID)
		fmt.Fprintf(w, "  every %s, %s scope, %s mode (%s)\n", t.Every, t.Scope, t.Mode, t.ModeSource)
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
			fmt.Fprintf(w, "  next due %s\n", t.NextDue.Local().Format("Mon Jan 2 15:04"))
		}
		if t.Tokens > 0 {
			fmt.Fprintf(w, "  %d tokens in the last 30 days\n", t.Tokens)
		}
	}
	for _, p := range r.Problems {
		fmt.Fprintf(w, "\nProblem in %s: %s\n", p.Path, p.Message)
	}
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
