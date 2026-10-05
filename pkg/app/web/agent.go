package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/agentchat"
)

func (s *Server) handleAgentSettings(w http.ResponseWriter, r *http.Request) {
	if s.agentService == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("agent service unavailable"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		response, err := s.agentService.Settings(r.Context())
		if err != nil {
			writeAgentError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, response)
	case http.MethodPut:
		defer r.Body.Close()
		var req struct {
			Settings agentchat.Settings `json:"settings"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		response, err := s.agentService.SaveSettings(r.Context(), req.Settings)
		if err != nil {
			writeAgentError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, response)
	default:
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

func (s *Server) handleAgentSessions(w http.ResponseWriter, r *http.Request) {
	if s.agentService == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("agent service unavailable"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		sessions, err := s.agentService.ListSessions(r.Context())
		if err != nil {
			writeAgentError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
	case http.MethodPost:
		defer r.Body.Close()
		var req struct {
			Title string `json:"title,omitempty"`
		}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&req)
		}
		resp, err := s.agentService.CreateSession(r.Context(), req.Title)
		if err != nil {
			writeAgentError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	default:
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

func (s *Server) handleAgentSessionByID(w http.ResponseWriter, r *http.Request) {
	if s.agentService == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("agent service unavailable"))
		return
	}
	sessionID, action, err := parseAgentSessionPath(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	switch {
	case r.Method == http.MethodGet && action == "":
		resp, err := s.agentService.GetSession(r.Context(), sessionID)
		if err != nil {
			writeAgentError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	case r.Method == http.MethodDelete && action == "":
		if err := s.agentService.DeleteSession(r.Context(), sessionID); err != nil {
			writeAgentError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
	case r.Method == http.MethodPost && action == "messages":
		defer r.Body.Close()
		var req agentchat.SendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		resp, err := s.agentService.SendMessage(r.Context(), sessionID, req.Content)
		if err != nil {
			writeAgentError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, resp)
	case r.Method == http.MethodGet && action == "events":
		s.handleAgentEvents(w, r, sessionID)
	case r.Method == http.MethodPost && strings.HasPrefix(action, "approvals/"):
		defer r.Body.Close()
		requestID := strings.TrimSpace(strings.TrimPrefix(action, "approvals/"))
		var req agentchat.ApprovalRequest
		if requestID == "" {
			writeError(w, http.StatusBadRequest, errors.New("approval request id is required"))
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := s.agentService.RespondToApproval(sessionID, requestID, req.Decision); err != nil {
			writeAgentError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"responded": true})
	case r.Method == http.MethodPost && action == "interrupt":
		if err := s.agentService.Interrupt(r.Context(), sessionID); err != nil {
			writeAgentError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"interrupted": true})
	default:
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

func (s *Server) handleAgentEvents(w http.ResponseWriter, r *http.Request, sessionID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	if _, err := s.agentService.GetSession(r.Context(), sessionID); err != nil {
		writeAgentError(w, err)
		return
	}
	stream, err := newAgentEventStream(r.Context(), s.agentService, sessionID, agentEventCursor(r))
	if err != nil {
		writeAgentError(w, err)
		return
	}
	defer stream.close()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()
	for event, ok := stream.next(r.Context()); ok; event, ok = stream.next(r.Context()) {
		writeAgentSSE(w, event)
		flusher.Flush()
	}
}

type agentEventSource interface {
	EventsAfter(context.Context, string, int64) ([]agentchat.Event, error)
	Subscribe(string) (<-chan agentchat.Event, func())
}

type agentEventStream struct {
	replay      []agentchat.Event
	replayIndex int
	live        <-chan agentchat.Event
	unsubscribe func()
	cursor      int64
}

func newAgentEventStream(ctx context.Context, source agentEventSource, sessionID string, after int64) (*agentEventStream, error) {
	live, unsubscribe := source.Subscribe(sessionID)
	replay, err := source.EventsAfter(ctx, sessionID, after)
	if err != nil {
		unsubscribe()
		return nil, err
	}
	return &agentEventStream{replay: replay, live: live, unsubscribe: unsubscribe, cursor: after}, nil
}

func (s *agentEventStream) close() {
	if s.unsubscribe != nil {
		s.unsubscribe()
		s.unsubscribe = nil
	}
}

func (s *agentEventStream) next(ctx context.Context) (agentchat.Event, bool) {
	for {
		var event agentchat.Event
		if s.replayIndex < len(s.replay) {
			event = s.replay[s.replayIndex]
			s.replayIndex++
		} else {
			select {
			case <-ctx.Done():
				return agentchat.Event{}, false
			case next, ok := <-s.live:
				if !ok {
					return agentchat.Event{}, false
				}
				event = next
			}
		}
		if event.ID > 0 {
			if event.ID <= s.cursor {
				continue
			}
			s.cursor = event.ID
		}
		return event, true
	}
}

func agentEventCursor(r *http.Request) int64 {
	for _, raw := range []string{r.Header.Get("Last-Event-ID"), r.URL.Query().Get("after")} {
		if parsed, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}

func parseAgentSessionPath(urlPath string) (sessionID, action string, err error) {
	const prefix = "/api/agent/sessions/"
	if !strings.HasPrefix(urlPath, prefix) {
		return "", "", errors.New("invalid agent session route")
	}
	rest := strings.Trim(strings.TrimPrefix(urlPath, prefix), "/")
	if rest == "" {
		return "", "", errors.New("agent session id is required")
	}
	parts := strings.Split(rest, "/")
	if len(parts) > 3 {
		return "", "", errors.New("invalid agent session route")
	}
	sessionID = strings.TrimSpace(parts[0])
	if sessionID == "" {
		return "", "", errors.New("agent session id is required")
	}
	if len(parts) >= 2 {
		action = strings.TrimSpace(strings.Join(parts[1:], "/"))
	}
	return sessionID, action, nil
}

func writeAgentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agentchat.ErrConflict):
		writeError(w, http.StatusConflict, err)
	case errors.Is(err, agentchat.ErrNotFound):
		writeError(w, http.StatusNotFound, err)
	default:
		writeError(w, http.StatusBadRequest, err)
	}
}

func writeAgentSSE(w http.ResponseWriter, event agentchat.Event) {
	data, err := json.Marshal(event)
	if err != nil {
		data = []byte(`{"error":"marshal failed"}`)
	}
	if event.ID > 0 {
		_, _ = fmt.Fprintf(w, "id: %d\n", event.ID)
	}
	_, _ = fmt.Fprintln(w, "event: agent_event")
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
}
