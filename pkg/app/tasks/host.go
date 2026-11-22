package tasks

import (
	"context"
	"fmt"
	"time"
)

// HostStatus is what Rhizome Desktop's menu bar shows for one repository
// (SPEC-0104 US7). The runtime sends it on the host stream; it adds RunHere
// and Draining, which live outside the task state.
type HostStatus struct {
	Repository  string       `json:"repository"`
	CommonDir   string       `json:"commonDir"`
	RunHere     bool         `json:"runHere"`
	Paused      bool         `json:"paused"`
	PausedUntil *time.Time   `json:"pausedUntil,omitempty"`
	Draining    bool         `json:"draining"`
	Running     *HostRun     `json:"running,omitempty"`
	Next        *HostNext    `json:"next,omitempty"`
	Attention   int          `json:"attention"`
	LastFailure *HostFailure `json:"lastFailure,omitempty"`
	Notices     []HostNotice `json:"notices"`
}

// HostRun is the run in progress.
type HostRun struct {
	Task      string    `json:"task"`
	Name      string    `json:"name"`
	RunID     string    `json:"runId"`
	StartedAt time.Time `json:"startedAt"`
	Stage     string    `json:"stage"`
}

// HostNext is the task that comes due first.
type HostNext struct {
	Task string    `json:"task"`
	Name string    `json:"name"`
	At   time.Time `json:"at"`
}

// HostFailure is the latest failure still standing: a task whose latest run
// failed, or a run-due pass that failed before running tasks (no task).
type HostFailure struct {
	Task    string    `json:"task,omitempty"`
	Name    string    `json:"name,omitempty"`
	At      time.Time `json:"at"`
	Message string    `json:"message"`
	Item    string    `json:"item,omitempty"`
}

// HostNotice is something Desktop may notify about. Its ID is stable for
// an item in one state, so Desktop notifies once per ID.
type HostNotice struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Task  string `json:"task,omitempty"`
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
}

// Notice kinds.
const (
	NoticeAttention = "attention"
	NoticeFailed    = "failed"
	NoticeReady     = "ready"
	NoticeDue       = "due"
)

// HostStatus reads the person's task state, the run lock, and the primary
// ref's definitions (cached per commit). It fetches nothing and writes
// nothing. When the primary ref does not resolve, it still reports, with
// task ids for names and no next task.
func (s *Service) HostStatus(ctx context.Context) (HostStatus, error) {
	st, err := s.store.Load()
	if err != nil {
		return HostStatus{}, err
	}
	now := s.opts.Now()
	hs := HostStatus{Repository: s.opts.Repo.MainCheckout, CommonDir: s.opts.Repo.CommonDir, Paused: st.PausedAt(now), Notices: []HostNotice{}}
	if hs.Paused {
		hs.PausedUntil = st.PausedUntil
	}
	names := map[string]string{}
	if defs, tagged, err := s.dueInputs(ctx); err == nil {
		for _, def := range defs {
			names[def.ID] = def.Name
		}
		if due, ok := s.earliestDue(&st, defs, tagged); ok {
			at := due.at
			if at.IsZero() {
				at = now
			}
			hs.Next = &HostNext{Task: due.def.ID, Name: due.def.Name, At: at}
		}
	}
	name := func(task string) string {
		if n := names[task]; n != "" {
			return n
		}
		return task
	}
	if r := st.Running; r != nil && s.RunLockHeld() {
		hs.Running = &HostRun{Task: r.Task, Name: name(r.Task), RunID: r.ID, StartedAt: r.StartedAt, Stage: r.Stage}
	}
	for _, it := range st.Items {
		if !it.Visible(now) || it.State == StateRunning {
			continue
		}
		hs.Attention++
		if n, ok := itemNotice(it, &st, name(it.Task)); ok {
			hs.Notices = append(hs.Notices, n)
		}
	}
	for task, r := range st.Last {
		if r.Outcome == OutcomeFailed && (hs.LastFailure == nil || r.At.After(hs.LastFailure.At)) {
			hs.LastFailure = &HostFailure{Task: task, Name: name(task), At: r.At, Message: r.Message, Item: r.Item}
		}
	}
	if pe := st.PassError; pe != nil {
		hs.Notices = append(hs.Notices, HostNotice{ID: fmt.Sprintf("pass-%d", pe.At.UnixNano()), Kind: NoticeFailed, Title: "Scheduled tasks could not run", Body: pe.Message})
		if hs.LastFailure == nil || pe.At.After(hs.LastFailure.At) {
			hs.LastFailure = &HostFailure{At: pe.At, Message: pe.Message}
		}
	}
	return hs, nil
}

func itemNotice(it Item, st *State, name string) (HostNotice, bool) {
	n := HostNotice{ID: "item-" + it.ID + "-" + string(it.State), Task: it.Task}
	switch {
	case it.Kind == KindAnnouncement:
		n.Kind, n.Title, n.Body = NoticeDue, "New task: "+name, it.Summary
	case it.State == StateDue:
		n.Kind, n.Title, n.Body = NoticeDue, name+" is due", it.Summary
	case it.State == StateReady:
		n.Kind, n.Title, n.Body = NoticeReady, name+" is ready for review", it.Message
	case it.State == StateAttention:
		n.Kind, n.Title, n.Body = NoticeAttention, name+" needs attention", it.Reason
		if last, ok := st.Last[it.Task]; ok && last.Item == it.ID && last.Outcome == OutcomeFailed {
			n.Kind, n.Title = NoticeFailed, name+" failed"
		}
	default:
		return n, false
	}
	return n, true
}
