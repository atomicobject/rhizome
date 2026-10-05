package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	appviews "github.com/atomicobject/rhizome/pkg/app/views"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

const maxPublicViewExecuteBodyBytes = 1 << 20

var errInvalidEditSessionRead = errors.New("invalid edit session read")

func (s *Server) handlePublicViews(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	clean := path.Clean(r.URL.Path)
	if clean == "/api/v1/views" {
		catalog, err := s.publicViewsCatalog(r.Context())
		if err != nil {
			writePublicError(w, http.StatusInternalServerError, PublicErrorInternal, err)
			return
		}
		writeJSON(w, http.StatusOK, catalog)
		return
	}
	viewID, action, err := parsePublicViewPath(clean)
	if err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	if action != "" {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	service, err := s.configuredViewsService(r.Context(), nil)
	if err != nil {
		writePublicError(w, http.StatusInternalServerError, PublicErrorInternal, err)
		return
	}
	entry, err := service.View(r.Context(), viewID)
	if err != nil {
		if errors.Is(err, appviews.ErrViewNotFound) {
			writePublicError(w, http.StatusNotFound, PublicErrorNotFound, err)
			return
		}
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) handlePublicViewFieldCandidates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	viewID, _, err := parsePublicViewPath(path.Clean(r.URL.Path))
	if err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	var service *appviews.Service
	if r.Method == http.MethodPost {
		overlay, ok := s.readOverlayForStagedRead(w, r)
		if !ok {
			return
		}
		service, err = s.configuredViewsService(r.Context(), overlay)
	} else {
		// GET keeps the older server-held session id form for existing clients.
		service, err = s.configuredViewsServiceForEditSession(r.Context(), &EditSessionReadRequest{SessionID: strings.TrimSpace(r.URL.Query().Get("editSessionId"))})
	}
	if err != nil {
		writePublicViewReadSetupError(w, err)
		return
	}
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 50)
	if limit > 200 {
		limit = 200
	}
	resp, err := service.FieldCandidates(r.Context(), viewID, appviews.FieldCandidatesRequest{
		Field: strings.TrimSpace(r.URL.Query().Get("field")),
		Query: strings.TrimSpace(r.URL.Query().Get("q")),
		Limit: limit,
	})
	if err != nil {
		writePublicViewExecutionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func parsePositiveInt(raw string, fallback int) int {
	var value int
	if _, err := fmt.Sscanf(strings.TrimSpace(raw), "%d", &value); err != nil || value <= 0 {
		return fallback
	}
	return value
}

func (s *Server) handlePublicViewExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	viewID, action, err := parsePublicViewPath(path.Clean(r.URL.Path))
	if err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	if action != "execute" {
		writePublicError(w, http.StatusNotFound, PublicErrorNotFound, errors.New("view action not found"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPublicViewExecuteBodyBytes)
	defer r.Body.Close()
	var req appviews.ExecuteRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		if !errors.Is(err, io.EOF) {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				writePublicError(w, http.StatusRequestEntityTooLarge, PublicErrorBadRequest, err)
				return
			}
			writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
			return
		}
	}
	overlayReq, err := publicViewEditSessionRequest(req.EditSession)
	if err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	service, err := s.configuredViewsServiceForEditSession(r.Context(), overlayReq)
	if err != nil {
		writePublicViewReadSetupError(w, err)
		return
	}
	resp, err := service.Execute(r.Context(), viewID, req)
	if err != nil {
		writePublicViewExecutionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handlePublicViewSave writes view state to .rhizome/views (SPEC-0112). The
// server-wide host and browser-origin guards cover it like other mutations.
func (s *Server) handlePublicViewSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	viewID, action, err := parsePublicViewPath(path.Clean(r.URL.Path))
	if err != nil || action != "save" {
		writePublicError(w, http.StatusNotFound, PublicErrorNotFound, errors.New("view action not found"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPublicViewExecuteBodyBytes)
	defer r.Body.Close()
	var req appviews.SaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	service, err := s.configuredViewsService(r.Context(), nil)
	if err != nil {
		writePublicError(w, http.StatusInternalServerError, PublicErrorInternal, err)
		return
	}
	resp, err := service.Save(r.Context(), viewID, req)
	if errors.Is(err, appviews.ErrSaveConflict) {
		writePublicError(w, http.StatusConflict, PublicErrorConflict, err)
		return
	}
	if err != nil {
		writePublicViewExecutionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) publicViewsCatalog(ctx context.Context) (appviews.Catalog, error) {
	service, err := s.configuredViewsService(ctx, nil)
	if err != nil {
		return appviews.Catalog{}, err
	}
	return service.Catalog(ctx)
}

func (s *Server) configuredViewsServiceForEditSession(ctx context.Context, req *EditSessionReadRequest) (*appviews.Service, error) {
	overlay, err := s.readOverlayForEditSession(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalidEditSessionRead, err)
	}
	return s.configuredViewsService(ctx, overlay)
}

func (s *Server) configuredViewsService(ctx context.Context, overlay *ontologyquery.ReadOverlay) (*appviews.Service, error) {
	_ = ctx
	defs, err := s.ontologyDefinitions()
	if err != nil {
		return nil, err
	}
	var schema *ontology.Schema
	var execSchema *ontologyquery.ExecutableSchema
	if defs != nil {
		schema = defs.schema
		execSchema = defs.exec
	}
	noteReader := s.noteReader
	if noteReader == nil {
		noteReader = &obsidian.Note{}
	}
	store := s.runtime.Intel()
	var ontologyService *ontology.Service
	if store != nil && schema != nil {
		ontologyService = ontology.NewService(s.cfg.VaultDef, noteReader, store, schema)
	}
	return appviews.New(appviews.ServiceOptions{
		VaultPath:   s.cfg.VaultPath,
		VaultDef:    s.cfg.VaultDef,
		NoteReader:  noteReader,
		Store:       store,
		Schema:      schema,
		ExecSchema:  execSchema,
		QueryDeps:   s.ontologyQueryDeps(ontologyService, noteReader),
		ReadOverlay: overlay,
		Bundled:     BundledViews(s.assets),
	}), nil
}

func publicViewEditSessionRequest(req *appviews.EditSessionRequest) (*EditSessionReadRequest, error) {
	if req == nil || req.IsZero() {
		return nil, nil
	}
	out := &EditSessionReadRequest{
		ID:        req.ID,
		SessionID: req.SessionID,
	}
	if len(req.Snapshot) > 0 {
		var snapshot OntologyEditSessionSnapshot
		if err := json.Unmarshal(req.Snapshot, &snapshot); err != nil {
			return nil, err
		}
		out.Snapshot = &snapshot
	}
	return out, nil
}

func writePublicViewExecutionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, appviews.ErrViewNotFound):
		writePublicError(w, http.StatusNotFound, PublicErrorNotFound, err)
	case errors.Is(err, appviews.ErrInvalidView), errors.Is(err, appviews.ErrUnsupportedVariant), errors.Is(err, appviews.ErrInvalidRequest):
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
	default:
		writePublicError(w, http.StatusInternalServerError, PublicErrorInternal, err)
	}
}

func writePublicViewReadSetupError(w http.ResponseWriter, err error) {
	if errors.Is(err, errInvalidEditSessionRead) {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	writePublicError(w, http.StatusInternalServerError, PublicErrorInternal, err)
}

func parsePublicViewPath(urlPath string) (viewID string, action string, err error) {
	const prefix = "/api/v1/views/"
	if !strings.HasPrefix(urlPath, prefix) {
		return "", "", errors.New("invalid view route")
	}
	rest := strings.Trim(strings.TrimPrefix(urlPath, prefix), "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return "", "", errMissing("id")
	}
	if len(parts) > 2 {
		return "", "", errors.New("invalid view route")
	}
	if len(parts) == 2 {
		action = strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(parts[0]), action, nil
}

func configuredViewsGeneration(catalog appviews.Catalog) string {
	encoded, err := json.Marshal(catalog)
	if err != nil {
		return hashString(fmt.Sprintf("%d:%d", len(catalog.Views), len(catalog.Issues)))
	}
	return hashString(string(encoded))
}
