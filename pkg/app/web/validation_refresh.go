package web

import (
	"errors"
	"net/http"
)

func (s *Server) handlePublicValidationRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	if s.runtime == nil || s.runtime.Intel() == nil || !s.runtime.RequestValidationRefresh() {
		writePublicError(w, http.StatusServiceUnavailable, PublicErrorIndexInitializing, errors.New("validation refresh is unavailable while the runtime initializes"))
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"accepted": true})
}
