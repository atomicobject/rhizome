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

// TaskHosts counts the host streams open on this runtime and remembers a
// drain. The runtime dispatches scheduled tasks only while Dispatching.
type TaskHosts struct {
	mu      sync.Mutex
	open    int
	drained bool
}

// Hold registers an open host stream until release.
func (h *TaskHosts) Hold() (release func()) {
	h.mu.Lock()
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

// Drain stops new dispatch until the runtime restarts.
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

// Dispatching reports whether a host stream is open and the runtime has not
// been drained.
func (h *TaskHosts) Dispatching() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.open > 0 && !h.drained
}

// TaskMachine sets the person's per-machine "Run tasks on this machine"
// (SPEC-0127 US10).
// LEAD: lane A's *tasks.Service has this method; once it lands, fold it into
// TasksAPI and drop the type assertion in setTaskMachine.
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
	return status, nil
}

// drainTaskHost stops new dispatch and answers with the run in progress,
// so Desktop can ask whether to wait for it or stop it.
func (s *Server) drainTaskHost(w http.ResponseWriter, r *http.Request, api TasksAPI) {
	hosts := s.taskHosts(w)
	if hosts == nil {
		return
	}
	hosts.Drain()
	status, err := api.HostStatus(r.Context())
	writeTasksResult(w, map[string]*tasks.HostRun{"running": status.Running}, err)
}

func (s *Server) setTaskMachine(w http.ResponseWriter, r *http.Request, api TasksAPI, runHere bool) {
	machine, ok := api.(TaskMachine)
	if !ok {
		writeError(w, http.StatusNotImplemented, errors.New("this Rhizome cannot change where tasks run"))
		return
	}
	writeTasksAction(w, machine.SetRunHere(r.Context(), runHere))
}
