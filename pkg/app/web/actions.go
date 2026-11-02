package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/actions"
	"github.com/atomicobject/rhizome/pkg/vault/actiondef"
	"github.com/atomicobject/rhizome/pkg/vault/assetroots"
)

const maxActionBody = 1 << 20

// handleActions serves module actions ([[module-actions|SPEC-0131]]):
// GET /api/v1/actions lists them, and POST /api/v1/actions/{module}/{name}
// runs one with the JSON body as its input. Every run that starts answers
// 200 with its envelope. The mutation-origin guard in Handler covers POST.
func (s *Server) handleActions(w http.ResponseWriter, r *http.Request) {
	runner := s.cfg.Actions
	if runner == nil {
		runner = actions.New(s.cfg.VaultPath)
	}
	rest := strings.Trim(strings.TrimPrefix(r.URL.EscapedPath(), "/api/v1/actions"), "/")
	if rest == "" {
		if r.Method != http.MethodGet {
			writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
			return
		}
		list, err := runner.List()
		if err != nil {
			writePublicError(w, http.StatusInternalServerError, PublicErrorInternal, err)
			return
		}
		writeJSON(w, http.StatusOK, list)
		return
	}
	module, name, ok := strings.Cut(rest, "/")
	module, _ = url.PathUnescape(module)
	name, _ = url.PathUnescape(name)
	if !ok || !assetroots.ValidModuleName(module) || !assetroots.ValidModuleName(name) {
		writePublicError(w, http.StatusNotFound, PublicErrorNotFound, errors.New("action not found"))
		return
	}
	if r.Method != http.MethodPost {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	var input map[string]any
	body, err := io.ReadAll(io.LimitReader(r.Body, maxActionBody+1))
	switch {
	case err != nil:
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	case len(body) > maxActionBody:
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, errors.New("action input is too large"))
		return
	case len(strings.TrimSpace(string(body))) > 0:
		if err := json.Unmarshal(body, &input); err != nil || input == nil {
			writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, errors.New("action input must be a JSON object"))
			return
		}
	}
	envelope, err := runner.Run(r.Context(), module+"/"+name, input)
	switch {
	case errors.Is(err, actions.ErrUnknownAction):
		writePublicError(w, http.StatusNotFound, PublicErrorNotFound, err)
	case errors.Is(err, actiondef.ErrInvalidInput):
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
	case err != nil:
		writePublicError(w, http.StatusInternalServerError, PublicErrorInternal, err)
	default:
		writeJSON(w, http.StatusOK, envelope)
	}
}
