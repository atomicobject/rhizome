package web

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func (s *Server) handlePublicGraphQLSchema(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	resp, err := s.querySchema()
	if err != nil {
		writePublicError(w, http.StatusServiceUnavailable, PublicErrorSchemaUnavailable, err)
		return
	}
	if !resp.SchemaPresent || !resp.QuerySchemaPresent || strings.TrimSpace(resp.SDL) == "" {
		writePublicError(w, http.StatusServiceUnavailable, PublicErrorSchemaUnavailable, errors.New("ontology query schema is unavailable"))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, resp.SDL)
}

func (s *Server) handlePublicGraphQL(w http.ResponseWriter, r *http.Request) {
	trace := os.Getenv("RZM_TRACE_GRAPHQL") != ""
	start := time.Now()
	last := start
	traceStep := func(label string) {
		if !trace {
			return
		}
		now := time.Now()
		log.Printf("graphql trace step=%s delta=%s total=%s", label, now.Sub(last), now.Sub(start))
		last = now
	}
	if r.Method != http.MethodPost {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	body, err := io.ReadAll(r.Body)
	traceStep("read-body")
	if err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	defer r.Body.Close()

	var req OntologyQueryRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	req.Query = strings.TrimSpace(req.Query)
	traceStep("decode")
	if req.Query == "" {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, errMissing("query"))
		return
	}

	overlay, err := s.readOverlayForEditSession(r.Context(), req.EditSession)
	if err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	traceStep("overlay")
	resp, err := s.executeOntologyQueryOperation(r.Context(), req.Query, req.Variables, req.OperationName, overlay)
	traceStep("execute")
	if err != nil {
		if errors.Is(err, errIndexInitializing) {
			w.Header().Set("Retry-After", "5")
			writePublicError(w, http.StatusServiceUnavailable, PublicErrorIndexInitializing, errIndexInitializing)
			return
		}
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	for i := range resp.Errors {
		if resp.Errors[i].Extensions == nil {
			resp.Errors[i].Extensions = map[string]any{"code": PublicErrorGraphQLValidation}
		}
	}
	traceStep("errors")
	writeJSON(w, http.StatusOK, resp)
	traceStep("write")
}
