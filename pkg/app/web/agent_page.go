package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/agentchat"
)

func (s *Server) SetPageRelayOrigin(origin string) error {
	return s.agentService.SetPageRelayOrigin(origin)
}

func (s *Server) handleAgentPageRelay(w http.ResponseWriter, r *http.Request, sessionID string) {
	authorization := r.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "Bearer ") {
		writeError(w, http.StatusUnauthorized, agentchat.ErrPageUnauthorized)
		return
	}
	page, err := s.agentService.RequestPage(r.Context(), sessionID, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, agentchat.ErrPageUnauthorized) {
			status = http.StatusUnauthorized
		}
		writeError(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleAgentPageReply(w http.ResponseWriter, r *http.Request, sessionID, requestID string) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var reply struct {
		Title string  `json:"title"`
		Route string  `json:"route"`
		Text  *string `json:"text"`
		Error *string `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reply); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if reply.Text == nil && reply.Error == nil {
		writeError(w, http.StatusBadRequest, errors.New("text or error is required"))
		return
	}
	page := agentchat.PageText{Title: reply.Title, Route: reply.Route}
	if reply.Text != nil {
		page.Text = *reply.Text
	}
	if reply.Error != nil {
		page.Error = *reply.Error
	}
	if err := s.agentService.ReplyPage(sessionID, requestID, page); err != nil {
		writeAgentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"responded": true})
}
