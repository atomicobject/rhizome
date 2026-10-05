package web

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
)

// RuntimeControl wires the loopback control API (SPEC-0104). Every callback is
// optional; a nil callback answers 503 so lanes can land independently. The
// token comes from the 0600 manifest; health is the only open route.
type RuntimeControl struct {
	Token  string
	Health func() appruntime.Health
	// Shutdown asks the process to exit gracefully. It returns after the
	// request is accepted, not after exit.
	Shutdown func(reason string)
	// SubmitIndex creates or joins the explicit index job; joined is true when
	// an existing job was returned.
	SubmitIndex func(ctx context.Context) (handle lane.Handle, joined bool, err error)
	// LookupIndex finds a job for its event stream or cancellation.
	LookupIndex func(id string) (lane.Handle, bool)
	// AgentOp executes one catalog operation with the runtime's live config.
	AgentOp func(ctx context.Context, name string, req appruntime.AgentOpRequest) (agentapi.CallOutcome, error)
	// Touch records client activity for the idle timer.
	Touch func()
	// Hold keeps the runtime from idling until release; UI event streams hold
	// it for as long as they are open.
	Hold func() (release func())
}

const (
	PublicErrorUnauthorized        = "UNAUTHORIZED"
	PublicErrorControlUnavailable  = "CONTROL_UNAVAILABLE"
	PublicErrorRebuildNotAllowed   = "REBUILD_NOT_ALLOWED"
	PublicErrorRuntimeJobNotFound  = "JOB_NOT_FOUND"
	PublicErrorRuntimeJobCancelled = "JOB_CANCELLED"
)

func (s *Server) registerRuntimeControlRoutes() {
	s.mux.HandleFunc(appruntime.HealthPath, publicGET(s.handleRuntimeHealth))
	s.mux.HandleFunc(appruntime.ShutdownPath, s.controlPOST(s.handleRuntimeShutdown))
	s.mux.HandleFunc(appruntime.IndexJobsPath, s.controlPOST(s.handleRuntimeIndexSubmit))
	s.mux.HandleFunc(appruntime.IndexJobsPathPrefix, s.controlAny(s.handleRuntimeIndexJob))
	s.mux.HandleFunc(appruntime.AgentOpsPathPrefix, s.controlPOST(s.handleRuntimeAgentOp))
}

func (s *Server) control() *RuntimeControl { return s.cfg.RuntimeControl }

// holdRuntime keeps a headless runtime from idling while a UI event stream is
// open, so an idle browser or desktop window keeps its backend.
func (s *Server) holdRuntime() (release func()) {
	if control := s.control(); control != nil && control.Hold != nil {
		return control.Hold()
	}
	return func() {}
}

func (s *Server) controlAuthorized(r *http.Request) bool {
	control := s.control()
	if control == nil || control.Token == "" {
		return false
	}
	presented := appruntime.BearerToken(r)
	return presented != "" && subtle.ConstantTimeCompare([]byte(presented), []byte(control.Token)) == 1
}

func (s *Server) controlAny(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.control() == nil {
			writePublicError(w, http.StatusServiceUnavailable, PublicErrorControlUnavailable, errors.New("runtime control is not enabled"))
			return
		}
		if !isLoopbackHostname(requestHostname(r.Host)) {
			// The listener is loopback-only; a non-loopback Host header means a
			// DNS-rebinding page, not a Rhizome client.
			writePublicError(w, http.StatusForbidden, PublicErrorUnauthorized, errors.New("control routes accept loopback hosts only"))
			return
		}
		if !s.controlAuthorized(r) {
			writePublicError(w, http.StatusUnauthorized, PublicErrorUnauthorized, errors.New("control token required"))
			return
		}
		if touch := s.control().Touch; touch != nil {
			touch()
		}
		handler(w, r)
	}
}

func (s *Server) controlPOST(handler http.HandlerFunc) http.HandlerFunc {
	return s.controlAny(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
			return
		}
		handler(w, r)
	})
}

func (s *Server) handleRuntimeHealth(w http.ResponseWriter, r *http.Request) {
	control := s.control()
	if control == nil || control.Health == nil {
		writePublicError(w, http.StatusServiceUnavailable, PublicErrorControlUnavailable, errors.New("runtime health is not enabled"))
		return
	}
	writeJSON(w, http.StatusOK, control.Health())
}

func (s *Server) handleRuntimeShutdown(w http.ResponseWriter, r *http.Request) {
	control := s.control()
	if control.Shutdown == nil {
		writePublicError(w, http.StatusServiceUnavailable, PublicErrorControlUnavailable, errors.New("shutdown is not enabled"))
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	control.Shutdown(body.Reason)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "shutting-down"})
}

func (s *Server) handleRuntimeIndexSubmit(w http.ResponseWriter, r *http.Request) {
	control := s.control()
	if control.SubmitIndex == nil {
		writePublicError(w, http.StatusServiceUnavailable, PublicErrorControlUnavailable, errors.New("index jobs are not enabled"))
		return
	}
	handle, joined, err := control.SubmitIndex(r.Context())
	if err != nil {
		if errors.Is(err, lane.ErrRebuildNotAllowed) {
			writePublicError(w, http.StatusConflict, PublicErrorRebuildNotAllowed, err)
			return
		}
		writePublicError(w, http.StatusInternalServerError, PublicErrorInternal, err)
		return
	}
	writeJSON(w, http.StatusAccepted, appruntime.IndexJobResponse{JobID: handle.ID(), Joined: joined})
}

// handleRuntimeIndexJob serves GET .../{id}/events (SSE) and DELETE .../{id}.
func (s *Server) handleRuntimeIndexJob(w http.ResponseWriter, r *http.Request) {
	control := s.control()
	if control.LookupIndex == nil {
		writePublicError(w, http.StatusServiceUnavailable, PublicErrorControlUnavailable, errors.New("index jobs are not enabled"))
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, appruntime.IndexJobsPathPrefix)
	id, tail, _ := strings.Cut(rest, "/")
	handle, ok := control.LookupIndex(id)
	if !ok {
		writePublicError(w, http.StatusNotFound, PublicErrorRuntimeJobNotFound, fmt.Errorf("index job %q not found", id))
		return
	}
	switch {
	case r.Method == http.MethodDelete && tail == "":
		handle.Cancel()
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "cancelling", "jobId": id})
	case r.Method == http.MethodGet && tail == "events":
		s.streamLaneEvents(w, r, handle)
	default:
		writePublicError(w, http.StatusNotFound, PublicErrorNotFound, fmt.Errorf("api route %q not found", r.URL.Path))
	}
}

func (s *Server) streamLaneEvents(w http.ResponseWriter, r *http.Request, handle lane.Handle) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writePublicError(w, http.StatusInternalServerError, PublicErrorStreamingUnsupported, errors.New("streaming unsupported"))
		return
	}
	events, unsubscribe := handle.Subscribe()
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	touch := s.control().Touch
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			// An open stream is client activity: keep a headless runtime alive.
			if touch != nil {
				touch()
			}
			_, _ = fmt.Fprintf(w, ": heartbeat %d\n\n", time.Now().Unix())
			flusher.Flush()
		case event, open := <-events:
			if !open {
				// The lane closed the stream without a terminal event (dropped
				// at shutdown). Synthesize one so the client never hangs.
				s.writeLaneEvent(w, flusher, terminalEventFor(handle))
				return
			}
			s.writeLaneEvent(w, flusher, event)
			if event.Type == lane.EventDone {
				return
			}
		}
	}
}

func (s *Server) writeLaneEvent(w http.ResponseWriter, flusher http.Flusher, event lane.Event) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
	flusher.Flush()
}

func terminalEventFor(handle lane.Handle) lane.Event {
	event := lane.Event{Type: lane.EventDone, JobID: handle.ID(), Kind: handle.Kind(), At: time.Now(), Outcome: lane.OutcomeCancelled, Error: "job ended without a terminal event"}
	select {
	case <-handle.Done():
		if err := handle.Err(); err != nil {
			event.Error = err.Error()
			if !errors.Is(err, context.Canceled) {
				event.Outcome = lane.OutcomeFailed
			}
		} else {
			event.OK, event.Outcome, event.Error = true, lane.OutcomeOK, ""
		}
	default:
	}
	return event
}

func (s *Server) handleRuntimeAgentOp(w http.ResponseWriter, r *http.Request) {
	control := s.control()
	if control.AgentOp == nil {
		writePublicError(w, http.StatusServiceUnavailable, PublicErrorControlUnavailable, errors.New("agent operations are not enabled"))
		return
	}
	name := strings.Trim(strings.TrimPrefix(r.URL.Path, appruntime.AgentOpsPathPrefix), "/")
	if name == "" || strings.Contains(name, "/") {
		writePublicError(w, http.StatusNotFound, PublicErrorNotFound, fmt.Errorf("agent operation %q not found", name))
		return
	}
	var req appruntime.AgentOpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, fmt.Errorf("decode agent operation: %w", err))
		return
	}
	outcome, err := control.AgentOp(r.Context(), name, req)
	if err != nil {
		writePublicError(w, http.StatusInternalServerError, PublicErrorInternal, err)
		return
	}
	// Domain failures travel inside the outcome (ok=false); only transport
	// or protocol problems become HTTP errors.
	writeJSON(w, http.StatusOK, outcome)
}
