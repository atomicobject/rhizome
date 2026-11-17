package web

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/modulereadiness"
)

type ModuleReadiness = modulereadiness.API

// starterProblems describes the setup notices that ask the user to act, in the
// words the app's banner uses.
func starterProblems(snapshots []modulereadiness.Snapshot) []string {
	labels := map[modulereadiness.State]string{
		modulereadiness.Failed:        "setup failed",
		modulereadiness.Blocked:       "setup blocked",
		modulereadiness.RequiredInput: "setup needs your input",
	}
	var problems []string
	for _, item := range snapshots {
		label, ok := labels[item.State]
		if !ok {
			continue
		}
		title := cmp.Or(item.Title, item.Module)
		problem := fmt.Sprintf("%s (module %s): %s", title, item.Module, label)
		if item.Reason != "" {
			problem += ". " + item.Reason
		}
		problems = append(problems, problem)
	}
	return problems
}

func (s *Server) handleStarterReadiness(w http.ResponseWriter, r *http.Request) {
	owner := s.cfg.ModuleReadiness
	if owner == nil && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, []modulereadiness.Snapshot{})
		return
	}
	if owner == nil {
		writePublicError(w, http.StatusServiceUnavailable, PublicErrorInternal, errors.New("starter readiness is unavailable"))
		return
	}
	module := strings.TrimPrefix(r.URL.Path, "/api/v1/starter-readiness")
	if module == "" && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, owner.Snapshot())
		return
	}
	if !strings.HasPrefix(module, "/") || strings.Contains(module[1:], "/") || r.Method != http.MethodPost {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("use GET for status or POST for module recovery"))
		return
	}
	var request modulereadiness.Request
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, errors.New("expected one recovery request"))
		return
	}
	if err := owner.Recover(module[1:], request); err != nil {
		writePublicError(w, http.StatusConflict, PublicErrorBadRequest, err)
		return
	}
	writeJSON(w, http.StatusAccepted, owner.Snapshot())
}
