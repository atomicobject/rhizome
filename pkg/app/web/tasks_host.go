package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/app/tasks"
)

// Rhizome Desktop hosts scheduled tasks by holding one host stream open to
// each repository's runtime (SPEC-0104 US7).
const (
	// taskHostPoll is how often the host stream rebuilds its status to send
	// changes; status reads task state and runs two git commands.
	taskHostPoll = 5 * time.Second
	// taskHostResend keeps an unchanged status coming at least every 30
	// seconds.
	taskHostResend = 25 * time.Second
)

// TaskHosts counts the host streams open on this runtime, remembers a
// drain, and tracks the last task process the runtime started. One mutex
// covers all three, so a scheduled pass never starts after a drain and only
// one run starts at a time from this runtime.
type TaskHosts struct {
	mu      sync.Mutex
	open    int
	drained bool
	// child is the latest run-due or run this runtime started.
	child *appruntime.Detached
}

// errTaskStarted answers Run now while a run this runtime started is alive.
var errTaskStarted = errors.New("a task run this runtime started is still running")

// Hold registers an open host stream until release. A host that connects
// after the last one closed starts fresh: a drain belongs to the Desktop that
// asked for it, so an attached runtime that outlived a complete quit
// schedules again when Desktop reopens.
func (h *TaskHosts) Hold() (release func()) {
	h.mu.Lock()
	if h.open == 0 {
		h.drained = false
	}
	h.open++
	h.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			h.mu.Lock()
			h.open--
			h.mu.Unlock()
		})
	}
}

// Held reports whether a host stream is open; a nil TaskHosts (no runtime
// host support) reports true, so nothing reads as hostless.
func (h *TaskHosts) Held() bool {
	if h == nil {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.open > 0
}

// Drain stops new dispatch until the runtime restarts. It waits for a
// dispatch in progress, so that one is reported as Child.
func (h *TaskHosts) Drain() {
	h.mu.Lock()
	h.drained = true
	h.mu.Unlock()
}

// Draining reports whether Drain was called.
func (h *TaskHosts) Draining() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.drained
}

// Hosting reports whether a host stream is open and the runtime has not
// been drained, the cheap test before a scheduled check reads task state.
func (h *TaskHosts) Hosting() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.open > 0 && !h.drained
}

// Dispatch starts a scheduled pass with start, checking under the lock
// Drain takes, immediately before starting, that a host stream is open, the
// runtime is not drained, and no process the runtime started is alive. It
// returns nil when it started nothing.
func (h *TaskHosts) Dispatch(start func() (*appruntime.Detached, error)) (*appruntime.Detached, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.open == 0 || h.drained || h.child.Alive() {
		return nil, nil
	}
	child, err := start()
	if err != nil {
		return nil, err
	}
	h.child = child
	return child, nil
}

// Launch starts a run the person asked for (Run now) with start, refusing
// while a process the runtime started is alive, as a second request does
// before the first run takes the run lock. A nil TaskHosts just starts it.
func (h *TaskHosts) Launch(start func() (*appruntime.Detached, error)) error {
	if h == nil {
		_, err := start()
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.child.Alive() {
		return fmt.Errorf("%w: %w", tasks.ErrBusy, errTaskStarted)
	}
	child, err := start()
	if err == nil && child != nil {
		h.child = child
	}
	return err
}

// Child is the process the runtime last started while it is alive, or nil.
func (h *TaskHosts) Child() *tasks.HostDispatch {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.child.Alive() {
		return nil
	}
	return &tasks.HostDispatch{PID: h.child.PID, StartedAt: h.child.StartedAt}
}

// TaskMachine sets the person's per-machine "Run tasks on this machine"
// (SPEC-0127 US10).
type TaskMachine interface {
	SetRunHere(ctx context.Context, on bool) error
}

func (s *Server) taskHosts(w http.ResponseWriter) *TaskHosts {
	if s.cfg.TaskHosts == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("task hosting unavailable"))
	}
	return s.cfg.TaskHosts
}

// serveTaskHost streams `event: status` with the repository's HostStatus at
// connect, when it changes, and at least every 30 seconds, for as long as
// the client holds it. Holding it keeps the runtime from idling and lets it
// dispatch scheduled tasks.
func (s *Server) serveTaskHost(w http.ResponseWriter, r *http.Request, api TasksAPI) {
	hosts := s.taskHosts(w)
	if hosts == nil {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	defer s.holdRuntime()()
	defer hosts.Hold()()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	poll := s.taskHostPoll
	if poll <= 0 {
		poll = taskHostPoll
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	var last []byte
	var sentAt time.Time
	for {
		event, data := "status", []byte(nil)
		status, err := s.hostStatus(r.Context(), api, hosts)
		if err == nil {
			data, err = json.Marshal(status)
		}
		if err != nil {
			event = "error"
			data, _ = json.Marshal(map[string]string{"error": err.Error()})
		}
		if !bytes.Equal(data, last) || time.Since(sentAt) >= taskHostResend {
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
			flusher.Flush()
			last, sentAt = data, time.Now()
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) hostStatus(ctx context.Context, api TasksAPI, hosts *TaskHosts) (tasks.HostStatus, error) {
	status, err := api.HostStatus(ctx)
	if err != nil {
		return status, err
	}
	status.Draining = hosts.Draining()
	if status.Running == nil {
		status.Dispatching = hosts.Child()
	}
	return status, nil
}

// drainTaskHost stops new dispatch and answers with the run in progress,
// or the process the runtime started that has recorded no run yet, so
// Desktop can ask whether to wait for it or stop it.
func (s *Server) drainTaskHost(w http.ResponseWriter, r *http.Request, api TasksAPI) {
	hosts := s.taskHosts(w)
	if hosts == nil {
		return
	}
	hosts.Drain()
	status, err := s.hostStatus(r.Context(), api, hosts)
	writeTasksResult(w, map[string]any{"running": status.Running, "dispatching": status.Dispatching}, err)
}

// stopTaskRun stops the run holding the run lock or, when no process holds
// it yet, the process this runtime started for a run.
func (s *Server) stopTaskRun(ctx context.Context, api TasksAPI) (bool, error) {
	stopped, err := api.Stop(ctx, tasks.DefaultStopWait)
	if err != nil || stopped {
		return stopped, err
	}
	child := s.cfg.TaskHosts.Child()
	if child == nil {
		return false, nil
	}
	if err := tasks.StopProcess(ctx, child.PID, tasks.DefaultStopWait); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Server) setTaskMachine(w http.ResponseWriter, r *http.Request, api TasksAPI, runHere bool) {
	machine, ok := api.(TaskMachine)
	if !ok {
		writeError(w, http.StatusNotImplemented, errors.New("this Rhizome cannot change where tasks run"))
		return
	}
	writeTasksAction(w, machine.SetRunHere(r.Context(), runHere))
}
