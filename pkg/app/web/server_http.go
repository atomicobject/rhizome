package web

import (
	"context"
	"errors"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// Handler returns the HTTP handler for the server.
func (s *Server) Handler() http.Handler {
	return s.observeRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.serveHTMLViewerHost(w, r) {
			return
		}
		if s.isHTMLViewerHost(r.Host) {
			http.NotFound(w, r)
			return
		}
		if applicationHostDenied(r, s.cfg.ApplicationOrigin) {
			writeError(w, http.StatusForbidden, errors.New("request host is not allowed"))
			return
		}
		if browserMutationOriginDenied(r, s.cfg.ApplicationOrigin) {
			writeError(w, http.StatusForbidden, errors.New("browser request origin is not allowed"))
			return
		}
		// Browser and API traffic keeps a headless runtime alive: a UI attached
		// to one must not lose its backend to the idle timer mid-session.
		if control := s.cfg.RuntimeControl; control != nil && control.Touch != nil && strings.HasPrefix(r.URL.Path, "/api/") {
			control.Touch()
		}
		s.mux.ServeHTTP(w, r)
	}))
}

func applicationHostDenied(r *http.Request, configuredApplicationOrigin string) bool {
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return true
	}
	if isLoopbackHostname(requestHostname(host)) {
		return false
	}
	if configuredApplicationOrigin == "" {
		return true
	}
	configured, err := url.Parse(configuredApplicationOrigin)
	if err != nil || !validHTTPOrigin(configured) {
		return true
	}
	requestOrigin := &url.URL{Scheme: configured.Scheme, Host: host}
	return !sameWebOrigin(requestOrigin, configured)
}

func browserMutationOriginDenied(r *http.Request, configuredApplicationOrigin string) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	if origin == "null" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || !validHTTPOrigin(parsed) {
		return true
	}
	expected := &url.URL{Scheme: requestScheme(r), Host: r.Host}
	if configuredApplicationOrigin != "" {
		configured, parseErr := url.Parse(configuredApplicationOrigin)
		if parseErr != nil {
			return true
		}
		expected = configured
	}
	return !sameWebOrigin(parsed, expected)
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/api/v1/view-preferences", s.handleViewPreferences)
	s.mux.HandleFunc("/api/v1/view-preferences/reset", s.handleViewPreferencesReset)
	s.mux.HandleFunc("/api/v1/view-preferences/import", s.handleViewPreferencesImport)
	s.mux.HandleFunc("/api/v1/diagnostics/client", s.handleClientDiagnostics)
	s.registerRuntimeControlRoutes()
	// API endpoints
	s.mux.HandleFunc("/api/v1/status", s.handlePublicStatus)
	s.mux.HandleFunc("/api/v1/capabilities", s.handlePublicCapabilities)
	s.mux.HandleFunc("/api/v1/validate", s.handleRetiredValidation)
	s.mux.HandleFunc("/api/v2/validate", s.handlePublicValidate)
	s.mux.HandleFunc("/api/v2/validate/refresh", s.handlePublicValidationRefresh)
	s.mux.HandleFunc("/api/v1/validation/diagnostics", s.handlePublicValidationDiagnostics)
	s.mux.HandleFunc("/api/v1/validation/summaries", s.handlePublicValidationSummaries)
	s.mux.HandleFunc("/api/v1/validation/groups", s.handlePublicValidationGroups)
	s.mux.HandleFunc("/api/v1/validation/repair-reviews", s.handlePublicValidationRepairReviews)
	s.mux.HandleFunc("/api/v1/validation/repair-reviews/", s.handlePublicValidationRepairReviewByID)
	s.mux.HandleFunc("/api/v1/events", s.handleGlobalEvents)
	s.mux.HandleFunc("/api/v1/nodes/preview", s.handleNodePreview)
	s.mux.HandleFunc("/api/v1/nodes/events", s.handleNodeEvents)
	s.mux.HandleFunc("/api/v1/suggest", publicGET(s.handleSuggest))
	s.mux.HandleFunc("/api/v1/search", publicGET(s.handleSearch))
	s.mux.HandleFunc("/api/v1/search/notes", publicGET(s.handleNoteSearch))
	s.mux.HandleFunc("/api/v1/graphs/global", publicGET(s.requireIndexReady(s.handleGraphGlobal)))
	s.mux.HandleFunc("/api/v1/graphs/local", publicGET(s.requireIndexReady(s.handleGraphLocal)))
	s.mux.HandleFunc("/api/v1/graphs/expand", publicGET(s.requireIndexReady(s.handleGraphExpand)))
	s.mux.HandleFunc("/api/v1/files/tree", publicGET(s.handleTree))
	s.mux.HandleFunc("/api/v1/files/view", publicGET(s.handleFileView))
	s.mux.HandleFunc("/api/v1/files/rendered", publicGET(s.handleRenderedFile))
	s.mux.HandleFunc("/api/v1/ontology/shape", publicGET(s.requireIndexReady(s.handleOntologyShape)))
	s.mux.HandleFunc("/api/v1/ontology/summary", publicGET(s.requireIndexReady(s.handleOntologySummary)))
	s.mux.HandleFunc("/api/v1/display-groups", publicGET(s.requireIndexReady(s.handleDisplayGroups)))
	s.mux.HandleFunc("/api/v1/ontology/atlas", publicGET(s.requireIndexReady(s.handleOntologyAtlas)))
	s.mux.HandleFunc("/api/v1/ontology/types", publicGET(s.requireIndexReady(s.handleOntologyTypes)))
	s.mux.HandleFunc("/api/v1/ontology/types/", s.requireIndexReady(s.handleOntologyTypeByName))
	s.mux.HandleFunc("/api/v1/ontology/inspect", publicGET(s.requireIndexReady(s.handleOntologyInspect)))
	s.mux.HandleFunc("/api/v1/ontology/query-schema", publicGET(s.handleOntologyQuerySchema))
	s.mux.HandleFunc("/openapi.yaml", s.handleOpenAPIYAML)
	s.mux.HandleFunc("/api/v1/graphql/schema", s.handlePublicGraphQLSchema)
	s.mux.HandleFunc("/api/v1/graphql", s.handlePublicGraphQL)
	s.mux.HandleFunc("/api/v1/query-recipes", s.handlePublicQueryRecipes)
	s.mux.HandleFunc("/api/v1/query-recipes/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(path.Clean(r.URL.Path), "/execute") {
			s.handlePublicQueryRecipeExecute(w, r)
			return
		}
		s.handlePublicQueryRecipes(w, r)
	})
	s.mux.HandleFunc("/api/v1/views", s.handlePublicViews)
	s.mux.HandleFunc("/api/v1/views/", func(w http.ResponseWriter, r *http.Request) {
		clean := path.Clean(r.URL.Path)
		if strings.HasSuffix(clean, "/execute") {
			s.requireIndexReady(s.handlePublicViewExecute)(w, r)
			return
		}
		if strings.HasSuffix(clean, "/field-candidates") {
			s.requireIndexReady(s.handlePublicViewFieldCandidates)(w, r)
			return
		}
		if strings.HasSuffix(clean, "/save") {
			s.handlePublicViewSave(w, r)
			return
		}
		s.handlePublicViews(w, r)
	})
	s.mux.HandleFunc("/api/v1/edit-sessions", s.handlePublicEditSessions)
	s.mux.HandleFunc("/api/v1/edit-sessions/", s.handlePublicEditSessionByID)
	s.mux.HandleFunc("/views/", s.handleCustomViews)
	s.mux.HandleFunc("/api/v1/html-viewers", s.handleHTMLViewers)
	s.mux.HandleFunc("/api/v1/html-viewers/", s.handleHTMLViewerByID)
	s.mux.HandleFunc("/api/agent/settings", s.handleAgentSettings)
	s.mux.HandleFunc("/api/agent/sessions", s.handleAgentSessions)
	s.mux.HandleFunc("/api/agent/sessions/", s.handleAgentSessionByID)
	s.mux.HandleFunc("/api", rejectUnknownAPI)
	s.mux.HandleFunc("/api/", rejectUnknownAPI)

	// Static UI
	if s.assets != nil {
		root := http.FS(s.assets)
		s.mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" || r.URL.Path == "/index.html" {
				serveFile(w, r, root, "index.html")
				return
			}
			clean := path.Clean(r.URL.Path)
			if strings.HasPrefix(clean, "/") {
				clean = strings.TrimPrefix(clean, "/")
			}
			if shouldServeSPA(clean) {
				serveFile(w, r, root, "index.html")
				return
			}
			serveFile(w, r, root, clean)
		}))
	}
}

func rejectUnknownAPI(w http.ResponseWriter, r *http.Request) {
	writePublicError(w, http.StatusNotFound, PublicErrorNotFound, fmt.Errorf("api route %q not found", r.URL.Path))
}

func publicGET(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
			return
		}
		handler(w, r)
	}
}

func (s *Server) attachWatchHub(hub *watchhub.Hub) {
	if s == nil {
		return
	}
	s.watchHubMu.Lock()
	defer s.watchHubMu.Unlock()
	if s.closed {
		return
	}
	if s.watchHubUnsub != nil {
		s.watchHubUnsub()
		s.watchHubUnsub = nil
	}
	if hub == nil || s.nodeEvents == nil {
		return
	}
	s.watchHubUnsub = hub.Subscribe(
		"web-ontology-events",
		watchhub.Filter{
			IncludeFiles: true,
		},
		func(ctx context.Context, events []watchhub.WatchEvent) {
			s.handleWatchHubEvents(ctx, events)
		},
		func(ctx context.Context, ev watchhub.StaleEvent) {
			s.invalidateNoteCaches()
			s.invalidateNodeProjectionCache()
			s.nodeEvents.PublishStaleAll("filesystem", fmt.Sprintf("watchhub:%s", ev.Reason))
		},
	)
}

func shouldServeSPA(clean string) bool {
	if clean == "" || clean == "." {
		return true
	}
	base := path.Base(clean)
	return !strings.Contains(base, ".")
}
