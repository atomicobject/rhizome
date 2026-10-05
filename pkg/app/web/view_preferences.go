package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/userstate"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
)

const maxViewPreferencesBodyBytes = 256 << 10

type viewPreferencesPatchRequest struct {
	Scope            userstate.Scope            `json:"scope"`
	ExpectedRevision *uint64                    `json:"expectedRevision"`
	Set              map[string]json.RawMessage `json:"set,omitempty"`
	Unset            []string                   `json:"unset,omitempty"`
}

type viewPreferencesResetRequest struct {
	Scope            userstate.Scope `json:"scope"`
	ExpectedRevision *uint64         `json:"expectedRevision"`
	IncludeSlots     bool            `json:"includeSlots,omitempty"`
}

type viewPreferencesImportRequest struct {
	Scope       userstate.Scope            `json:"scope"`
	MigrationID string                     `json:"migrationId"`
	Values      map[string]json.RawMessage `json:"values"`
}

type viewPreferencesImportResponse struct {
	userstate.Snapshot
	Imported bool `json:"imported"`
}

func decodeViewPreferences(reader io.Reader, target any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("expected exactly one JSON value")
	}
	return nil
}

func (s *Server) viewPreferencesReady(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	allowed := false
	for _, method := range methods {
		allowed = allowed || r.Method == method
	}
	if !allowed {
		w.Header().Set("Allow", strings.Join(methods, ", "))
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	if s.userState == nil {
		message := "personal preferences are unavailable; check the local state database and restart Rhizome"
		var future *migration.ErrFutureSchema
		var drift *migration.ErrSchemaDrift
		if errors.As(s.userStateErr, &future) {
			message = "personal preferences need a newer Rhizome version"
		} else if errors.As(s.userStateErr, &drift) {
			message = "personal preferences are unavailable because the saved database schema is invalid"
		}
		writePublicError(w, http.StatusServiceUnavailable, "USER_STATE_UNAVAILABLE", errors.New(message))
		return false
	}
	return true
}

func writeViewPreferencesError(w http.ResponseWriter, err error) {
	var conflict *userstate.ConflictError
	if errors.As(err, &conflict) {
		writePublicErrorDetails(w, http.StatusConflict, PublicErrorConflict, err, map[string]any{"current": conflict.Current})
	} else if errors.Is(err, userstate.ErrInvalid) {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
	} else {
		writePublicError(w, http.StatusInternalServerError, PublicErrorInternal, err)
	}
}

func (s *Server) publishViewPreferences(snapshot userstate.Snapshot, includeSlots bool) {
	s.NotifyGlobalEvent("view_preferences.changed", struct {
		Scope        userstate.Scope `json:"scope"`
		Revision     uint64          `json:"revision"`
		VaultKey     string          `json:"vaultKey"`
		IncludeSlots bool            `json:"includeSlots,omitempty"`
	}{snapshot.Scope, snapshot.Revision, s.cfg.VaultPath, includeSlots})
}

func (s *Server) handleViewPreferences(w http.ResponseWriter, r *http.Request) {
	if !s.viewPreferencesReady(w, r, http.MethodGet, http.MethodPatch) {
		return
	}
	if r.Method == http.MethodGet {
		query, err := r.URL.Query()["scope"], error(nil)
		var scope userstate.Scope
		if len(query) != 1 || len(query[0]) > userstate.MaxScopeBytes {
			err = errors.New("exactly one bounded scope query parameter is required")
		} else {
			err = decodeViewPreferences(strings.NewReader(query[0]), &scope)
		}
		if err != nil {
			writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
			return
		}
		result, err := s.userState.Read(r.Context(), scope)
		if err != nil {
			writeViewPreferencesError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxViewPreferencesBodyBytes)
	defer r.Body.Close()
	var req viewPreferencesPatchRequest
	if err := decodeViewPreferences(r.Body, &req); err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	if req.ExpectedRevision == nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, errors.New("expectedRevision is required"))
		return
	}
	result, err := s.userState.Patch(r.Context(), req.Scope, *req.ExpectedRevision, req.Set, req.Unset)
	if err != nil {
		writeViewPreferencesError(w, err)
		return
	}
	s.publishViewPreferences(result, false)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleViewPreferencesReset(w http.ResponseWriter, r *http.Request) {
	if !s.viewPreferencesReady(w, r, http.MethodPost) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxViewPreferencesBodyBytes)
	defer r.Body.Close()
	var req viewPreferencesResetRequest
	if err := decodeViewPreferences(r.Body, &req); err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	if req.ExpectedRevision == nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, errors.New("expectedRevision is required"))
		return
	}
	var result userstate.Snapshot
	var err error
	if req.IncludeSlots {
		result, err = s.userState.ResetInstance(r.Context(), req.Scope, *req.ExpectedRevision)
	} else {
		result, err = s.userState.Reset(r.Context(), req.Scope, *req.ExpectedRevision)
	}
	if err != nil {
		writeViewPreferencesError(w, err)
		return
	}
	s.publishViewPreferences(result, req.IncludeSlots)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleViewPreferencesImport(w http.ResponseWriter, r *http.Request) {
	if !s.viewPreferencesReady(w, r, http.MethodPost) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxViewPreferencesBodyBytes)
	defer r.Body.Close()
	var req viewPreferencesImportRequest
	if err := decodeViewPreferences(r.Body, &req); err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	if req.Values == nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, errors.New("values must be an object"))
		return
	}
	result, imported, err := s.userState.ImportIfAbsent(r.Context(), req.Scope, req.MigrationID, req.Values)
	if err != nil {
		writeViewPreferencesError(w, err)
		return
	}
	if imported {
		s.publishViewPreferences(result, false)
	}
	writeJSON(w, http.StatusOK, viewPreferencesImportResponse{Snapshot: result, Imported: imported})
}
