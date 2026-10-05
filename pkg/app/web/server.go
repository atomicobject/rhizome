package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/agentchat"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/app/runtimeview"
	"github.com/atomicobject/rhizome/pkg/app/userstate"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/claude"
	"github.com/atomicobject/rhizome/pkg/harness/codex"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	codeemb "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
	codeembsqlite "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex/sqlite"
	embsqlite "github.com/atomicobject/rhizome/pkg/search/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
)

// Config configures the HTTP API server.
type Config struct {
	Vault     *obsidian.Vault
	VaultDef  obsidian.VaultDefinition
	VaultPath string
	Debug     bool
	Cache     *cache.Service
	Runtime   *Runtime
	// NoteMetadata is the explicit provider-aware raw-note metadata indexer
	// shared with ontology convergence for this server.
	NoteMetadata notemeta.Indexer
	// EditsRecovered is set by live startup after it has synchronously stabilized
	// edit and repair journals and before it starts cache or index workers.
	EditsRecovered bool
	// NotePathOwned optionally supplies the live ownership selector. Serve
	// provides it so config reloads govern durable exact-note reads.
	NotePathOwned func(string) bool
	// HTMLContentOriginTemplate is the separately-originated viewer base URL.
	// It must be an absolute HTTP(S) URL whose hostname contains {token}.
	// Empty uses http://{token}.localhost:<application-port> for loopback requests.
	HTMLContentOriginTemplate string
	// ApplicationOrigin allows an explicit external HTTP(S) application origin.
	// It can be configured independently of HTML viewing. Remote HTML viewing
	// requires HTTPS and a separate content origin. This value also fixes the
	// frame parent and browser mutation boundary behind a proxy.
	ApplicationOrigin string
	// RuntimeControl enables the loopback control API (SPEC-0104). Nil
	// leaves every control route answering 503 except health.
	RuntimeControl *RuntimeControl
}

// Server serves the API and bundled UI.
type Server struct {
	diagnostics         *diagnostics.Recorder
	cfg                 Config
	runtime             *Runtime
	cleanup             []func() error
	mux                 *http.ServeMux
	assets              fs.FS
	catalog             *FileCatalog
	noteReader          obsidian.NoteReader
	statusMu            sync.RWMutex
	lastReady           StatusResponse
	noteMu              sync.Mutex
	noteCache           *obsidian.NotePathCache
	noteLinksMu         sync.Mutex
	noteLinksByPath     map[string][]resolvedNoteLink
	noteLinksAt         time.Time
	anchorMu            sync.Mutex
	anchorSvc           *codeanchor.Service
	ontologyMu          sync.RWMutex
	ontologyDefs        *ontologyDefinitions
	summaryMu           sync.Mutex
	lastSummary         *OntologySummaryResponse
	editSessionsMu      sync.RWMutex
	editSessions        map[string]*ontologyEditSession
	graphCache          *graphResponseCache
	nodeProjectionCache *nodeProjectionCache
	noteMetadata        notemeta.Indexer
	graphScoresBackfill sync.Once
	nodeEvents          *nodeEventBroker
	globalEvents        *globalEventBroker
	agentService        *agentchat.Service
	userState           *userstate.Store
	userStateErr        error // Retains startup diagnostics without exposing raw errors in responses.
	watchHubMu          sync.Mutex
	closed              bool
	watchHubUnsub       func()
	htmlViewers         *htmlViewerRegistry
	viewModules         viewModuleCache
	viewWatch           viewFolderWatch
}

var agentHarnessOverride func() map[harness.Kind]harness.Harness

type resolvedNoteLink struct {
	Path     string
	Fragment string
	LinkType obsidian.BacklinkType
}

// Runtime holds dependencies for API handlers.
//
// The index-readiness fields implement the one-shot gate used by
// requireIndexReady to defer index-dependent reads during first launch and
// post-migration windows. See gate.go for the gate contract.
type Runtime struct {
	mu           sync.RWMutex
	Live         runtimeview.View
	NoteIndex    embeddings.Index
	NoteProvider embeddings.Provider
	CodeIndex    codeemb.Index
	CodeProvider embeddings.Provider
	IntelStore   *semdb.Store
	Ignore       *ignore.Matcher
	WatchHub     *watchhub.Hub
	ownsNote     bool
	ownsCode     bool
	ownsIntel    bool
	hubListeners []func(*watchhub.Hub)
	liveHealth   interface {
		LiveHealth() bootstrap.LiveHealth
	}
	indexReady                 chan struct{}
	indexReadyOnce             sync.Once
	noteReadReady              chan struct{}
	noteReadReadyOnce          sync.Once
	indexGated                 bool
	validationRepairAuthority  ValidationRepairAuthority
	repairPathCoordinator      *validate.RepairPathCoordinator
	validationRefreshRequester ValidationRefreshRequester
}

func (r *Runtime) NoteEmbeddings() (embeddings.Index, embeddings.Provider) {
	if r == nil {
		return nil, nil
	}
	if r.Live != nil {
		snapshot := r.Live.Snapshot()
		return snapshot.NoteIndex, snapshot.NoteProvider
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.NoteIndex, r.NoteProvider
}

func (r *Runtime) SetNoteEmbeddings(index embeddings.Index, provider embeddings.Provider) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.NoteIndex = index
	r.NoteProvider = provider
}

func (r *Runtime) CodeEmbeddings() (codeemb.Index, embeddings.Provider) {
	if r == nil {
		return nil, nil
	}
	if r.Live != nil {
		snapshot := r.Live.Snapshot()
		return snapshot.CodeIndex, snapshot.CodeProvider
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.CodeIndex, r.CodeProvider
}

func (r *Runtime) SetCodeEmbeddings(index codeemb.Index, provider embeddings.Provider) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.CodeIndex = index
	r.CodeProvider = provider
}

func (r *Runtime) Intel() *semdb.Store {
	if r == nil {
		return nil
	}
	if r.Live != nil {
		return r.Live.Snapshot().IntelStore
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.IntelStore
}

func (r *Runtime) SetIntelStore(store *semdb.Store) {
	if r == nil {
		return
	}
	var closeOld *semdb.Store
	r.mu.Lock()
	if r.ownsIntel && r.IntelStore != nil && r.IntelStore != store {
		closeOld = r.IntelStore
		r.ownsIntel = false
	}
	r.IntelStore = store
	r.mu.Unlock()
	if closeOld != nil {
		_ = closeOld.Close()
	}
}

func (r *Runtime) IgnoreMatcher() *ignore.Matcher {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.Ignore
}

func (r *Runtime) SetIgnoreMatcher(matcher *ignore.Matcher) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Ignore = matcher
}

func (r *Runtime) WatchHubRef() *watchhub.Hub {
	if r == nil {
		return nil
	}
	if r.Live != nil {
		return r.Live.Snapshot().WatchHub
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.WatchHub
}

func (r *Runtime) SetWatchHub(hub *watchhub.Hub) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.Live == nil {
		r.WatchHub = hub
	}
	listeners := append([]func(*watchhub.Hub){}, r.hubListeners...)
	r.mu.Unlock()
	for _, listener := range listeners {
		if listener != nil {
			listener(hub)
		}
	}
}

// CapabilityView returns the live view when this runtime is backed by
// LiveRuntime. Standalone/test runtimes continue to use their owned fields.
func (r *Runtime) CapabilityView() runtimeview.View {
	if r == nil {
		return nil
	}
	return r.Live
}

func (r *Runtime) OnWatchHubSet(listener func(*watchhub.Hub)) {
	if r == nil || listener == nil {
		return
	}
	r.mu.Lock()
	r.hubListeners = append(r.hubListeners, listener)
	hub := r.WatchHub
	if r.Live != nil {
		hub = r.Live.Snapshot().WatchHub
	}
	r.mu.Unlock()
	if hub != nil {
		listener(hub)
	}
}

func (r *Runtime) SetLiveHealthProvider(provider interface {
	LiveHealth() bootstrap.LiveHealth
}) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.liveHealth = provider
}

func (r *Runtime) LiveHealth() *bootstrap.LiveHealth {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	provider := r.liveHealth
	r.mu.RUnlock()
	if provider == nil {
		return nil
	}
	health := provider.LiveHealth()
	return &health
}

// NewServer constructs a server with initialized dependencies.
func NewServer(ctx context.Context, cfg Config, assets fs.FS) (*Server, error) {
	if cfg.Vault == nil {
		return nil, errors.New("vault is required")
	}
	if cfg.VaultPath == "" {
		return nil, errors.New("vault path is required")
	}
	if err := cfg.NoteMetadata.Validate(); err != nil {
		return nil, fmt.Errorf("note metadata indexer: %w", err)
	}
	if !cfg.EditsRecovered {
		if err := validate.StabilizePendingRepairJournals(validate.RunContext{VaultDef: cfg.VaultDef, VaultPath: cfg.VaultPath}); err != nil {
			return nil, fmt.Errorf("recover interrupted repair writes: %w", err)
		}
		if err := ontology.RecoverInterruptedEdits(cfg.VaultPath); err != nil {
			return nil, fmt.Errorf("recover interrupted note edits: %w", err)
		}
	}
	if err := validateHTMLViewerOrigins(cfg.HTMLContentOriginTemplate, cfg.ApplicationOrigin); err != nil {
		return nil, err
	}
	cfg.HTMLContentOriginTemplate = strings.TrimSpace(cfg.HTMLContentOriginTemplate)
	cfg.ApplicationOrigin = strings.TrimSpace(cfg.ApplicationOrigin)

	runtime, status, err := initRuntime(ctx, cfg)
	if err != nil {
		return nil, err
	}
	cleanup := ownedCleanup(runtime)
	ready := false
	defer func() {
		if !ready {
			for _, closeFn := range cleanup {
				_ = closeFn()
			}
		}
	}()

	noteReader := obsidian.NoteReader(&obsidian.Note{})
	if cfg.Cache != nil {
		noteReader = cache.NewNoteAdapter(cfg.Cache, &obsidian.Note{})
	}

	catalog, err := NewFileCatalog(cfg.VaultDef, runtime.IgnoreMatcher(), cfg.NoteMetadata)
	if err != nil {
		return nil, fmt.Errorf("file catalog: %w", err)
	}
	personalState, personalStateErr := userstate.Open(ctx, cfg.VaultPath)
	if personalStateErr == nil {
		cleanup = append(cleanup, personalState.Close)
	}

	s := &Server{
		diagnostics:         diagnostics.FromContext(ctx),
		cfg:                 cfg,
		runtime:             runtime,
		cleanup:             cleanup,
		mux:                 http.NewServeMux(),
		assets:              assets,
		catalog:             catalog,
		noteReader:          noteReader,
		editSessions:        map[string]*ontologyEditSession{},
		graphCache:          newGraphResponseCache(defaultScopedGraphCacheEntries),
		nodeProjectionCache: newNodeProjectionCache(defaultNodeProjectionCacheEntries),
		noteMetadata:        cfg.NoteMetadata,
		nodeEvents:          newNodeEventBroker(),
		globalEvents:        newGlobalEventBroker(),
		htmlViewers:         newHTMLViewerRegistry(),
		userState:           personalState,
		userStateErr:        personalStateErr,
	}
	agentHarnesses := map[harness.Kind]harness.Harness{
		harness.KindCodex:  codex.New(),
		harness.KindClaude: claude.New(),
	}
	if agentHarnessOverride != nil {
		agentHarnesses = agentHarnessOverride()
	}
	agentService, err := agentchat.NewService(ctx, cfg.VaultPath, agentHarnesses)
	if err != nil {
		return nil, err
	}
	s.agentService = agentService
	s.lastReady = status
	if runtime != nil {
		runtime.OnWatchHubSet(s.attachWatchHub)
	}
	s.registerRoutes()
	ready = true
	return s, nil
}

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

// Close releases runtime resources.
func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	// Drain detached graph builds before the stores they read from are closed.
	if s.graphCache != nil {
		s.graphCache.Close()
	}
	var errs []string
	for _, closeFn := range s.cleanup {
		if closeFn == nil {
			continue
		}
		if err := closeFn(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	s.watchHubMu.Lock()
	s.closed = true
	if s.watchHubUnsub != nil {
		s.watchHubUnsub()
		s.watchHubUnsub = nil
	}
	s.watchHubMu.Unlock()
	if s.nodeEvents != nil {
		s.nodeEvents.Close()
	}
	if s.globalEvents != nil {
		s.globalEvents.Close()
	}
	if s.agentService != nil {
		if err := s.agentService.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("close: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/api/v1/view-preferences", s.handleViewPreferences)
	s.mux.HandleFunc("/api/v1/view-preferences/reset", s.handleViewPreferencesReset)
	s.mux.HandleFunc("/api/v1/view-preferences/import", s.handleViewPreferencesImport)
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
			Extensions:   []string{".md"},
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

func (s *Server) codeAnchorService() *codeanchor.Service {
	intelStore := s.runtime.Intel()
	if intelStore == nil {
		return nil
	}
	s.anchorMu.Lock()
	defer s.anchorMu.Unlock()
	if s.anchorSvc != nil {
		return s.anchorSvc
	}
	s.anchorSvc = codeanchor.NewServiceWithOptions(
		intelStore,
		nil,
		codeanchor.WithBasePath(s.cfg.VaultPath),
		codeanchor.WithoutWarmCache(),
	)
	return s.anchorSvc
}

func initRuntime(ctx context.Context, cfg Config) (*Runtime, StatusResponse, error) {
	status := StatusResponse{
		VaultName: cfg.Vault.Name,
		VaultPath: cfg.VaultPath,
		UpdatedAt: time.Now(),
	}

	ignoreMatcher := ignore.LoadUnifiedMatcher(cfg.VaultPath, cfg.VaultDef.Excludes)
	rt := &Runtime{Ignore: ignoreMatcher}
	if cfg.Runtime != nil {
		rt = cfg.Runtime
		if rt.IgnoreMatcher() == nil {
			rt.SetIgnoreMatcher(ignoreMatcher)
		}
		noteIndex, noteProvider := rt.NoteEmbeddings()
		codeIndex, codeProvider := rt.CodeEmbeddings()
		intelStore := rt.Intel()
		status.Embeddings = noteIndex != nil && noteProvider != nil
		status.CodeIndex = intelStore != nil || (codeIndex != nil && codeProvider != nil)
		if status.CodeIndex || status.Embeddings {
			codeCfg, err := obsidian.LoadCodeConfig(cfg.VaultPath)
			if err == nil {
				status.IndexPath = obsidian.UnifiedIndexPath(cfg.VaultPath, codeCfg.IndexPath)
			}
		}
		if rt.Live == nil && intelStore == nil {
			codeCfg, err := obsidian.LoadCodeConfig(cfg.VaultPath)
			if err != nil {
				codeCfg = codeanchor.DefaultConfig(cfg.VaultPath)
			}
			codeCfg.IndexPath = obsidian.UnifiedIndexPath(cfg.VaultPath, codeCfg.IndexPath)
			if store, _, err := obsidian.OpenIntelStoreFromConfigIfVaultPresent(cfg.VaultPath, codeCfg, false); err == nil && store != nil {
				rt.SetIntelStore(store)
				rt.ownsIntel = true
				status.CodeIndex = true
				status.IndexPath = codeCfg.IndexPath
			}
		}
		status.Ready = true
		if !status.Embeddings {
			status.ReadyReason = "embeddings disabled"
		}
		return rt, status, nil
	}

	// Note embeddings (optional, but required for semantic search).
	embCfg, err := obsidian.LoadEmbeddingsConfig(cfg.VaultPath)
	if err != nil {
		return nil, status, err
	}
	if embCfg.Enabled {
		provider, _, err := embeddings.NewProviderForConfig(embCfg, "")
		if err == nil {
			store, err := embsqlite.Open(embCfg.IndexPath, provider.Dimensions())
			if err == nil {
				status.Embeddings = true
				status.IndexPath = embCfg.IndexPath
				rt.NoteIndex = store
				rt.NoteProvider = provider
				rt.ownsNote = true
			} else if closer, ok := provider.(io.Closer); ok {
				_ = closer.Close()
			}
		}
	}

	// Code embeddings (optional).
	codeEmbCfg, _, err := obsidian.LoadCodeEmbeddingsConfig(cfg.VaultPath)
	if err == nil && codeEmbCfg.Enabled {
		codeProvider, _, err := embeddings.NewProviderForConfig(codeEmbCfg, "")
		if err == nil {
			if store, err := codeembsqlite.Open(codeEmbCfg.IndexPath, codeProvider.Dimensions()); err == nil {
				status.CodeIndex = true
				rt.CodeIndex = store
				rt.CodeProvider = codeProvider
				rt.ownsCode = true
			} else if closer, ok := codeProvider.(io.Closer); ok {
				_ = closer.Close()
			}
		}
	}

	// Unified intel store (required for doc graph + code edges).
	codeCfg, err := obsidian.LoadCodeConfig(cfg.VaultPath)
	if err != nil {
		codeCfg = codeanchor.DefaultConfig(cfg.VaultPath)
	}
	codeCfg.IndexPath = obsidian.UnifiedIndexPath(cfg.VaultPath, codeCfg.IndexPath)
	if store, _, err := obsidian.OpenIntelStoreFromConfigIfVaultPresent(cfg.VaultPath, codeCfg, false); err == nil && store != nil {
		rt.IntelStore = store
		rt.ownsIntel = true
		status.CodeIndex = true
		if status.IndexPath == "" {
			status.IndexPath = codeCfg.IndexPath
		}

	}

	status.Ready = true
	if !status.Embeddings {
		status.ReadyReason = "embeddings disabled"
	}
	return rt, status, nil
}

func ownedCleanup(rt *Runtime) []func() error {
	if rt == nil {
		return nil
	}
	var cleanup []func() error
	if rt.ownsIntel && rt.IntelStore != nil {
		cleanup = append(cleanup, func() error {
			rt.mu.Lock()
			if !rt.ownsIntel || rt.IntelStore == nil {
				rt.mu.Unlock()
				return nil
			}
			store := rt.IntelStore
			rt.IntelStore = nil
			rt.ownsIntel = false
			rt.mu.Unlock()
			return store.Close()
		})
	}
	if rt.ownsNote && rt.NoteIndex != nil {
		cleanup = append(cleanup, rt.NoteIndex.Close)
		if closer, ok := rt.NoteProvider.(io.Closer); ok {
			cleanup = append(cleanup, closer.Close)
		}
	}
	if rt.ownsCode && rt.CodeIndex != nil {
		cleanup = append(cleanup, rt.CodeIndex.Close)
		if closer, ok := rt.CodeProvider.(io.Closer); ok {
			cleanup = append(cleanup, closer.Close)
		}
	}
	return cleanup
}

func serveFile(w http.ResponseWriter, r *http.Request, fsys http.FileSystem, file string) {
	f, err := fsys.Open(file)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
}
