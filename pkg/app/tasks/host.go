package tasks

import (
	"context"
	"fmt"
	"time"
)

// HostStatus is what Rhizome Desktop's menu bar shows for one repository
// (SPEC-0104 US7). The runtime sends it on the host stream and adds
// Draining, which lives in the runtime.
type HostStatus struct {
	Repository  string       `json:"repository"`
	CommonDir   string       `json:"commonDir"`
	RunHere     bool         `json:"runHere"`
	Paused      bool         `json:"paused"`
	PausedUntil *time.Time   `json:"pausedUntil,omitempty"`
	Draining    bool         `json:"draining"`
	Running     *HostRun     `json:"running,omitempty"`
	// Dispatching is a run the runtime started that has recorded no run
	// yet; the runtime adds it, like Draining.
	Dispatching *HostDispatch `json:"dispatching,omitempty"`
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

// HostDispatch is a run-due or run process the runtime started that is
// still alive.
type HostDispatch struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"startedAt"`
}

// HostNext is the task that comes due first.
type HostNext struct {
	Task string    `json:"task"`
	Name string    `json:"name"`
	At   time.Time `json:"at"`
}

// HostFailure is the latest failure still standing: a task whose latest run
// failed, or a run-due pass that failed before running tasks (an empty
// task, named passName). Every key is always sent.
type HostFailure struct {
	Task    string    `json:"task"`
	Name    string    `json:"name"`
	At      time.Time `json:"at"`
	Message string    `json:"message"`
	Item    string    `json:"item"`
}

// HostNotice is something Desktop may notify about. Its ID is stable for
// an item in one state, so Desktop notifies once per ID. Every key is
// always sent.
type HostNotice struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Task  string `json:"task"`
	Name  string `json:"name"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// passName names a pass-level failure, which belongs to no task.
const passName = "Scheduled tasks"

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
	machine, _ := s.MachineSettings()
	hs := HostStatus{Repository: s.opts.Repo.MainCheckout, CommonDir: s.opts.Repo.CommonDir, RunHere: machine.Repository.RunHere, Paused: st.PausedAt(now), Notices: []HostNotice{}}
	if hs.Paused && !st.PausedUntil.IsZero() {
		until := st.PausedUntil
		hs.PausedUntil = &until
	}
	names := map[string]string{}
	if defs, _, err := s.dueInputs(ctx); err == nil {
		for _, def := range defs {
			names[def.ID] = def.Name
		}
		if def, at, ok, _ := s.nextDue(ctx, &st, machine); ok {
			hs.Next = &HostNext{Task: def.ID, Name: def.Name, At: at}
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
		hs.Notices = append(hs.Notices, HostNotice{ID: fmt.Sprintf("pass-%d", pe.At.UnixNano()), Kind: NoticeFailed, Name: passName, Title: "Scheduled tasks could not run", Body: pe.Message})
		if hs.LastFailure == nil || pe.At.After(hs.LastFailure.At) {
			hs.LastFailure = &HostFailure{Name: passName, At: pe.At, Message: pe.Message}
		}
	}
	return hs, nil
}

func itemNotice(it Item, st *State, name string) (HostNotice, bool) {
	n := HostNotice{ID: "item-" + it.ID + "-" + string(it.State), Task: it.Task, Name: name}
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
