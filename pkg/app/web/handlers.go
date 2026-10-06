package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"
)

func (s *Server) handleSuggest(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := parseInt(r.URL.Query().Get("limit"), 25)
	matches := s.catalog.Suggest(q, limit)
	writeJSON(w, http.StatusOK, SuggestResponse{Query: q, Matches: matches})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	seed := strings.TrimSpace(r.URL.Query().Get("seed"))
	intent := strings.TrimSpace(r.URL.Query().Get("intent"))
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	pathPrefix := strings.TrimSpace(r.URL.Query().Get("pathPrefix"))
	if pathPrefix == "" {
		pathPrefix = strings.TrimSpace(r.URL.Query().Get("folder"))
	}
	noteType := strings.TrimSpace(r.URL.Query().Get("noteType"))
	continuationToken := strings.TrimSpace(r.URL.Query().Get("continuationToken"))
	limit := parseInt(r.URL.Query().Get("limit"), 25)
	budget := parseInt(r.URL.Query().Get("budget"), 120000)

	cfg := s.semanticConfig()
	seeds := []string{}
	if seed != "" {
		seeds = append(seeds, seed)
	}

	resp, err := agentapi.SemanticQueryUnifiedWithOptions(ctx, cfg, agentapi.SemanticQueryOptions{
		Profile:           searchapplication.ProfileInteractive,
		Query:             q,
		SeedTokens:        seeds,
		Mode:              intent,
		Limit:             limit,
		BudgetChars:       budget,
		Scope:             scope,
		PathPrefix:        pathPrefix,
		NoteType:          noteType,
		ContinuationToken: continuationToken,
	})
	if err != nil {
		switch {
		case errors.Is(err, unifiedsearch.ErrCursorStale), errors.Is(err, unifiedsearch.ErrCursorRefreshRequired):
			writeJSON(w, http.StatusConflict, ErrorResponse{
				Error: strings.TrimSpace(strings.TrimPrefix(err.Error(), "continuation_stale:")),
				Code:  "CONTINUATION_STALE",
			})
		default:
			writeError(w, http.StatusBadRequest, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, mapSearchResponse(resp))
}

func (s *Server) handleGraphLocal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limit := parseInt(r.URL.Query().Get("limit"), 200)
	depth := parseInt(r.URL.Query().Get("depth"), 1)
	notesOnly := parseBool(r.URL.Query().Get("notesOnly"), false)
	diagnostics := parseBool(r.URL.Query().Get("diagnostics"), false) || parseBool(r.URL.Query().Get("debug"), false)
	var (
		resp GraphResponse
		err  error
	)
	if strings.TrimSpace(r.URL.Query().Get("ref")) != "" {
		ref, refErr := nodeRefFromRequest(r.URL.Query())
		if refErr != nil {
			writeError(w, http.StatusBadRequest, refErr)
			return
		}
		resp, err = s.buildLocalGraphRefMode(ctx, ref, limit, depth, notesOnly, diagnostics)
	} else {
		path := strings.TrimSpace(r.URL.Query().Get("path"))
		if path == "" {
			writeError(w, http.StatusBadRequest, errMissing("path or ref"))
			return
		}
		resp, err = s.buildLocalGraphMode(ctx, path, limit, depth, notesOnly, diagnostics)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGraphGlobal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limit := parseInt(r.URL.Query().Get("limit"), 2000)
	depth := parseInt(r.URL.Query().Get("depth"), 2)
	notesOnly := parseBool(r.URL.Query().Get("notesOnly"), false)
	diagnostics := parseBool(r.URL.Query().Get("diagnostics"), false) || parseBool(r.URL.Query().Get("debug"), false)
	resp, err := s.buildGlobalGraphMode(ctx, limit, depth, notesOnly, diagnostics)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGraphExpand(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	module := strings.TrimSpace(r.URL.Query().Get("module"))
	if module == "" {
		writeError(w, http.StatusBadRequest, errMissing("module"))
		return
	}
	limit := parseInt(r.URL.Query().Get("limit"), 600)
	diagnostics := parseBool(r.URL.Query().Get("diagnostics"), false) || parseBool(r.URL.Query().Get("debug"), false)
	resp, err := s.buildModuleGraphMode(ctx, module, limit, diagnostics)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	limit := parseInt(r.URL.Query().Get("limit"), 200)
	resp, err := s.listTree(ctx, path, limit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleFileView(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		writeError(w, http.StatusBadRequest, errMissing("path"))
		return
	}
	resp, err := s.readFileView(ctx, path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRenderedFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		writeError(w, http.StatusBadRequest, errMissing("path"))
		return
	}
	resp, err := s.readRenderedNote(ctx, path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleOntologySummary(w http.ResponseWriter, r *http.Request) {
	resp, err := s.ontologySummary(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleOntologyTypes(w http.ResponseWriter, r *http.Request) {
	resp, err := s.ontologySummary(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, resp.Types)
}

func (s *Server) handleOntologyTypeByName(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	typeName, err := parseOntologyTypeNameFromPath(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	overlay, ok := s.readOverlayForStagedRead(w, r)
	if !ok {
		return
	}
	read := s.ontologyType
	if r.URL.Query().Get("notes") == "none" {
		read = s.ontologyTypeDoc
	}
	var resp OntologyTypeResponse
	if raw, present := r.URL.Query()["limit"]; present {
		limit, parseErr := strconv.Atoi(raw[0])
		if parseErr != nil || limit < 1 || limit > 500 || typeName != pseudoTypeAll || r.Method != http.MethodGet {
			writeError(w, http.StatusBadRequest, errors.New("limit must be 1..500 on GET types/__all__"))
			return
		}
		if r.URL.Query().Get("notes") == "none" {
			resp, err = read(r.Context(), typeName, overlay)
		} else {
			resp, err = s.ontologyRecentNotes(r.Context(), limit)
		}
	} else {
		resp, err = read(r.Context(), typeName, overlay)
	}
	if err != nil {
		if errors.Is(err, errIndexInitializing) {
			w.Header().Set("Retry-After", "5")
			writePublicError(w, http.StatusServiceUnavailable, PublicErrorIndexInitializing, errIndexInitializing)
			return
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleOntologyAtlas(w http.ResponseWriter, r *http.Request) {
	resp, err := s.ontologyAtlas(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// parseOntologyTypeNameFromPath extracts the type name from ontology type
// routes, decoding any percent-encoding the client used.
func parseOntologyTypeNameFromPath(urlPath string) (string, error) {
	clean := path.Clean(urlPath)
	const prefix = "/api/v1/ontology/types/"
	rest := ""
	if strings.HasPrefix(clean, prefix) {
		rest = strings.TrimPrefix(clean, prefix)
	}
	if rest == "" || rest == "." {
		return "", errMissing("name")
	}
	if strings.Contains(rest, "/") {
		return "", errors.New("invalid ontology type route")
	}
	decoded, err := url.PathUnescape(rest)
	if err != nil {
		return "", err
	}
	decoded = strings.TrimSpace(decoded)
	if decoded == "" {
		return "", errMissing("name")
	}
	return decoded, nil
}

func (s *Server) handleOntologyInspect(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		writeError(w, http.StatusBadRequest, errMissing("path"))
		return
	}
	resp, err := s.ontologyInspect(r.Context(), path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleOntologyQuerySchema(w http.ResponseWriter, r *http.Request) {
	resp, err := s.querySchema()
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleNoteSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeError(w, http.StatusBadRequest, errMissing("q"))
		return
	}
	resp, err := s.searchNotes(
		r.Context(),
		query,
		r.URL.Query().Get("type"),
		r.URL.Query().Get("pathPrefix"),
		r.URL.Query().Get("tag"),
		parseInt(r.URL.Query().Get("limit"), 25),
		parseInt(r.URL.Query().Get("offset"), 0),
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) status(ctx context.Context) StatusResponse {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()
	resp := s.lastReady
	noteIndex, noteProvider := s.runtime.NoteEmbeddings()
	codeIndex, codeProvider := s.runtime.CodeEmbeddings()
	intelStore := s.runtime.Intel()
	resp.Embeddings = noteIndex != nil && noteProvider != nil
	resp.CodeIndex = intelStore != nil || (codeIndex != nil && codeProvider != nil)
	resp.Validation = validationStatusInfoFromEnvelope(s.readCachedValidationEnvelope(ctx))
	if resp.Embeddings {
		resp.ReadyReason = ""
	} else {
		resp.ReadyReason = "embeddings disabled"
	}
	resp.IndexState = indexReadyState(s.runtime.IndexReady())
	resp.Live = s.runtime.LiveHealth()
	resp.UpdatedAt = time.Now()
	return resp
}

func validationStatusInfoFromEnvelope(env ValidationEnvelope) ValidationStatusInfo {
	return ValidationStatusInfo{
		Status:              env.Status,
		Health:              env.Health,
		Generation:          env.Generation,
		PublishedGeneration: env.PublishedGeneration,
		ComputedAt:          env.ComputedAt,
		DurationMs:          env.DurationMs,
		Error:               env.Error,
	}
}

func (s *Server) semanticConfig() agentapi.Config {
	noteIndex, noteProvider := s.runtime.NoteEmbeddings()
	codeIndex, codeProvider := s.runtime.CodeEmbeddings()
	return agentapi.Config{
		Vault:             s.cfg.Vault,
		VaultPath:         s.cfg.VaultPath,
		VaultDef:          s.cfg.VaultDef,
		NoteMetadata:      s.noteMetadata,
		Cache:             s.cfg.Cache,
		Embeddings:        noteIndex,
		EmbedProvider:     noteProvider,
		EmbeddingsOn:      noteIndex != nil && noteProvider != nil,
		CodeEmbeddings:    codeIndex,
		CodeEmbedProvider: codeProvider,
		CodeEmbeddingsOn:  codeIndex != nil && codeProvider != nil,
		IntelStore:        s.runtime.Intel(),
		SessionStore:      s.runtime.Intel(),
		Runtime:           s.runtime.CapabilityView(),
	}
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, ErrorResponse{Error: err.Error(), Code: publicErrorCodeForStatus(status)})
}

func parseInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func parseBool(raw string, fallback bool) bool {
	if raw == "" {
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	case "0", "false", "f", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func errMissing(field string) error {
	return errors.New("missing required parameter: " + field)
}

func mapSearchResponse(resp agentapi.SemanticQueryResponse) SearchResponse {
	matches := make([]SearchMatch, 0, len(resp.Matches))
	for _, m := range resp.Matches {
		snippet := strings.TrimSpace(m.Preview)
		snippetStatus := "unavailable"
		if snippet != "" &&
			!strings.EqualFold(snippet, strings.TrimSpace(m.Heading)) &&
			!strings.EqualFold(snippet, strings.TrimSpace(m.Title)) &&
			m.ContentKind != "stub" {
			snippetStatus = "available"
		} else {
			snippet = ""
		}
		matches = append(matches, SearchMatch{
			Type:             m.Type,
			Path:             m.Path,
			Title:            m.Title,
			Symbol:           m.Symbol,
			FQN:              m.FQN,
			AnchorID:         m.AnchorID,
			NodeID:           m.NodeID,
			NodeRefJSON:      m.NodeRefJSON,
			SourceLocator:    m.SourceLocator,
			NodeKind:         m.NodeKind,
			ParentNodeID:     m.ParentNodeID,
			NodeRef:          m.NodeRef,
			LinkTarget:       m.LinkTarget,
			Kind:             m.Kind,
			ChunkIndex:       m.ChunkIndex,
			StartLine:        m.StartLine,
			EndLine:          m.EndLine,
			Score:            m.Score,
			Heading:          m.Heading,
			Snippet:          snippet,
			SnippetKind:      m.ContentKind,
			SnippetStatus:    snippetStatus,
			SnippetTruncated: m.ContentTruncated,
			NoteType:         m.NoteType,
		})
	}

	return SearchResponse{
		Profile:           resp.Profile,
		Policy:            semanticResponsePolicy(resp.Policy),
		Query:             resp.Query,
		Returned:          resp.Returned,
		Total:             resp.Total,
		Count:             resp.Count,
		ContinuationToken: resp.ContinuationToken,
		Text:              resp.Text,
		Matches:           matches,
		Warnings:          resp.Warnings,
		Lanes:             resp.Lanes,
		TargetStatus:      resp.TargetStatus,
		Confidence:        resp.Confidence,
		Coverage:          resp.Coverage,
	}
}

func semanticResponsePolicy(policy *searchapplication.EffectivePolicy) searchapplication.EffectivePolicy {
	if policy == nil {
		return searchapplication.EffectivePolicy{}
	}
	return *policy
}
