package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/version"
)

const (
	publicAPIVersion = "v1"

	PublicErrorBadRequest           = "BAD_REQUEST"
	PublicErrorGraphQLValidation    = "GRAPHQL_VALIDATION_FAILED"
	PublicErrorMethodNotAllowed     = "METHOD_NOT_ALLOWED"
	PublicErrorNotFound             = "NOT_FOUND"
	PublicErrorConflict             = "CONFLICT"
	PublicErrorRecipeCompileFailed  = "QUERY_RECIPE_COMPILE_FAILED"
	PublicErrorSchemaUnavailable    = "SCHEMA_UNAVAILABLE"
	PublicErrorStreamingUnsupported = "STREAMING_UNSUPPORTED"
	// PublicErrorIndexInitializing identifies a retryable 503 from an
	// index-dependent read while first-launch indexing is still in progress.
	PublicErrorIndexInitializing    = "INDEX_INITIALIZING"
	PublicErrorInternal             = "INTERNAL"
	PublicErrorValidationPageLimit  = "VALIDATION_PAGE_INVALID_LIMIT"
	PublicErrorValidationPageCursor = "VALIDATION_PAGE_INVALID_CURSOR"
	PublicErrorValidationPageFilter = "VALIDATION_PAGE_INVALID_FILTER"
	PublicErrorValidationPageSort   = "VALIDATION_PAGE_INVALID_SORT"
	PublicErrorValidationGeneration = "VALIDATION_GENERATION_EXPIRED"
	PublicErrorValidationScope      = "VALIDATION_SCOPE_INVALID"
	PublicErrorValidationAPIRetired = "VALIDATION_API_RETIRED"

	PublicNodeEventUpdated = "node.updated"
	PublicNodeEventStale   = "node.stale"
	PublicNodeEventDeleted = "node.deleted"
)

func (s *Server) handlePublicStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	writeJSON(w, http.StatusOK, s.status(r.Context()))
}

func (s *Server) handlePublicCapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	resp, err := s.publicCapabilities(r.Context())
	if err != nil {
		writePublicError(w, http.StatusInternalServerError, PublicErrorInternal, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handlePublicValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	writeJSON(w, http.StatusOK, s.readCachedValidationEnvelope(r.Context()))
}

func (s *Server) handlePublicQueryRecipes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
	default:
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	clean := path.Clean(r.URL.Path)
	if clean == "/api/v1/query-recipes" {
		recipes, issues := queryrecipe.LoadDefaultSources(s.cfg.VaultPath)
		writeJSON(w, http.StatusOK, PublicQueryRecipeListResponse{Recipes: recipes, Issues: issues})
		return
	}
	recipeID, action, err := parsePublicQueryRecipePath(clean)
	if err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	if action != "" {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	recipe, found, _ := s.publicQueryRecipeByID(recipeID)
	if !found {
		writePublicError(w, http.StatusNotFound, PublicErrorNotFound, fmt.Errorf("query recipe %q not found", recipeID))
		return
	}
	writeJSON(w, http.StatusOK, recipe)
}

func (s *Server) handlePublicQueryRecipeExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	recipeID, action, err := parsePublicQueryRecipePath(path.Clean(r.URL.Path))
	if err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	if action != "execute" {
		writePublicError(w, http.StatusNotFound, PublicErrorNotFound, errors.New("query recipe action not found"))
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	defer r.Body.Close()
	var req PublicQueryRecipeExecuteRequest
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
			return
		}
	}
	recipe, found, loadIssues := s.publicQueryRecipeByID(recipeID)
	if !found {
		writePublicError(w, http.StatusNotFound, PublicErrorNotFound, fmt.Errorf("query recipe %q not found", recipeID))
		return
	}
	if len(loadIssues) > 0 {
		writePublicErrorDetails(w, http.StatusBadRequest, PublicErrorRecipeCompileFailed, errors.New("query recipe failed to load"), map[string]any{"issues": loadIssues})
		return
	}
	result, issues, err := s.executePublicQueryRecipe(r.Context(), recipe, req.Inputs)
	if err != nil {
		if errors.Is(err, errIndexInitializing) {
			w.Header().Set("Retry-After", "5")
			writePublicError(w, http.StatusServiceUnavailable, PublicErrorIndexInitializing, errIndexInitializing)
			return
		}
		writePublicError(w, http.StatusServiceUnavailable, PublicErrorSchemaUnavailable, err)
		return
	}
	if len(issues) > 0 {
		writePublicErrorDetails(w, http.StatusBadRequest, PublicErrorRecipeCompileFailed, errors.New("query recipe failed to compile"), map[string]any{"issues": issues})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handlePublicEditSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	defer r.Body.Close()

	var req OntologyEditSessionCreateRequest
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
			return
		}
	}

	// WHY: public v1 edit sessions intentionally delegate to the existing
	// ontology edit-session service so browser and external clients exercise
	// the same rebase/conflict/write behavior while route names migrate.
	resp, err := s.createOntologyEditSessionResponse(r.Context(), req)
	if err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handlePublicEditSessionByID(w http.ResponseWriter, r *http.Request) {
	sessionID, action, err := parsePublicEditSessionPath(path.Clean(r.URL.Path))
	if err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	switch {
	case action == "" && r.Method == http.MethodGet:
		resp, getErr := s.getOntologyEditSessionResponse(r.Context(), sessionID)
		if getErr != nil {
			if strings.Contains(getErr.Error(), "not found") {
				writePublicError(w, http.StatusNotFound, PublicErrorNotFound, getErr)
				return
			}
			writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, getErr)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	case action == "" && r.Method == http.MethodDelete:
		if !s.deleteOntologyEditSession(sessionID) {
			writePublicError(w, http.StatusNotFound, PublicErrorNotFound, errors.New("edit session not found"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
	case action == "stage" && r.Method == http.MethodPost:
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, readErr)
			return
		}
		defer r.Body.Close()
		var req OntologyEditSessionStageRequest
		if err := json.Unmarshal(body, &req); err != nil {
			writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
			return
		}
		resp, stageErr := s.stageOntologyEditSessionResponse(r.Context(), sessionID, req)
		if stageErr != nil {
			writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, stageErr)
			return
		}
		if resp.Status == OntologyEditSessionStatusConflicted {
			writeJSON(w, http.StatusConflict, resp)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	case action == "preview" && r.Method == http.MethodPost:
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, readErr)
			return
		}
		defer r.Body.Close()
		var req OntologyEditSessionPreviewRequest
		if len(strings.TrimSpace(string(body))) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
				return
			}
		}
		resp, previewErr := s.previewOntologyEditSessionResponse(r.Context(), sessionID, req)
		if previewErr != nil {
			writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, previewErr)
			return
		}
		if resp.Status == OntologyEditSessionStatusConflicted {
			writeJSON(w, http.StatusConflict, resp)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	case action == "diff" && r.Method == http.MethodPost:
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, readErr)
			return
		}
		defer r.Body.Close()
		var req OntologyEditSessionPreviewRequest
		if len(strings.TrimSpace(string(body))) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
				return
			}
		}
		resp, diffErr := s.diffOntologyEditSessionResponse(r.Context(), sessionID, req.Snapshot)
		if diffErr != nil {
			writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, diffErr)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	case action == "commit" && r.Method == http.MethodPost:
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, readErr)
			return
		}
		defer r.Body.Close()
		var req OntologyEditSessionCommitRequest
		if len(strings.TrimSpace(string(body))) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
				return
			}
		}
		resp, commitErr := s.commitOntologyEditSessionResponse(r.Context(), sessionID, req)
		if commitErr != nil {
			writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, commitErr)
			return
		}
		if resp.Status == OntologyEditSessionStatusConflicted {
			writeJSON(w, http.StatusConflict, resp)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	default:
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
	}
}

func (s *Server) handleOpenAPIYAML(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	if len(embeddedOpenAPIYAML) == 0 {
		writePublicError(w, http.StatusNotFound, PublicErrorNotFound, errors.New("openapi document not found"))
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(embeddedOpenAPIYAML)
}

func (s *Server) publicCapabilities(ctx context.Context) (PublicCapabilitiesResponse, error) {
	status := s.status(ctx)
	querySchema, queryErr := s.querySchema()
	recipes, recipeIssues := queryrecipe.LoadDefaultSources(s.cfg.VaultPath)
	viewCatalog, viewErr := s.publicViewsCatalog(ctx)
	noteIndex, noteProvider := s.runtime.NoteEmbeddings()
	codeIndex, codeProvider := s.runtime.CodeEmbeddings()
	intelStore := s.runtime.Intel()
	degraded := []string{}
	if queryErr != nil || !querySchema.QuerySchemaPresent {
		degraded = append(degraded, "graphql schema unavailable")
	}
	if !status.Embeddings {
		degraded = append(degraded, "embeddings unavailable")
	}
	if !status.CodeIndex {
		degraded = append(degraded, "code index unavailable")
	}
	if len(recipeIssues) > 0 {
		degraded = append(degraded, "query recipes have issues")
	}
	if viewErr != nil {
		degraded = append(degraded, "views unavailable")
	} else if len(viewCatalog.Issues) > 0 {
		degraded = append(degraded, "views have issues")
	}
	generation := publicGeneration(status.UpdatedAt)
	validationGeneration := ""
	if status.Validation.Generation > 0 {
		validationGeneration = fmt.Sprintf("%d", status.Validation.Generation)
	}
	viewGeneration := generation
	if viewErr == nil {
		viewGeneration = configuredViewsGeneration(viewCatalog)
	}
	resp := PublicCapabilitiesResponse{
		RhizomeVersion: version.Version,
		APIVersion:     publicAPIVersion,
		Vault: PublicVaultInfo{
			Name: s.cfg.VaultDef.Name,
			Path: s.cfg.VaultPath,
		},
		Status: status,
		GraphQL: PublicGraphQLInfo{
			Available:      queryErr == nil && querySchema.QuerySchemaPresent,
			Endpoint:       "/api/v1/graphql",
			SchemaEndpoint: "/api/v1/graphql/schema",
			ExplorerURL:    "/graphql",
			SchemaHash:     hashString(querySchema.SDL),
			OntologyHash:   querySchema.SchemaHash,
			Introspection:  true,
			Operations:     []string{"query"},
		},
		REST: PublicRESTInfo{
			BasePath: "/api/v1",
			OpenAPI:  "/openapi.yaml",
			Resources: []string{
				"capabilities",
				"status",
				"validate",
				"validation-repair-reviews",
				"events",
				"nodes",
				"files",
				"search",
				"graphs",
				"ontology",
				"display-groups",
				"graphql",
				"query-recipes",
				"views",
				"edit-sessions",
			},
		},
		Events: PublicEventsInfo{
			Endpoint:     "/api/v1/events",
			NodeEndpoint: "/api/v1/nodes/events",
			Format:       "text/event-stream",
			Replay:       "not-yet-supported",
			Heartbeat:    "comment heartbeat; clients should treat missed heartbeats as a refetch trigger",
			Reconnect:    "refetch capabilities/status/schema/active views after reconnect",
			Invalidations: []string{
				GlobalEventNodeChanged,
				GlobalEventValidationInvalidated,
				GlobalEventSchemaInvalidated,
				GlobalEventQueryRecipeInvalidated,
				GlobalEventIndexInvalidated,
				GlobalEventCapabilitiesInvalidated,
				GlobalEventEditSessionInvalidated,
				GlobalEventViewsChanged,
			},
			StreamedKinds: []string{
				GlobalEventValidateChanged,
				GlobalEventIndexChanged,
				GlobalEventNodeChanged,
				GlobalEventValidationInvalidated,
				GlobalEventSchemaInvalidated,
				GlobalEventQueryRecipeInvalidated,
				GlobalEventIndexInvalidated,
				GlobalEventCapabilitiesInvalidated,
				GlobalEventEditSessionInvalidated,
				GlobalEventViewsChanged,
			},
			NodeStreamedKinds: []string{
				PublicNodeEventUpdated,
				PublicNodeEventStale,
				PublicNodeEventDeleted,
			},
			SupportedKinds: []string{
				GlobalEventValidateChanged,
				GlobalEventIndexChanged,
				GlobalEventNodeChanged,
				GlobalEventValidationInvalidated,
				GlobalEventSchemaInvalidated,
				GlobalEventQueryRecipeInvalidated,
				GlobalEventIndexInvalidated,
				GlobalEventCapabilitiesInvalidated,
				GlobalEventEditSessionInvalidated,
				GlobalEventViewsChanged,
				PublicNodeEventUpdated,
				PublicNodeEventStale,
				PublicNodeEventDeleted,
			},
		},
		QueryRecipes: PublicQueryRecipeInfo{
			Available:  len(recipes) > 0 || len(recipeIssues) == 0,
			Generation: generation,
			Count:      len(recipes),
			Issues:     len(recipeIssues),
			Endpoints: []string{
				"/api/v1/query-recipes",
				"/api/v1/query-recipes/{id}",
				"/api/v1/query-recipes/{id}/execute",
			},
		},
		Views: PublicViewsInfo{
			Available:  viewErr == nil,
			Generation: viewGeneration,
			Count:      len(viewCatalog.Views),
			Issues:     len(viewCatalog.Issues),
			Endpoints: []string{
				"/api/v1/views",
				"/api/v1/views/{id}",
				"/api/v1/views/{id}/execute",
			},
		},
		Validation: PublicValidationInfo{
			Available:       intelStore != nil,
			Endpoint:        "/api/v2/validate",
			Endpoints:       []string{"/api/v2/validate", "/api/v2/validate/refresh", "/api/v1/validation/diagnostics", "/api/v1/validation/summaries", "/api/v1/validation/groups"},
			RepairAvailable: s.validationRepairAuthority() != nil,
			RepairEndpoints: []string{
				"/api/v1/validation/repair-reviews",
				"/api/v1/validation/repair-reviews/{id}",
				"/api/v1/validation/repair-reviews/{id}/apply",
			},
			Generation: validationGeneration,
			Status:     status.Validation.Status,
		},
		Index: PublicIndexInfo{
			Available:  status.CodeIndex,
			Generation: generation,
		},
		Providers: PublicProviderInfo{
			Embeddings: publicProviderState(noteIndex != nil && noteProvider != nil),
			CodeIndex:  publicProviderState(intelStore != nil || (codeIndex != nil && codeProvider != nil)),
		},
		EditSessions: PublicEditSessionsInfo{
			Available: true,
			Endpoints: []string{
				"/api/v1/edit-sessions",
				"/api/v1/edit-sessions/{id}",
				"/api/v1/edit-sessions/{id}/stage",
				"/api/v1/edit-sessions/{id}/preview",
				"/api/v1/edit-sessions/{id}/diff",
				"/api/v1/edit-sessions/{id}/commit",
			},
		},
		Errors: PublicErrorsInfo{
			Format: "application/json { error, code } and GraphQL errors[].extensions.code",
			Codes: []string{
				PublicErrorBadRequest,
				PublicErrorGraphQLValidation,
				PublicErrorMethodNotAllowed,
				PublicErrorNotFound,
				PublicErrorRecipeCompileFailed,
				PublicErrorSchemaUnavailable,
				PublicErrorStreamingUnsupported,
				PublicErrorIndexInitializing,
				PublicErrorValidationPageLimit,
				PublicErrorValidationPageCursor,
				PublicErrorValidationPageFilter,
				PublicErrorValidationPageSort,
				PublicErrorValidationGeneration,
				PublicErrorValidationScope,
				PublicErrorValidationAPIRetired,
				PublicErrorRepairApplyFailed,
				PublicErrorRepairWrongVault,
				strings.ToUpper(string(validate.RepairReviewErrorNotFound)),
				strings.ToUpper(string(validate.RepairReviewErrorActionNotFound)),
				strings.ToUpper(string(validate.RepairReviewErrorActionUnavailable)),
				strings.ToUpper(string(validate.RepairReviewErrorConfirmationRequired)),
				strings.ToUpper(string(validate.RepairReviewErrorTransactionIncomplete)),
				strings.ToUpper(string(validate.RepairReviewErrorPlanFingerprintMismatch)),
				strings.ToUpper(string(validate.RepairReviewErrorSelectionFingerprintMismatch)),
				strings.ToUpper(string(validate.RepairReviewErrorGenerationMismatch)),
				strings.ToUpper(string(validate.RepairReviewErrorManualOverlap)),
				strings.ToUpper(string(validate.RepairReviewErrorExpired)),
				strings.ToUpper(string(validate.RepairReviewErrorApplying)),
				strings.ToUpper(string(validate.RepairReviewErrorCapacityFull)),
				strings.ToUpper(string(validate.RepairReviewErrorRevalidationRequired)),
				strings.ToUpper(string(validate.RepairReviewErrorAuthorityUnavailable)),
				PublicErrorInternal,
			},
		},
		DegradedReasons:  degraded,
		GeneratedAt:      time.Now(),
		CompatibilityURL: "/docs/api/public-api.md",
	}
	if queryErr != nil {
		resp.GraphQL.SchemaHash = ""
	}
	return resp, nil
}

func publicProviderState(available bool) PublicProviderState {
	if available {
		return PublicProviderState{Available: true, State: "ready"}
	}
	return PublicProviderState{Available: false, State: "unavailable"}
}

func (s *Server) publicQueryRecipeByID(id string) (queryrecipe.Recipe, bool, []queryrecipe.Issue) {
	id = strings.TrimSpace(id)
	recipes, issues := queryrecipe.LoadDefaultSources(s.cfg.VaultPath)
	for _, recipe := range recipes {
		if recipe.ID == id {
			return recipe, true, issues
		}
	}
	return queryrecipe.Recipe{}, false, issues
}

func (s *Server) executePublicQueryRecipe(ctx context.Context, recipe queryrecipe.Recipe, inputs map[string]string) (queryrecipe.RunResult, []queryrecipe.Issue, error) {
	defs, err := s.ontologyDefinitions()
	if err != nil {
		return queryrecipe.RunResult{}, nil, err
	}
	if defs == nil || defs.exec == nil {
		return queryrecipe.RunResult{}, nil, errors.New("ontology query schema is unavailable")
	}
	compiled, issues := queryrecipe.Compile(recipe, defs.exec, inputs)
	if len(issues) > 0 {
		return queryrecipe.RunResult{}, issues, nil
	}
	deps, err := s.ontologyQueryDepsForPrepared(ctx, defs, compiled.Prepared)
	if err != nil {
		return queryrecipe.RunResult{}, nil, err
	}
	result := ontologyquery.ExecutePrepared(ctx, deps, defs.schema, defs.exec, compiled.Prepared)
	return queryrecipe.RunResult{
		Recipe:         recipe,
		Inputs:         inputs,
		Variables:      compiled.Variables,
		Query:          compiled.Query,
		OutputContract: recipe.OutputContract,
		Result:         result,
	}, nil, nil
}

func parsePublicQueryRecipePath(urlPath string) (recipeID string, action string, err error) {
	const prefix = "/api/v1/query-recipes/"
	if !strings.HasPrefix(urlPath, prefix) {
		return "", "", errors.New("invalid query recipe route")
	}
	rest := strings.Trim(strings.TrimPrefix(urlPath, prefix), "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return "", "", errMissing("id")
	}
	if len(parts) > 2 {
		return "", "", errors.New("invalid query recipe route")
	}
	if len(parts) == 2 {
		action = strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(parts[0]), action, nil
}

func parsePublicEditSessionPath(urlPath string) (sessionID string, action string, err error) {
	const prefix = "/api/v1/edit-sessions/"
	if !strings.HasPrefix(urlPath, prefix) {
		return "", "", errors.New("invalid edit session route")
	}
	rest := strings.Trim(strings.TrimPrefix(urlPath, prefix), "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return "", "", errMissing("id")
	}
	if len(parts) > 2 {
		return "", "", errors.New("invalid edit session route")
	}
	if len(parts) == 2 {
		action = strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(parts[0]), action, nil
}

func hashString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func publicGeneration(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(time.RFC3339Nano)
}

func writePublicError(w http.ResponseWriter, status int, code string, err error) {
	writePublicErrorDetails(w, status, code, err, nil)
}

func writePublicErrorDetails(w http.ResponseWriter, status int, code string, err error, details any) {
	if strings.TrimSpace(code) == "" {
		code = publicErrorCodeForStatus(status)
	}
	resp := ErrorResponse{Error: err.Error(), Code: code}
	if details != nil {
		resp.Details = details
	}
	writeJSON(w, status, resp)
}

func publicErrorCodeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return PublicErrorBadRequest
	case http.StatusMethodNotAllowed:
		return PublicErrorMethodNotAllowed
	case http.StatusNotFound:
		return PublicErrorNotFound
	case http.StatusServiceUnavailable:
		return PublicErrorSchemaUnavailable
	default:
		return PublicErrorInternal
	}
}
