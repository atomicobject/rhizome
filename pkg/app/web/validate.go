package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/validate"
)

type ValidationEnvelope struct {
	RefreshPending      bool                      `json:"refreshPending"`
	Status              string                    `json:"status"`
	Health              string                    `json:"health"`
	Generation          int64                     `json:"generation"`
	PublishedGeneration int64                     `json:"publishedGeneration"`
	StartedAt           *time.Time                `json:"startedAt,omitempty"`
	ComputedAt          *time.Time                `json:"computedAt,omitempty"`
	DurationMs          int64                     `json:"durationMs,omitempty"`
	Error               string                    `json:"error,omitempty"`
	Snapshot            *semdb.ValidationSnapshot `json:"snapshot,omitempty"`
}

func (s *Server) handlePublicValidationDiagnostics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	var store *semdb.Store
	if s != nil && s.runtime != nil {
		store = s.runtime.Intel()
	}
	if store == nil {
		writePublicError(w, http.StatusServiceUnavailable, PublicErrorIndexInitializing, errors.New("validation diagnostics are unavailable while the index initializes"))
		return
	}
	generation, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("generation")), 10, 64)
	if err != nil || generation <= 0 {
		writePublicError(w, http.StatusBadRequest, PublicErrorValidationPageFilter, errors.New("validation generation must be a positive integer"))
		return
	}
	limit := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil {
			writePublicError(w, http.StatusBadRequest, PublicErrorValidationPageLimit, errors.New("validation diagnostic limit must be an integer"))
			return
		}
	}
	check := strings.TrimSpace(r.URL.Query().Get("check"))
	if check != "" {
		canonical, ok := validate.CanonicalCheck(check)
		if !ok {
			writePublicError(w, http.StatusBadRequest, PublicErrorValidationPageFilter, fmt.Errorf("unknown validation check %q", check))
			return
		}
		check = canonical
	}
	page, err := store.GetValidationDiagnosticsPage(r.Context(), semdb.ValidationDiagnosticPageRequest{
		Generation: generation,
		Limit:      limit,
		Cursor:     strings.TrimSpace(r.URL.Query().Get("cursor")),
		Sort:       strings.TrimSpace(r.URL.Query().Get("sort")),
		Filter: semdb.ValidationDiagnosticFilter{
			Check: check, Code: strings.TrimSpace(r.URL.Query().Get("code")), Variant: strings.TrimSpace(r.URL.Query().Get("variant")),
			Path: strings.TrimSpace(r.URL.Query().Get("path")), Text: strings.TrimSpace(r.URL.Query().Get("text")), RepairAvailability: strings.TrimSpace(r.URL.Query().Get("repairAvailability")),
			ScopeKind: strings.TrimSpace(r.URL.Query().Get("scopeKind")), ScopeKey: strings.TrimSpace(r.URL.Query().Get("scopeKey")),
			InterfaceImplementors: s.validationInterfaceImplementors(),
		},
	})
	if err != nil {
		switch {
		case errors.Is(err, semdb.ErrValidationPageLimit):
			writePublicError(w, http.StatusBadRequest, PublicErrorValidationPageLimit, err)
		case errors.Is(err, semdb.ErrValidationPageCursor):
			writePublicError(w, http.StatusBadRequest, PublicErrorValidationPageCursor, err)
		case errors.Is(err, semdb.ErrValidationPageFilter):
			writePublicError(w, http.StatusBadRequest, PublicErrorValidationPageFilter, err)
		case errors.Is(err, semdb.ErrValidationPageSort):
			writePublicError(w, http.StatusBadRequest, PublicErrorValidationPageSort, err)
		case errors.Is(err, semdb.ErrValidationGenerationExpired):
			writePublicError(w, http.StatusGone, PublicErrorValidationGeneration, err)
		default:
			writePublicError(w, http.StatusInternalServerError, PublicErrorInternal, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handlePublicValidationSummaries(w http.ResponseWriter, r *http.Request) {
	servePublicValidationRead(s, w, r, "validation summaries",
		func(request *semdb.ValidationScopeSummaryRequest) *semdb.ValidationDiagnosticFilter {
			return &request.Filter
		},
		(*semdb.Store).GetValidationScopeSummaries)
}

func (s *Server) handlePublicValidationGroups(w http.ResponseWriter, r *http.Request) {
	servePublicValidationRead(s, w, r, "validation groups",
		func(request *semdb.ValidationIssueGroupRequest) *semdb.ValidationDiagnosticFilter {
			return &request.Filter
		},
		(*semdb.Store).GetValidationIssueGroups)
}

// servePublicValidationRead serves a generation-bound POST read whose JSON
// body carries a diagnostic filter: strict decoding, canonical check names,
// 400 for an invalid scope or filter, and 410 for an expired generation.
func servePublicValidationRead[Request, Response any](
	s *Server, w http.ResponseWriter, r *http.Request, name string,
	filter func(*Request) *semdb.ValidationDiagnosticFilter,
	read func(*semdb.Store, context.Context, Request) (Response, error),
) {
	if r.Method != http.MethodPost {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	var store *semdb.Store
	if s != nil && s.runtime != nil {
		store = s.runtime.Intel()
	}
	if store == nil {
		writePublicError(w, http.StatusServiceUnavailable, PublicErrorIndexInitializing, fmt.Errorf("%s are unavailable while the index initializes", name))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request Request
	if err := decoder.Decode(&request); err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorValidationScope, fmt.Errorf("decode %s request: %w", name, err))
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writePublicError(w, http.StatusBadRequest, PublicErrorValidationScope, fmt.Errorf("%s request must contain one JSON object", name))
		return
	}
	requestFilter := filter(&request)
	if requestFilter.Check != "" {
		canonical, ok := validate.CanonicalCheck(requestFilter.Check)
		if !ok {
			writePublicError(w, http.StatusBadRequest, PublicErrorValidationScope, fmt.Errorf("unknown validation check %q", requestFilter.Check))
			return
		}
		requestFilter.Check = canonical
	}
	requestFilter.InterfaceImplementors = s.validationInterfaceImplementors()
	response, err := read(store, r.Context(), request)
	if err != nil {
		switch {
		case errors.Is(err, semdb.ErrValidationScopeRequest):
			writePublicError(w, http.StatusBadRequest, PublicErrorValidationScope, err)
		case errors.Is(err, semdb.ErrValidationGenerationExpired):
			writePublicError(w, http.StatusGone, PublicErrorValidationGeneration, err)
		default:
			writePublicError(w, http.StatusInternalServerError, PublicErrorInternal, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, response)
}

// validationInterfaceImplementors resolves interface scopes for the store,
// which has no schema. Without a loadable schema an interface scope matches
// only the snapshot's explicit interface rows.
func (s *Server) validationInterfaceImplementors() map[string][]string {
	defs, err := s.ontologyDefinitions()
	if err != nil || defs == nil {
		return nil
	}
	return ontology.InterfaceImplementors(defs.schema)
}

const (
	ValidationHealthNeverChecked  = "never_checked"
	ValidationHealthRunning       = "running"
	ValidationHealthCurrentClean  = "current_clean"
	ValidationHealthCurrentIssues = "current_issues"
	ValidationHealthIncomplete    = "incomplete"
	ValidationHealthFailed        = "failed"
	ValidationHealthStale         = "stale"
)

// readCachedValidationEnvelope reads the pre-computed validation result from SQLite.
// The result is published by the indexing pipeline and the validation refresh
// coordinator. Queue status comes from the live runtime.
func (s *Server) readCachedValidationEnvelope(ctx context.Context) (envelope ValidationEnvelope) {
	defer func() {
		if s != nil && s.runtime != nil {
			envelope.RefreshPending = s.runtime.ValidationRefreshPending()
		}
	}()
	if s == nil || s.runtime == nil {
		return ValidationEnvelope{Status: semdb.ValidationStatusNeverRan, Health: ValidationHealthNeverChecked}
	}
	store := s.runtime.Intel()
	if store == nil {
		return ValidationEnvelope{Status: semdb.ValidationStatusNeverRan, Health: ValidationHealthNeverChecked}
	}
	read, err := store.GetValidationStateSnapshot(ctx)
	if err != nil {
		return ValidationEnvelope{Status: semdb.ValidationStatusError, Health: ValidationHealthFailed, Error: err.Error()}
	}
	state := read.State
	out := validationEnvelopeFromState(state)
	if read.HasSnapshot {
		out.Snapshot = &read.Snapshot
	}
	out.Health = validationHealth(out.Status, out.Snapshot)
	return out
}

func validationHealth(status string, snapshot *semdb.ValidationSnapshot) string {
	switch status {
	case semdb.ValidationStatusNeverRan:
		return ValidationHealthNeverChecked
	case semdb.ValidationStatusRunning:
		return ValidationHealthRunning
	case semdb.ValidationStatusError:
		return ValidationHealthFailed
	}
	if snapshot == nil || snapshot.Completion != semdb.ValidationCompletionComplete {
		return ValidationHealthIncomplete
	}
	if strings.TrimSpace(snapshot.StaleReason) != "" {
		return ValidationHealthStale
	}
	if snapshot.ErrorCount > 0 || !validationSnapshotChecksComplete(snapshot) {
		return ValidationHealthFailed
	}
	if snapshot.IssueCount > 0 {
		return ValidationHealthCurrentIssues
	}
	return ValidationHealthCurrentClean
}

func validationSnapshotChecksComplete(snapshot *semdb.ValidationSnapshot) bool {
	if snapshot == nil || len(snapshot.SelectedChecks) != len(snapshot.Checks) {
		return false
	}
	completed := make(map[string]struct{}, len(snapshot.Checks))
	for _, check := range snapshot.Checks {
		if check.Outcome != semdb.ValidationCheckOutcomeCompleted && check.Outcome != semdb.ValidationCheckOutcomeNotApplicable {
			return false
		}
		completed[check.Check] = struct{}{}
	}
	for _, check := range snapshot.SelectedChecks {
		if _, ok := completed[check]; !ok {
			return false
		}
	}
	return true
}

func validationEnvelopeFromState(state semdb.ValidationState) ValidationEnvelope {
	status := strings.TrimSpace(state.Status)
	if status == "" {
		status = semdb.ValidationStatusNeverRan
	}
	out := ValidationEnvelope{
		Status:              status,
		Health:              validationHealth(status, nil),
		Generation:          state.Generation,
		PublishedGeneration: state.PublishedGeneration,
		DurationMs:          state.DurationMs,
		Error:               state.Error,
	}
	if state.StartedAt > 0 {
		started := time.Unix(state.StartedAt, 0).UTC()
		out.StartedAt = &started
	}
	if state.FinishedAt > 0 {
		computed := time.Unix(state.FinishedAt, 0).UTC()
		out.ComputedAt = &computed
	}
	return out
}

func (s *Server) handleRetiredValidation(w http.ResponseWriter, r *http.Request) {
	writePublicError(w, http.StatusGone, PublicErrorValidationAPIRetired, errors.New("validation result API retired; use GET /api/v2/validate for snapshot metadata and /api/v1/validation/diagnostics for generation-bound issue pages"))
}
