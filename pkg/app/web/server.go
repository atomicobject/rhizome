package web

import (
	"context"
	"errors"
	"fmt"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"io"
	"io/fs"
	"net/http"

	"strings"
	"sync"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/agentchat"

	"github.com/atomicobject/rhizome/pkg/app/finder"
	"github.com/atomicobject/rhizome/pkg/app/userstate"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/embeddings"

	codeembsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/claude"
	"github.com/atomicobject/rhizome/pkg/harness/codex"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"

	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"

	// Config configures the HTTP API server.
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

type Config struct {
	Vault     *repoconfig.Vault
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
	// TasksOpener opens the vault's scheduled-tasks service for one
	// /api/tasks request. Nil answers every tasks route with 503.
	TasksOpener func(ctx context.Context, vaultPath string) (TasksAPI, error)
}

// Server serves the API and bundled UI.
type Server struct {
	queryRecipes        *queryrecipe.Registry
	queryRecipesOnce    sync.Once
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
	noteFormats         noteformat.Runtime
	graphScoresBackfill sync.Once
	nodeEvents          *nodeEventBroker
	globalEvents        *globalEventBroker
	agentService        *agentchat.Service
	userState           *userstate.Store
	finder              *finder.Live
	userStateErr        error // Retains startup diagnostics without exposing raw errors in responses.
	watchHubMu          sync.Mutex
	closed              bool
	watchHubUnsub       func()
	htmlViewers         *htmlViewerRegistry
	viewModules         viewModuleCache
	viewWatch           viewFolderWatch
	tasksMergeWarning   mergeWarningCache
	tasksLaunch         func(vaultPath, itemID string) error // nil starts `rzm tasks run`
}

var agentHarnessOverride func() map[harness.Kind]harness.Harness

type resolvedNoteLink struct {
	Path     string
	Fragment string
	LinkType markdownsyntax.BacklinkType
}

// NewServer constructs a server with initialized dependencies.
func NewServer(ctx context.Context, cfg Config, assets fs.FS) (*Server, error) {
	if cfg.Vault == nil {
		return nil, errors.New("vault is required")
	}
	ctx = vaultconfig.WithEnv(ctx, cfg.Vault.Env)
	if cfg.VaultPath == "" {
		return nil, errors.New("vault path is required")
	}
	formats, err := cfg.NoteMetadata.FormatRuntime()
	if err != nil {
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
		queryRecipes:        queryrecipe.NewRegistry(queryrecipe.DefaultSourceRoots(cfg.VaultPath)),
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
		noteFormats:         formats,
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
	s.finder = finder.NewLive(s.loadFinderCatalog, finderRebuildDelay)
	s.lastReady = status
	if runtime != nil {
		runtime.OnWatchHubSet(s.attachWatchHub)
	}
	s.registerRoutes()
	ready = true
	return s, nil
}

// Close releases runtime resources.
func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	// Drain detached graph and finder builds before the stores they read from are closed.
	if s.graphCache != nil {
		s.graphCache.Close()
	}
	if s.finder != nil {
		s.finder.Close()
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

	var resolved repoconfig.Resolved
	var configErr error
	if cfg.Runtime != nil && cfg.Runtime.Live != nil {
		if provider, ok := cfg.Runtime.Live.(interface{ Configuration() repoconfig.Resolved }); ok {
			resolved = provider.Configuration()
		} else {
			resolved, configErr = repoconfig.Load(cfg.VaultPath)
		}
	} else {
		resolved, configErr = repoconfig.Load(cfg.VaultPath)
	}
	if configErr != nil {
		return nil, status, configErr
	}
	ignoreMatcher := ignore.LoadUnifiedMatcher(cfg.VaultPath, cfg.VaultDef.Excludes)
	rt := &Runtime{Ignore: ignoreMatcher, configuration: &resolved}
	if cfg.Runtime != nil {
		rt = cfg.Runtime
		rt.configuration = &resolved
		if rt.IgnoreMatcher() == nil {
			rt.SetIgnoreMatcher(ignoreMatcher)
		}
		noteIndex, noteProvider := rt.NoteEmbeddings()
		codeIndex, codeProvider := rt.CodeEmbeddings()
		intelStore := rt.Intel()
		status.Embeddings = noteIndex != nil && noteProvider != nil
		status.CodeIndex = intelStore != nil || (codeIndex != nil && codeProvider != nil)
		if status.CodeIndex || status.Embeddings {
			status.IndexPath = resolved.Code.IndexPath
		}
		if rt.Live == nil && intelStore == nil {
			codeCfg := resolved.Code
			store, _, err := indexstore.Open(cfg.VaultPath, indexstore.Options{Access: indexstore.ReadOnly, Config: &codeCfg, IfVaultPresent: true})
			if err != nil {
				return nil, status, err
			}
			if store != nil {
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
	embCfg := resolved.NoteEmbeddings

	if embCfg.Enabled {
		provider, providerCfg, err := embeddings.NewProviderForConfig(vaultconfig.FromContext(ctx), embCfg, "")
		if err == nil {
			metadata := embeddings.MetadataForProvider(provider, providerCfg)
			store, err := codeembsqlite.OpenNotes(ctx, codeembsqlite.File(embCfg.IndexPath), codeembsqlite.Options{Access: codeembsqlite.ReadExisting, Dimensions: provider.Dimensions(), Metadata: &metadata})
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
	codeEmbCfg := resolved.CodeEmbeddings

	if codeEmbCfg.Enabled {
		codeProvider, codeProviderCfg, err := embeddings.NewProviderForConfig(vaultconfig.FromContext(ctx), codeEmbCfg, "")
		if err == nil {
			metadata := embeddings.MetadataForProvider(codeProvider, codeProviderCfg)
			if store, err := codeembsqlite.OpenCode(ctx, codeembsqlite.File(codeEmbCfg.IndexPath), codeembsqlite.Options{Access: codeembsqlite.ReadExisting, Dimensions: codeProvider.Dimensions(), Metadata: &metadata}); err == nil {
				status.CodeIndex = true
				rt.CodeIndex = store
				rt.CodeProvider = codeProvider
				rt.ownsCode = true
			} else if closer, ok := codeProvider.(io.Closer); ok {
				_ = closer.Close()
			}
		}
	}
	codeCfg := resolved.Code
	store, _, err := indexstore.Open(cfg.VaultPath, indexstore.Options{Access: indexstore.ReadOnly, Config: &codeCfg, IfVaultPresent: true})
	if err != nil {
		for _, close := range ownedCleanup(rt) {
			_ = close()
		}
		return nil, status, err
	}
	if store != nil {
		rt.IntelStore = store
		rt.ownsIntel = true
		status.CodeIndex = true
		if status.IndexPath == "" {
			status.IndexPath = codeCfg.IndexPath
		}
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

// Configuration returns the live snapshot when present or this runtime's startup snapshot.
func (r *Runtime) Configuration() repoconfig.Resolved {
	if provider, ok := r.Live.(interface{ Configuration() repoconfig.Resolved }); ok {
		return provider.Configuration()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.configuration != nil {
		return *r.configuration
	}
	return repoconfig.FromLocal("", nil)
}
