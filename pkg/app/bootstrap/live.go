// Package bootstrap provides initialization helpers for rhizome commands.
//
// Docs: [CONTEXT.md](pkg/app/bootstrap/CONTEXT.md)
//
// This package provides two complementary bootstrap patterns:
//   - LiveRuntime (live.go): async capability-based init for long-running servers (`serve`, web)
//   - CodexBootstrap (codex.go): one-shot context gathering for agent CLI commands
//
// LiveRuntime is designed to keep long-running server startup responsive. All
// heavy initialization runs in background goroutines across four phases
// (Search → Semantic → Code → Leader). Callers await capabilities via WaitFor*
// methods rather than blocking on startup.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/runtimeview"
	"github.com/atomicobject/rhizome/pkg/app/semanticruntime"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/logging"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	codeemb "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex/sqlite"
	embsqlite "github.com/atomicobject/rhizome/pkg/search/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
)

// LiveRuntime encapsulates shared initialization for long-running server processes.
// Long-running servers use this to avoid duplicating boot logic.
//
// Docs: [CONTEXT.md](pkg/app/bootstrap/CONTEXT.md)
//
// LiveRuntime uses an async capability-based API:
//   - NewLiveRuntime returns immediately after vault resolution (~10ms)
//   - Heavy init runs in background across 4 phases (Search → Semantic → Code → Leader)
//   - Each phase closes a channel when ready; errors are stored for WaitFor* to return
//   - Phases continue to the next even on failure (graceful degradation)
//
// Capability channels:
//   - searchReady: cache + watchhub (Phase 1)
//   - semanticReady: note embeddings index + provider (Phase 2)
//   - codeReady: code anchor service + intel store (Phase 3)
//   - leaderCh: this process won leader election (Phase 1, triggers Phase 4 work)
//
// Runtime ownership: exactly one process wins .rhizome/runtime.lock and owns
// the vault's watcher, schedulers, and background index (SPEC-0104). A loser
// exits; there is no follower role.
type LiveRuntime struct {
	// Immediately available (set in NewLiveRuntime before it returns)
	Vault     *obsidian.Vault
	VaultPath string
	VaultDef  obsidian.VaultDefinition
	LocalCfg  *obsidian.LocalConfig

	// noteMetadataIndexer is immutable runtime composition for all raw note
	// metadata work in this long-lived process. Construct it before any async
	// initialization so every leader path observes the same providers.
	noteMetadataIndexer notemeta.Indexer
	// noteFormats is the immutable source-format runtime shared by cache
	// selection and the live ownership coordinator.
	noteFormats noteformat.Runtime
	// ownershipVaultDef is the latest immutable definition used by the live
	// ownership selector.
	ownershipVaultDef atomic.Pointer[obsidian.VaultDefinition]
	// ownershipConfigMu serializes ownership selection reloads with the
	// snapshot used to start leader work.
	ownershipConfigMu sync.Mutex

	// Context and options
	ctx                     context.Context
	cancelCtx               context.CancelFunc
	debug                   bool
	skipCacheWarmup         bool
	disableWatchHub         bool
	disableLeaderWork       bool
	requirements            RuntimeRequirements
	skipIntelIntegrityCheck bool
	readOnlyCodeIndex       bool
	disableSessionStore     bool
	sessionOnlyExisting     bool
	queryProvidersOnly      bool

	globalEventSink atomic.Value // stores GlobalEventSink

	// Capability channels (closed when ready)
	searchReady   chan struct{}
	semanticReady chan struct{}
	codeReady     chan struct{}
	leaderCh      chan struct{}

	// Capability errors (set if capability fails to initialize)
	searchErr   atomic.Pointer[error]
	semanticErr atomic.Pointer[error]
	codeErr     atomic.Pointer[error]

	// Components (accessed via getters, nil until ready)
	cache         atomic.Pointer[cache.Service]
	hub           atomic.Pointer[watchhub.Hub]
	noteIndex     atomic.Pointer[embsqlite.Store]
	noteProvider  atomic.Pointer[embeddings.Provider]
	codeAnchorSvc atomic.Pointer[codeanchor.Service]
	intelStore    atomic.Pointer[semdb.Store]
	sessionStore  atomic.Pointer[sessionStoreHolder]
	sessionReady  chan struct{}

	// Code embeddings (optional, may be nil)
	codeEmbStore    atomic.Pointer[codeemb.Store]
	codeEmbProvider atomic.Pointer[embeddings.Provider]

	// Syncers for leader work (created when capabilities are ready)
	noteSyncer atomic.Pointer[semantic.NoteSyncer]
	codeSyncer atomic.Pointer[semantic.Syncer]

	liveWatcher atomic.Pointer[unifiedSemanticWatcher]

	// verifiedSources remembers the exact stat at which SyncWatcher last
	// hash-confirmed each note against its row (live_sync.go).
	sourcesMu       sync.Mutex
	verifiedSources map[string]sourceWitness

	// Embeddings config (for syncer creation)
	embCfg            atomic.Pointer[embeddings.Config]
	codeEmbCfg        atomic.Pointer[embeddings.Config]
	queryNoteProvider atomic.Pointer[embeddings.Provider]
	queryNoteMeta     atomic.Pointer[embeddings.IndexMetadata]

	// Code config (for watcher setup)
	codeCfg       atomic.Pointer[codeanchor.Config]
	noteSelection atomic.Pointer[liveNoteSelectionState]

	// Leader state
	leaderFlag atomic.Bool
	leaderOnce sync.Once

	// indexLane is the serialized executor for all runtime indexing work
	// (SPEC-0104 US3). Lane B installs it before leader work starts.
	indexLane atomic.Pointer[laneHolder]

	// Background indexer callback (optional, set from options)
	backgroundIndexer BackgroundIndexer

	// Cleanup
	closers                []func()
	closeMu                sync.Mutex
	closed                 bool
	workers                sync.WaitGroup
	lockRelease            func() error
	beforeOwnershipRelease func()
	closeOnce              sync.Once

	// Shared write mutex for stores sharing unified DB
	sharedWriteMu sync.Mutex

	liveHealth *liveHealthTracker
}

var _ runtimeview.View = (*LiveRuntime)(nil)

// GlobalEventSink publishes a vault-wide event to the local web broker.
type GlobalEventSink func(kind string, data any)

const (
	globalEventIndexChanged            = "index.changed"
	globalEventValidationInvalidated   = "validation.invalidated"
	globalEventSchemaInvalidated       = "schema.invalidated"
	globalEventQueryRecipeInvalidated  = "query_recipe.invalidated"
	globalEventIndexInvalidated        = "index.invalidated"
	globalEventCapabilitiesInvalidated = "capabilities.invalidated"
	// Mirrors web.GlobalEventReasonReconcileFailed.
	globalEventReasonReconcileFailed = "reconcile-failed"
)

func liveSQLitePoolOptions() sqliteutil.Options {
	return sqliteutil.Options{
		MaxOpenConns:    4,
		MaxIdleConns:    1,
		ConnMaxIdleTime: 5 * time.Minute,
	}
}

func sessionSQLitePoolOptions() sqliteutil.Options {
	return sqliteutil.Options{MaxOpenConns: 1, MaxIdleConns: 1}
}

type sessionStoreHolder struct {
	store semdb.SessionDedupeHandle
}

// SetGlobalEventSink installs the local broker callback. Bootstrap stays
// independent of the web package; serve wires the concrete SSE broker after
// server construction.
func (rt *LiveRuntime) SetGlobalEventSink(fn GlobalEventSink) {
	if rt == nil || fn == nil {
		return
	}
	rt.globalEventSink.Store(fn)
}

// PublishGlobalEvent emits a vault-wide freshness event on this process.
func (rt *LiveRuntime) PublishGlobalEvent(kind string, data any) {
	if rt == nil {
		return
	}
	fn, _ := rt.globalEventSink.Load().(GlobalEventSink)
	if fn != nil {
		fn(kind, data)
	}
}

// ErrEmbeddingsDisabled is the semantic capability's error for a vault whose
// configuration turns embeddings off. It is a supported configuration, not a
// fault: lexical search and the code index work without it.
var ErrEmbeddingsDisabled = errors.New("embeddings not enabled")

// BackgroundIndexer is called by the leader to perform background indexing.
// It receives the context, the process's immutable note metadata indexer,
// vault path, and vault definition.
// The indexer should acquire its own lock and handle yielding to CLI commands.
type BackgroundIndexer func(ctx context.Context, noteMetadataIndexer notemeta.Indexer, vaultPath string, vaultDef obsidian.VaultDefinition, debug bool) error

// RuntimeCapability names an independently requested part of LiveRuntime.
// Long-lived callers use the zero-value RuntimeRequirements, which preserves
// the full runtime. One-shot callers select only capabilities their response
// can consume.
type RuntimeCapability string

const (
	RuntimeCapabilitySearch           RuntimeCapability = "search"
	RuntimeCapabilitySemantic         RuntimeCapability = "semantic"
	RuntimeCapabilityCodeIndex        RuntimeCapability = "code_index"
	RuntimeCapabilityCodeAnchorWarmup RuntimeCapability = "code_anchor_warmup"
	RuntimeCapabilityCodeEmbeddings   RuntimeCapability = "code_embeddings"
	RuntimeCapabilityLeaderSyncers    RuntimeCapability = "leader_syncers"
	RuntimeCapabilityCodeRefDiscovery RuntimeCapability = "coderef_discovery"
)

// RuntimeRequirements is a typed capability selection. Its zero value means
// the full runtime so existing serve/web/agent callers retain current behavior.
type RuntimeRequirements struct {
	selected map[RuntimeCapability]struct{}
}

// RequireRuntimeCapabilities constructs an explicit capability selection.
func RequireRuntimeCapabilities(capabilities ...RuntimeCapability) RuntimeRequirements {
	selected := make(map[RuntimeCapability]struct{}, len(capabilities))
	for _, capability := range capabilities {
		selected[capability] = struct{}{}
	}
	return RuntimeRequirements{selected: selected}
}

// Includes reports whether a capability belongs to this runtime request.
func (r RuntimeRequirements) Includes(capability RuntimeCapability) bool {
	if r.selected == nil {
		return true
	}
	_, ok := r.selected[capability]
	return ok
}

// LiveOptions configures LiveRuntime initialization.
type LiveOptions struct {
	VaultName               string
	Debug                   bool
	BackgroundIndexer       BackgroundIndexer   // optional callback for background indexing on leader start
	BeforeOwnershipRelease  func()              // owner-only finalization after resources drain, before election release
	OnElected               func() error        // owner-only startup barrier; see below
	SkipCacheWarmup         bool                // skip background note-cache warmup for one-shot CLI callers
	DisableWatchHub         bool                // skip watchhub for one-shot CLI callers
	DisableLeaderWork       bool                // skip leader election, watchers, and background work
	Requirements            RuntimeRequirements // requested runtime capabilities (zero value = full runtime)
	SkipIntelIntegrityCheck bool                // explicit latency policy for agent-start rich opens only
	ReadOnlyCodeIndex       bool                // query the existing validated index
	DisableSessionStore     bool                // do not expose a session-dedupe handle or own session cleanup
	SessionOnlyExisting     bool                // open only the existing session-dedupe writer, without CodeIndex
	QueryProvidersOnly      bool                // construct query embedders without opening or mutating embedding stores
}

// OnElected runs synchronously after this process wins the vault and before any
// capability phase starts. Serve uses it for the write-stability barrier, which
// a process that lost election must never perform. An error fails
// NewLiveRuntime. It is never called when DisableLeaderWork is set.

// IndexedReadOnlyRuntimeOptions applies the one-shot policy for commands that
// answer exclusively from the existing indexed read model. The returned
// runtime opens the validated CodeIndex in SQLite read-only/query-only mode
// plus its existing-only session-dedupe handle. Static plan composition may
// suppress that handle when it declares SessionNone. It does not initialize
// search, semantic providers, cache/watchers, indexers, syncers, or leader work.
//
// Callers remain responsible for treating a missing, stale, or incompatible
// index as a fail-soft read result; this policy never creates, migrates,
// repairs, or refreshes indexed state.
func IndexedReadOnlyRuntimeOptions(opts LiveOptions) LiveOptions {
	opts.SkipCacheWarmup = true
	opts.DisableWatchHub = true
	opts.DisableLeaderWork = true
	opts.Requirements = RequireRuntimeCapabilities(RuntimeCapabilityCodeIndex)
	opts.SkipIntelIntegrityCheck = true
	opts.ReadOnlyCodeIndex = true
	return opts
}

// NewLiveRuntime creates a runtime for a long-running server process.
// Returns immediately after resolving vault config. All heavy initialization
// runs asynchronously. Use WaitFor* methods to await specific capabilities.
//
// Only returns an error for truly fatal conditions (vault doesn't exist).
func NewLiveRuntime(ctx context.Context, opts LiveOptions) (*LiveRuntime, error) {
	runtimeCtx, cancel := context.WithCancel(ctx)
	formats, err := builtin.NewRuntime()
	if err != nil {
		cancel()
		return nil, err
	}
	noteMetadataIndexer, err := notemeta.NewIndexer(formats)
	if err != nil {
		cancel()
		return nil, err
	}
	rt := &LiveRuntime{
		ctx:                     runtimeCtx,
		cancelCtx:               cancel,
		debug:                   opts.Debug,
		skipCacheWarmup:         opts.SkipCacheWarmup,
		disableWatchHub:         opts.DisableWatchHub,
		disableLeaderWork:       opts.DisableLeaderWork,
		requirements:            opts.Requirements,
		skipIntelIntegrityCheck: opts.SkipIntelIntegrityCheck,
		readOnlyCodeIndex:       opts.ReadOnlyCodeIndex,
		disableSessionStore:     opts.DisableSessionStore,
		sessionOnlyExisting:     opts.SessionOnlyExisting,
		queryProvidersOnly:      opts.QueryProvidersOnly,
		beforeOwnershipRelease:  opts.BeforeOwnershipRelease,
		searchReady:             make(chan struct{}),
		semanticReady:           make(chan struct{}),
		codeReady:               make(chan struct{}),
		leaderCh:                make(chan struct{}),
		noteMetadataIndexer:     noteMetadataIndexer,
		noteFormats:             formats,
		sessionReady:            make(chan struct{}),
	}

	// Resolve vault (this is fast and required for everything else)
	if opts.VaultName == "" {
		vault := &obsidian.Vault{}
		defaultName, err := vault.DefaultName()
		if err != nil {
			cancel()
			return nil, err
		}
		opts.VaultName = defaultName
	}

	rt.Vault = &obsidian.Vault{Name: opts.VaultName}
	vaultDef, err := rt.Vault.Definition()
	if err != nil {
		cancel()
		return nil, err
	}
	rt.VaultDef = vaultDef
	rt.ownershipVaultDef.Store(&vaultDef)
	rt.VaultPath = vaultDef.BasePath()
	rt.liveHealth = newLiveHealthTracker(rt.VaultPath)

	// Load local config (fast)
	if _, cfg, err := obsidian.FindLocalConfig(rt.VaultPath); err == nil && cfg != nil {
		rt.LocalCfg = cfg
		// The registered vault definition supplies identity and location, while
		// the repository config owns the current note selection policy. Keep the
		// public runtime definition aligned with the watcher so web mutations and
		// other exact refreshes do not fall back to Markdown-only discovery.
		if configured, loadErr := loadWatcherVaultDefinition(rt.VaultPath, rt.VaultDef); loadErr == nil {
			rt.VaultDef = configured
			rt.ownershipVaultDef.Store(&configured)
		}
	}

	// Elect the vault's runtime owner before returning so `rzm serve` can
	// decide to exit before binding a listener (SPEC-0104 US1). Runtimes with
	// leader work disabled never elect.
	rt.backgroundIndexer = opts.BackgroundIndexer

	if !opts.DisableLeaderWork && !rt.electRuntimeOwner() {
		// Another process owns this vault. A loser must exit without opening,
		// migrating, or writing anything of the winner's, so no initialization
		// starts at all; every capability reports why.
		rt.failAllCapabilities(ErrNotRuntimeOwner)
		return rt, nil
	}

	// The owner's synchronous write barrier runs here: after it is entitled to
	// write the vault, and before any reader or background worker starts.
	if opts.OnElected != nil {
		if err := func() (err error) {
			defer func() {
				if panicked := recover(); panicked != nil {
					_ = rt.Close()
					panic(panicked)
				}
			}()
			return opts.OnElected()
		}(); err != nil {
			// Election already installed the process-lifetime lock. A failed
			// owner-only barrier must release it before the caller can retry.
			_ = rt.Close()
			return nil, err
		}
	}

	if opts.SessionOnlyExisting && !opts.DisableSessionStore {
		rt.startWorker(rt.initSessionStoreOnly)
	} else {
		close(rt.sessionReady)
	}
	// Start async initialization
	rt.startWorker(rt.initPhase1Search)

	return rt, nil
}

// initSessionStoreOnly opens the narrow existing-only session writer without
// making note-only requests construct or await the CodeIndex reader.
func (rt *LiveRuntime) initSessionStoreOnly() {
	defer close(rt.sessionReady)
	if rt.requirements.Includes(RuntimeCapabilityCodeIndex) {
		// Initial WAL connections can contend even before a query executes.
		// Open the required reader before its optional session writer.
		select {
		case <-rt.ctx.Done():
			return
		case <-rt.codeReady:
		}
	}
	codeCfg := loadLiveCodeConfig(rt.VaultPath)
	store, err := semdb.OpenSessionStoreExisting(codeCfg.IndexPath, rt.ctx, sessionSQLitePoolOptions())
	if err != nil || store == nil || rt.isClosed() {
		if store != nil {
			_ = store.Close()
		}
		return
	}
	rt.sessionStore.Store(&sessionStoreHolder{store: store})
	rt.addCloser(func() { _ = store.Close() })
}

// Phase 1: Basic search capability (cache + watchhub)
func (rt *LiveRuntime) initPhase1Search() {
	defer rt.observeCapabilityPhase("search")()
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("search init panic: %v", r)
			rt.searchErr.Store(&err)
			close(rt.searchReady)
			// Continue to next phases so WaitFor* methods don't hang forever
			rt.startWorker(rt.initPhase2Semantic)
		}
	}()

	if !rt.requirements.Includes(RuntimeCapabilitySearch) {
		err := capabilityNotRequested(RuntimeCapabilitySearch)
		rt.searchErr.Store(&err)
		close(rt.searchReady)
		rt.startWorker(rt.initPhase2Semantic)
		return
	}

	// Initialize cache service from the immutable ownership selector. Raw watch
	// events remain available even when this Markdown projection rejects them.
	cacheCodeCfg := loadLiveCodeConfig(rt.VaultPath)
	rt.codeCfg.Store(&cacheCodeCfg)
	selectionPolicy, err := liveCacheSelectionPolicy(rt.VaultDef, rt.noteFormats, cacheCodeCfg)
	if err != nil {
		err = fmt.Errorf("compile cache ownership selection: %w", err)
		rt.searchErr.Store(&err)
		close(rt.searchReady)
		rt.startWorker(rt.initPhase2Semantic)
		return
	}
	rt.setNoteSelectionPolicy(selectionPolicy)
	rt.publishWarmIntelStoreForRead(cacheCodeCfg)
	cacheOpts := cache.Options{
		DiscoverFiles: selectionPolicy.DiscoverFiles,
		AdmitNote:     selectionPolicy.Admit,
		UserExcludes:  selectionPolicy.UserExcludes,
		NoteRuntime:   &rt.noteFormats,
	}
	var watchRoots []string
	if rt.VaultDef.IsCollection() {
		watchRoots = rt.VaultDef.WatchRoots()
	}

	// Add coderef config if available
	if rt.requirements.Includes(RuntimeCapabilityCodeRefDiscovery) && rt.LocalCfg != nil {
		if inc, exc := obsidian.NormalizeCodeRefPatterns(*rt.LocalCfg); len(inc) > 0 {
			cacheOpts.CodeRefConfig = coderefs.NewConfig(true, inc, exc)
		}
	}

	cacheService, err := cache.NewService(rt.VaultPath, cacheOpts)
	if err != nil {
		err = fmt.Errorf("cache init failed: %w", err)
		rt.searchErr.Store(&err)
		close(rt.searchReady)
		rt.startWorker(rt.initPhase2Semantic)
		return
	}
	rt.cache.Store(cacheService)
	rt.addCloser(func() { _ = cacheService.Close() })

	if !rt.disableWatchHub {
		// Initialize watchhub
		watchPolicy := semanticruntime.PolicyFor(semanticruntime.IndexRequest{
			Source:       semanticruntime.IndexSourceWatcher,
			LatencyClass: semanticruntime.LatencyAuto,
		})
		hubOpts := watchhub.Options{
			UserExcludes: rt.VaultDef.Excludes,
			Debug:        rt.debug,
			Debounce:     watchPolicy.Debounce,
		}
		hub, err := watchhub.NewHub(rt.VaultPath, hubOpts)
		if err != nil {
			if diagnostics.FromContext(rt.ctx) != nil {
				rt.recordDiagnostic(slog.LevelWarn, "watchhub.unavailable", err)
			}
		} else {
			rt.hub.Store(hub)
			hub.Start(rt.ctx)
			rt.addCloser(func() {
				if err := hub.Close(); err != nil {
					rt.recordDiagnostic(slog.LevelWarn, "watchhub.close_failed", err)
				}
			})

			_ = hub.AddRoot(rt.VaultPath, watchhub.RootOptions{Kind: watchhub.RootNotes, WatchForIgnore: true})
			_ = hub.AddRoot(filepath.Join(rt.VaultPath, ".rhizome"), watchhub.RootOptions{Kind: watchhub.RootNotes, IncludeHidden: true})
			for _, root := range watchRoots {
				_ = hub.AddRoot(root, watchhub.RootOptions{Kind: watchhub.RootNotes})
			}
			cache.SubscribeWatchHub(hub, cacheService)

			rt.startWorker(hub.AddWatchRoots)
		}
	}

	if !rt.skipCacheWarmup {
		// Warm the note cache without delaying search readiness.
		rt.startWorker(func() {
			if err := cacheService.EnsureReady(rt.ctx); err != nil && !errors.Is(err, context.Canceled) {
				rt.recordDiagnostic(slog.LevelWarn, "cache.warmup_failed", err)
			}
		})
	}

	// Search capability is ready
	close(rt.searchReady)

	// Continue to phase 2
	rt.startWorker(rt.initPhase2Semantic)
}

// Phase 2: Semantic search capability (note embeddings)
func (rt *LiveRuntime) initPhase2Semantic() {
	defer rt.observeCapabilityPhase("semantic")()
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("semantic init panic: %v", r)
			rt.semanticErr.Store(&err)
			close(rt.semanticReady)
			// Continue to next phases so WaitFor* methods don't hang forever
			rt.startWorker(rt.initPhase3Code)
		}
	}()
	if rt.isClosed() {
		err := context.Canceled
		rt.semanticErr.Store(&err)
		close(rt.semanticReady)
		rt.startWorker(rt.initPhase3Code)
		return
	}
	if !rt.requirements.Includes(RuntimeCapabilitySemantic) {
		err := capabilityNotRequested(RuntimeCapabilitySemantic)
		rt.semanticErr.Store(&err)
		close(rt.semanticReady)
		rt.startWorker(rt.initPhase3Code)
		return
	}

	// Validate embeddings provider is configured
	if err := obsidian.ValidateEmbeddingsProvider(rt.VaultPath); err != nil {
		err = fmt.Errorf("semantic search unavailable: %w\n\nTo fix: run 'rzm init' in your terminal to configure an embeddings provider", err)
		rt.semanticErr.Store(&err)
		close(rt.semanticReady)
		rt.startWorker(rt.initPhase3Code)
		return
	}

	embCfg, err := obsidian.LoadEmbeddingsConfig(rt.VaultPath)
	if err != nil || !embCfg.Enabled {
		if err != nil {
			rt.semanticErr.Store(&err)
		} else {
			err = ErrEmbeddingsDisabled
			rt.semanticErr.Store(&err)
		}
		close(rt.semanticReady)
		rt.startWorker(rt.initPhase3Code)
		return
	}

	provider, providerCfg, err := embeddings.NewProviderForConfig(embCfg, "")
	if err != nil {
		err = embeddings.ProviderUnavailableError(embCfg, err)
		rt.semanticErr.Store(&err)
		close(rt.semanticReady)
		rt.startWorker(rt.initPhase3Code)
		return
	}
	if closer, ok := provider.(io.Closer); ok {
		rt.addCloser(func() { _ = closer.Close() })
	}
	if rt.queryProvidersOnly {
		meta := embeddings.MetadataForProvider(provider, providerCfg)
		rt.queryNoteProvider.Store(&provider)
		rt.queryNoteMeta.Store(&meta)
		rt.embCfg.Store(&embCfg)
		close(rt.semanticReady)
		rt.startWorker(rt.initPhase3Code)
		return
	}

	ctxReady, cancelReady := context.WithTimeout(rt.ctx, 5*time.Minute)
	defer cancelReady()

	storeCtx := indexingperf.WithPhase(ctxReady, indexingperf.AgentStartPhaseStoreWarmOpen)
	doneStore := indexingperf.StartSpan(storeCtx, indexingperf.AgentStartPhaseStoreWarmOpen)
	store, err := embsqlite.OpenWithMetadataWithOptions(storeCtx, embCfg.IndexPath, provider, embeddings.MetadataForProvider(provider, providerCfg), embsqlite.OpenOptions{
		Pool: liveSQLitePoolOptions(),
	})
	doneStore(err)
	if err != nil {
		var metaErr embeddings.MetadataError
		if errors.As(err, &metaErr) {
			err = fmt.Errorf("semantic index: %w", metaErr)
		} else {
			err = fmt.Errorf("semantic index unavailable at %s: %w", embCfg.IndexPath, err)
		}
		rt.semanticErr.Store(&err)
		close(rt.semanticReady)
		rt.startWorker(rt.initPhase3Code)
		return
	}
	if rt.isClosed() {
		_ = store.Close()
		err := context.Canceled
		rt.semanticErr.Store(&err)
		close(rt.semanticReady)
		rt.startWorker(rt.initPhase3Code)
		return
	}

	store.SetWriteMu(&rt.sharedWriteMu)
	rt.noteIndex.Store(store)
	rt.noteProvider.Store(&provider)
	rt.embCfg.Store(&embCfg)
	rt.addCloser(func() {
		if err := store.Close(); err != nil {
			rt.recordDiagnostic(slog.LevelWarn, "note_embeddings.close_failed", err)
		}
	})

	// Semantic capability is ready
	close(rt.semanticReady)

	// Continue to phase 3
	rt.startWorker(rt.initPhase3Code)
}

// Phase 3: Code index capability (code anchor service + intel store)
func (rt *LiveRuntime) initPhase3Code() {
	defer rt.observeCapabilityPhase("code")()
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("code index init panic: %v", r)
			rt.codeErr.Store(&err)
			close(rt.codeReady)
			// Continue to leader phase so leader work can still proceed
			rt.initPhase4Leader()
		}
	}()
	if rt.isClosed() {
		err := context.Canceled
		rt.codeErr.Store(&err)
		close(rt.codeReady)
		rt.initPhase4Leader()
		return
	}
	if !rt.requirements.Includes(RuntimeCapabilityCodeIndex) {
		err := capabilityNotRequested(RuntimeCapabilityCodeIndex)
		rt.codeErr.Store(&err)
		close(rt.codeReady)
		rt.initPhase4Leader()
		return
	}

	codeCfg, err := rt.publishPhase3OwnershipConfiguration()
	if err != nil {
		err = fmt.Errorf("compile cache ownership selection: %w", err)
		rt.codeErr.Store(&err)
		close(rt.codeReady)
		rt.initPhase4Leader()
		return
	}

	recoveryWindow := codeIndexOpenRecoveryWindow
	if rt.readOnlyCodeIndex {
		// One-shot indexed reads report unavailable evidence promptly; they do
		// not own the long-lived runtime's startup recovery window.
		recoveryWindow = 0
	}
	store, cleanup, err := openIntelStoreWithRetry(rt.ctx, recoveryWindow, func() (*semdb.Store, func(), error) {
		return rt.openIntelStoreForServe(codeCfg)
	})
	if err != nil {
		err = fmt.Errorf("code index unavailable at %s: %w", codeCfg.IndexPath, err)
		rt.codeErr.Store(&err)
		close(rt.codeReady)
		rt.initPhase4Leader()
		return
	}
	if store == nil {
		err = errors.New("code index not available")
		rt.codeErr.Store(&err)
		close(rt.codeReady)
		rt.initPhase4Leader()
		return
	}
	if rt.isClosed() {
		cleanup()
		err := context.Canceled
		rt.codeErr.Store(&err)
		close(rt.codeReady)
		rt.initPhase4Leader()
		return
	}

	store.SetWriteMu(&rt.sharedWriteMu)
	if rt.queryProvidersOnly {
		provider := rt.queryNoteProvider.Load()
		meta := rt.queryNoteMeta.Load()
		if provider != nil && meta != nil {
			err = embsqlite.ValidateExistingMetadata(rt.ctx, store.DB(), *meta)
		}
		if err != nil {
			cleanup()
			err = fmt.Errorf("note embedding index incompatible: %w", err)
			rt.codeErr.Store(&err)
			close(rt.codeReady)
			rt.initPhase4Leader()
			return
		}
		if provider != nil {
			rt.noteProvider.Store(provider)
		}
	}
	rt.intelStore.Store(store)
	rt.addCloser(func() {
		if diagnostics.FromContext(rt.ctx) != nil {
			rt.recordDiagnostic(slog.LevelInfo, "intel_store.closed", nil)
		}
		cleanup()
	})

	var sessionStore semdb.SessionDedupeHandle
	if !rt.disableSessionStore && !rt.readOnlyCodeIndex {
		sessionStore = store
	}
	if !rt.disableSessionStore && rt.readOnlyCodeIndex && !rt.sessionOnlyExisting {
		// Indexed retrieval remains query-only. Session dedupe gets a separate,
		// existing-only validated handle so repeated one-shot commands can
		// persist reservations without enabling index mutation or migrations.
		doneSessionStore := indexingperf.StartSpan(
			indexingperf.WithPhase(rt.ctx, indexingperf.AgentStartPhaseStoreWarmOpen),
			indexingperf.AgentStartPhaseStoreWarmOpen,
		)
		sessionStore, err = semdb.OpenSessionStoreExisting(codeCfg.IndexPath, rt.ctx, sessionSQLitePoolOptions())
		doneSessionStore(err)
		if err != nil {
			sessionStore = nil
			if diagnostics.FromContext(rt.ctx) != nil {
				rt.recordDiagnostic(slog.LevelInfo, "session_store.unavailable", err)
			}
		} else {
			rt.addCloser(func() {
				if err := sessionStore.Close(); err != nil {
					rt.recordDiagnostic(slog.LevelWarn, "session_store.close_failed", err)
				}
			})
		}
	}
	if sessionStore != nil {
		rt.sessionStore.Store(&sessionStoreHolder{store: sessionStore})
	}
	if rt.ownsSessionCleanup() {
		// Only write-capable runtimes with session persistence own cleanup.
		rt.startSessionCleanup(store)
	}

	// Create code anchor service
	var warmCtx context.Context
	includeIndexers := codeCfg.Enabled && rt.requirements.Includes(RuntimeCapabilityCodeAnchorWarmup)
	if includeIndexers {
		warmCtx = rt.ctx
	}
	svc, _ := NewCodeAnchorService(CodeAnchorServiceConfig{
		VaultPath:        rt.VaultPath,
		CodeCfg:          codeCfg,
		Store:            store,
		WarmCacheContext: warmCtx,
		IncludeIndexers:  includeIndexers,
		WriteAccess:      includeIndexers,
	})
	if svc != nil {
		rt.codeAnchorSvc.Store(svc)
	}

	// Initialize code embeddings (optional, failures don't block code capability)
	if rt.requirements.Includes(RuntimeCapabilityCodeEmbeddings) {
		rt.initCodeEmbeddings(store)
	}

	// Create syncers for leader work
	if rt.requirements.Includes(RuntimeCapabilityLeaderSyncers) {
		rt.createSyncers(store)
	}

	// Code capability is ready
	close(rt.codeReady)

	// Continue to phase 4
	rt.initPhase4Leader()
}

// Phase 4: Leader work (runs continuously in background)
func (rt *LiveRuntime) initPhase4Leader() {
	defer rt.observeCapabilityPhase("leader")()
	if rt.disableLeaderWork {
		return
	}
	// Wait for leader status (either already leader or become leader later)
	rt.startWorker(func() {
		select {
		case <-rt.ctx.Done():
			return
		case <-rt.leaderCh:
			rt.startLeaderWork()
		}
	})
}

// WaitForSearch blocks until the search capability is ready or context is cancelled.
// Search capability includes: cache and watchhub.
func (rt *LiveRuntime) WaitForSearch(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-rt.searchReady:
		if err := rt.searchErr.Load(); err != nil {
			return *err
		}
		return nil
	}
}

// WaitForSemantic blocks until the semantic capability is ready or context is cancelled.
// Semantic capability includes: note embeddings index and provider.
func (rt *LiveRuntime) WaitForSemantic(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-rt.semanticReady:
		if err := rt.semanticErr.Load(); err != nil {
			return *err
		}
		return nil
	}
}

// WaitForCodeIndex blocks until the code index capability is ready or context is cancelled.
// Code index capability includes: code anchor service and intel store.
func (rt *LiveRuntime) WaitForCodeIndex(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-rt.codeReady:
		if err := rt.codeErr.Load(); err != nil {
			return *err
		}
		return nil
	}
}

// WaitForLeader blocks until this runtime becomes leader or context is cancelled.
func (rt *LiveRuntime) WaitForLeader(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-rt.leaderCh:
		return true
	}
}

// Snapshot returns the currently available phase states and dependencies
// without waiting for initialization.
func (rt *LiveRuntime) Snapshot() runtimeview.Snapshot {
	if rt == nil {
		return runtimeview.Snapshot{}
	}
	notePath := ""
	if cfg := rt.embCfg.Load(); cfg != nil {
		notePath = cfg.IndexPath
	}
	codeIndex, codeProvider := rt.CodeEmbeddings()
	var noteReader obsidian.NoteReader
	if cacheService := rt.cache.Load(); cacheService != nil {
		noteReader = cache.NewNoteAdapter(cacheService, &obsidian.Note{})
	}
	return runtimeview.Snapshot{
		Search:            capabilityState(rt.searchReady, rt.searchErr.Load()),
		Semantic:          capabilityState(rt.semanticReady, rt.semanticErr.Load()),
		Code:              capabilityState(rt.codeReady, rt.codeErr.Load()),
		Leader:            rt.leaderFlag.Load(),
		WatchHub:          rt.hub.Load(),
		NoteReader:        noteReader,
		NoteIndex:         rt.noteIndex.Load(),
		NoteProvider:      rt.NoteProvider(),
		NoteIndexPath:     notePath,
		CodeIndex:         codeIndex,
		CodeProvider:      codeProvider,
		CodeAnchorService: rt.codeAnchorSvc.Load(),
		IntelStore:        rt.intelStore.Load(),
		SessionStore:      rt.SessionDedupeStore(),
	}
}

func capabilityState(done <-chan struct{}, errPtr *error) runtimeview.CapabilityState {
	state := runtimeview.CapabilityState{Err: nil}
	select {
	case <-done:
		state.Done = true
	default:
		return state
	}
	if errPtr != nil {
		state.Err = *errPtr
		return state
	}
	state.Ready = true
	return state
}

// SearchReady returns true if the search capability is ready.
func (rt *LiveRuntime) SearchReady() bool {
	select {
	case <-rt.searchReady:
		return rt.searchErr.Load() == nil
	default:
		return false
	}
}

// SemanticReady returns whether semantic capability is ready and any error.
func (rt *LiveRuntime) SemanticReady() (bool, error) {
	select {
	case <-rt.semanticReady:
		if err := rt.semanticErr.Load(); err != nil {
			return false, *err
		}
		return true, nil
	default:
		return false, nil
	}
}

// CodeIndexReady returns whether code index capability is ready and any error.
func (rt *LiveRuntime) CodeIndexReady() (bool, error) {
	select {
	case <-rt.codeReady:
		if err := rt.codeErr.Load(); err != nil {
			return false, *err
		}
		return true, nil
	default:
		return false, nil
	}
}

// Cache returns the cache service (nil until search ready).
func (rt *LiveRuntime) Cache() *cache.Service {
	return rt.cache.Load()
}

// Hub returns the watchhub (nil until search ready, may be nil if unavailable).
func (rt *LiveRuntime) Hub() *watchhub.Hub {
	return rt.hub.Load()
}

// NoteIndex returns the note embeddings index (nil until semantic ready).
func (rt *LiveRuntime) NoteIndex() *embsqlite.Store {
	return rt.noteIndex.Load()
}

// NoteProvider returns the note embeddings provider (nil until semantic ready).
func (rt *LiveRuntime) NoteProvider() embeddings.Provider {
	if p := rt.noteProvider.Load(); p != nil {
		return *p
	}
	return nil
}

// CodeAnchorService returns the code anchor service (nil until code ready).
func (rt *LiveRuntime) CodeAnchorService() *codeanchor.Service {
	return rt.codeAnchorSvc.Load()
}

// IntelStore returns the current unified intel read handle. A validated,
// existing read-only handle may be published during search initialization;
// phase 3 replaces it with the full code-index handle.
func (rt *LiveRuntime) IntelStore() *semdb.Store {
	return rt.intelStore.Load()
}

// NoteMetadataIndexer returns the immutable provider-aware metadata indexer
// composed for this runtime before asynchronous initialization begins.
func (rt *LiveRuntime) NoteMetadataIndexer() notemeta.Indexer {
	if rt == nil {
		return notemeta.Indexer{}
	}
	return rt.noteMetadataIndexer
}

// SessionDedupeStore returns the narrow session persistence capability.
func (rt *LiveRuntime) SessionDedupeStore() semdb.SessionDedupeStore {
	holder := rt.sessionStore.Load()
	if holder == nil {
		return nil
	}
	return holder.store
}

// WaitForSession waits only for the optional existing-only session writer.
func (rt *LiveRuntime) WaitForSession(ctx context.Context) error {
	select {
	case <-rt.sessionReady:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// LiveHealth reports current live watcher status.
func (rt *LiveRuntime) LiveHealth() LiveHealth {
	if rt == nil {
		return LiveHealth{Role: "unknown", WatcherMode: "unavailable"}
	}
	role := "leader"
	if !rt.leaderFlag.Load() {
		role = "unavailable"
	}
	mode := "single-process"
	var current, last *LiveEpoch
	if rt.liveHealth != nil {
		current, last = rt.liveHealth.snapshot()
	}
	cacheService := rt.cache.Load()
	pendingDirty := 0
	if cacheService != nil {
		pendingDirty = len(cacheService.DirtySnapshot())
	}
	var scheduler LiveSchedulerHealth
	if watcher := rt.liveWatcher.Load(); watcher != nil {
		scheduler = watcher.schedulerHealth()
	}
	health := LiveHealth{
		Role:               role,
		WatcherMode:        mode,
		PendingDirtyCount:  pendingDirty,
		Scheduler:          scheduler,
		CurrentEpoch:       current,
		LastCompletedEpoch: last,
	}
	if last != nil {
		if last.Status == "failed" {
			health.FailedPhase = last.Phase
			health.Error = last.Error
		}
		health.DegradedReasons = append([]string(nil), last.DegradedReasons...)
	}
	return health
}

// EmbeddingsConfig returns the note embeddings configuration (nil until semantic ready).
func (rt *LiveRuntime) EmbeddingsConfig() *embeddings.Config {
	return rt.embCfg.Load()
}

// CodeEmbeddingsConfig returns the code embeddings configuration (nil until code ready).
func (rt *LiveRuntime) CodeEmbeddingsConfig() *embeddings.Config {
	return rt.codeEmbCfg.Load()
}

// CodeEmbeddings returns the code embeddings store and provider (both nil until code ready).
func (rt *LiveRuntime) CodeEmbeddings() (*codeemb.Store, embeddings.Provider) {
	store := rt.codeEmbStore.Load()
	if p := rt.codeEmbProvider.Load(); p != nil {
		return store, *p
	}
	return store, nil
}

// NoteSyncer returns the note syncer for embeddings (nil until leader work starts).
func (rt *LiveRuntime) NoteSyncer() *semantic.NoteSyncer {
	return rt.noteSyncer.Load()
}

// CodeSyncer returns the code syncer for embeddings (nil until leader work starts).
func (rt *LiveRuntime) CodeSyncer() *semantic.Syncer {
	return rt.codeSyncer.Load()
}

func (rt *LiveRuntime) startLeaderWork() {
	cacheService := rt.cache.Load()
	noteSvc := rt.codeAnchorSvc.Load()
	intelStore := rt.intelStore.Load()
	hub := rt.hub.Load()
	vaultDef, leaderCodeCfg, ownershipErr := rt.leaderOwnershipConfiguration()
	if ownershipErr != nil {
		rt.recordDiagnostic(slog.LevelWarn, "ownership.reload_failed", ownershipErr)
	}
	codeCfg := &leaderCodeCfg
	unifiedWatcherAvailable := cacheService != nil && noteSvc != nil && intelStore != nil

	// subscriberReady is closed after WatchHub roots are registered and any
	// fallback codeanchor subscriber is installed, allowing us to safely enable
	// FSNotify without losing events.
	subscriberReady := make(chan struct{})

	if codeCfg.Enabled && noteSvc != nil {
		// Register WatchHub roots for note/code events. When the unified semantic
		// watcher is available, it is the live owner for durable note/code ingest;
		// the legacy codeanchor subscriber stays off so it cannot also full-rescan,
		// ingest notes, index code, or recompute scopes for the same events.
		noteRoots := []string{rt.VaultPath}
		codeRoots := ResolveRoots(rt.VaultPath, codeCfg.CodeRoots())
		ignoreGlobs := append([]string{}, codeCfg.PythonIgnore...)
		ignoreGlobs = append(ignoreGlobs, codeCfg.GoIgnore...)
		ignoreGlobs = append(ignoreGlobs, codeCfg.TSIgnore...)
		ignoreGlobs = append(ignoreGlobs, codeCfg.CSharpIgnore...)
		ignoreGlobs = append(ignoreGlobs, codeCfg.PHPIgnore...)

		rt.startWorker(func() {
			defer close(subscriberReady)
			if hub == nil {
				rt.recordDiagnostic(slog.LevelWarn, "watch_roots.skipped", nil, slog.String("reason_code", "watch_hub_unavailable"))
				return
			}
			for _, root := range noteRoots {
				_ = hub.AddRoot(root, watchhub.RootOptions{Kind: watchhub.RootNotes})
			}
			for _, root := range codeRoots {
				if err := hub.AddRoot(root, watchhub.RootOptions{Kind: watchhub.RootCode}); err != nil {
					rt.recordDiagnostic(slog.LevelWarn, "watch_root.registration_failed", err)
				} else {
					rt.recordDiagnostic(slog.LevelInfo, "watch_root.registered", nil)
				}
			}
			if !shouldStartLegacyCodeAnchorSubscriber(unifiedWatcherAvailable) {
				if diagnostics.FromContext(rt.ctx) != nil {
					rt.recordDiagnostic(slog.LevelInfo, "code_subscriber.skipped", nil, slog.String("reason_code", "unified_watcher_owns_ingest"))
				}
				hub.AddWatchRoots()
				return
			}
			watcher, err := codeanchor.NewHubSubscriber(
				noteSvc,
				noteRoots,
				codeRoots,
				func(path string, content []byte, mtime int64) codeanchor.NoteSource {
					return notemeta.NewContentOnlyNoteSourceSnapshot(path, string(content), mtime)
				},
				codeanchor.WithWatcherExcludeGlobs(ignoreGlobs),
				codeanchor.WithWatcherIgnoreMatcher(hub.IgnoreMatcher()),
				codeanchor.WithWatcherDebounce(0),
			)
			if err != nil {
				rt.recordDiagnostic(slog.LevelWarn, "code_subscriber.unavailable", err)
				return
			}
			rt.addCloser(func() {
				if err := watcher.Close(); err != nil {
					rt.recordDiagnostic(slog.LevelWarn, "code_subscriber.close_failed", err)
				}
			})
			if err := watcher.Start(rt.ctx); err != nil {
				rt.recordDiagnostic(slog.LevelWarn, "code_subscriber.start_failed", err)
			} else {
				rt.recordDiagnostic(slog.LevelInfo, "code_subscriber.started", nil, slog.Int("note_roots", len(noteRoots)), slog.Int("code_roots", len(codeRoots)))
			}
			vaultPaths, _ := paths.NewVaultPaths(rt.VaultPath)
			prefixes := make([]string, 0, len(noteRoots)+len(codeRoots))
			for _, root := range append(append([]string{}, noteRoots...), codeRoots...) {
				if rel, err := vaultPaths.RelStrict(root); err == nil {
					if rel.String() == "" {
						prefixes = nil
						break
					}
					prefixes = append(prefixes, filepath.ToSlash(rel.String()))
				}
			}
			onStale := func(ctx context.Context, ev watchhub.StaleEvent) {
				if ev.Reason == watchhub.StaleIgnoreChanged {
					watcher.SetIgnoreMatcher(hub.IgnoreMatcher())
				}
				watcher.HandleWatchStale(ctx, ev)
			}
			unsub := hub.Subscribe("codeanchor", watchhub.Filter{
				Prefixes:      prefixes,
				IncludeDirs:   true,
				IncludeFiles:  true,
				IncludeHidden: false,
			}, watcher.HandleWatchEvents, onStale)
			rt.addCloser(unsub)
			hub.AddWatchRoots()
			if diagnostics.FromContext(rt.ctx) != nil {
				rt.recordDiagnostic(slog.LevelInfo, "watcher.ready", nil, slog.Int("note_roots", len(noteRoots)), slog.Int("code_roots", len(codeRoots)))
			}
		})
	} else {
		// No codeanchor subscriber needed; signal ready immediately.
		close(subscriberReady)
	}

	rt.startLeaderIndexing(vaultDef, codeCfg, cacheService, noteSvc, intelStore, hub, unifiedWatcherAvailable)
}

func shouldStartLegacyCodeAnchorSubscriber(unifiedWatcherAvailable bool) bool {
	return !unifiedWatcherAvailable
}

func (rt *LiveRuntime) startSessionCleanup(store *semdb.Store) {
	if store == nil {
		return
	}
	doCleanup := func() {
		removed, ran, err := runSessionCleanupIfDue(context.Background(), store, time.Now())
		if err != nil {
			if diagnostics.FromContext(rt.ctx) != nil {
				rt.recordDiagnostic(slog.LevelWarn, "sessions.cleanup_failed", err)
			}
		} else if ran && removed > 0 {
			rt.recordDiagnostic(slog.LevelInfo, "sessions.cleaned", nil, slog.Int64("removed", removed))
		}
	}
	rt.startWorker(doCleanup)
	ticker := time.NewTicker(12 * time.Hour)
	rt.startWorker(func() {
		defer ticker.Stop()
		for {
			select {
			case <-rt.ctx.Done():
				return
			case <-ticker.C:
				doCleanup()
			}
		}
	})
}

func (rt *LiveRuntime) ownsSessionCleanup() bool {
	return !rt.readOnlyCodeIndex && !rt.disableSessionStore
}

func runSessionCleanupIfDue(ctx context.Context, store *semdb.Store, now time.Time) (removed int64, ran bool, err error) {
	return runSessionCleanupIfDueWith(ctx, store, now, store.CleanupSessions)
}

func runSessionCleanupIfDueWith(
	ctx context.Context,
	store *semdb.Store,
	now time.Time,
	cleanup func(context.Context, time.Time) (int64, error),
) (removed int64, ran bool, err error) {
	due, err := store.SessionCleanupDue(ctx, now)
	if err != nil || !due {
		return 0, false, err
	}
	claimedUntil := now.Add(12 * time.Hour)
	claimed, err := store.ClaimSessionCleanup(ctx, now, claimedUntil)
	if err != nil || !claimed {
		return 0, false, err
	}
	removed, err = cleanup(ctx, now.Add(-semdb.DefaultSessionRetention))
	if err != nil {
		// Cleanup can fail because ctx was canceled. Use a bounded context that
		// preserves values but not cancellation so the failed claimant can
		// relinquish without suppressing retries for the full cadence.
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, releaseErr := store.RelinquishSessionCleanup(releaseCtx, claimedUntil, now)
		return removed, true, errors.Join(err, releaseErr)
	}
	return removed, true, err
}

// initCodeEmbeddings initializes code embeddings store (optional, failures don't block).
func (rt *LiveRuntime) initCodeEmbeddings(intelStore *semdb.Store) {
	codeEmbCfg, exists, err := obsidian.LoadCodeEmbeddingsConfig(rt.VaultPath)
	if err != nil || !exists || !codeEmbCfg.Enabled {
		return
	}

	rt.codeEmbCfg.Store(&codeEmbCfg)

	var codeProvider embeddings.Provider
	codeProviderCfg := codeEmbCfg.ProviderCfg("")
	if rt.queryProvidersOnly {
		if noteCfg := rt.embCfg.Load(); noteCfg != nil && queryEmbeddingConfigsCompatible(*noteCfg, codeEmbCfg) {
			if noteProvider := rt.queryNoteProvider.Load(); noteProvider != nil {
				codeProvider = *noteProvider
			}
		}
	}
	if codeProvider == nil {
		codeProvider, codeProviderCfg, err = embeddings.NewProviderForConfig(codeEmbCfg, "")
		if err != nil {
			if diagnostics.FromContext(rt.ctx) != nil {
				rt.recordDiagnostic(slog.LevelInfo, "code_provider.unavailable", err)
			}
			return
		}
		if closer, ok := codeProvider.(io.Closer); ok {
			rt.addCloser(func() { _ = closer.Close() })
		}
	}

	if _, err := os.Stat(codeEmbCfg.IndexPath); err != nil {
		if diagnostics.FromContext(rt.ctx) != nil {
			rt.recordDiagnostic(slog.LevelInfo, "code_embeddings.index_unavailable", err)
		}
		return
	}

	ctxReady, cancelReady := context.WithTimeout(rt.ctx, 5*time.Minute)
	defer cancelReady()

	meta := embeddings.MetadataForProvider(codeProvider, codeProviderCfg)
	meta.FingerprintVersion = 2
	if rt.queryProvidersOnly {
		if err := codeemb.ValidateExistingMetadata(rt.ctx, intelStore.DB(), meta); err != nil {
			if diagnostics.FromContext(rt.ctx) != nil {
				rt.recordDiagnostic(slog.LevelInfo, "code_embeddings.metadata_unavailable", err)
			}
			return
		}
		rt.codeEmbProvider.Store(&codeProvider)
		return
	}
	codeEmbStore, err := rt.openCodeEmbeddingsStoreForServe(ctxReady, codeEmbCfg.IndexPath, codeProvider, meta, intelStore)
	if err != nil {
		var metaErr embeddings.MetadataError
		if errors.As(err, &metaErr) {
			if diagnostics.FromContext(rt.ctx) != nil {
				rt.recordDiagnostic(slog.LevelWarn, "code_embeddings.metadata_init_failed", metaErr)
			}
		} else {
			rt.recordDiagnostic(slog.LevelWarn, "code_embeddings.open_failed", err)
		}
		return
	}

	codeEmbStore.SetWriteMu(&rt.sharedWriteMu)
	rt.codeEmbStore.Store(codeEmbStore)
	rt.codeEmbProvider.Store(&codeProvider)
	rt.addCloser(func() {
		if err := codeEmbStore.Close(); err != nil {
			rt.recordDiagnostic(slog.LevelWarn, "code_embeddings.close_failed", err)
		}
	})
}

func queryEmbeddingConfigsCompatible(note, code embeddings.Config) bool {
	return strings.EqualFold(strings.TrimSpace(note.Provider), strings.TrimSpace(code.Provider)) &&
		strings.TrimSpace(note.Model) == strings.TrimSpace(code.Model) &&
		strings.TrimRight(strings.TrimSpace(note.Endpoint), "/") == strings.TrimRight(strings.TrimSpace(code.Endpoint), "/") &&
		note.Dimensions == code.Dimensions
}

func (rt *LiveRuntime) openIntelStoreForServe(codeCfg codeanchor.Config) (*semdb.Store, func(), error) {
	doneStore := indexingperf.StartSpan(indexingperf.WithPhase(rt.ctx, indexingperf.AgentStartPhaseStoreWarmOpen), indexingperf.AgentStartPhaseStoreWarmOpen)
	if rt.readOnlyCodeIndex {
		store, err := semdb.OpenReadOnlyExisting(codeCfg.IndexPath, rt.ctx, liveSQLitePoolOptions())
		doneStore(err)
		if err != nil {
			return nil, nil, err
		}
		return store, func() { _ = store.Close() }, nil
	}
	if embCfg := rt.embCfg.Load(); embCfg != nil && sameSQLitePath(embCfg.IndexPath, codeCfg.IndexPath) {
		if noteStore := rt.noteIndex.Load(); noteStore != nil && noteStore.DB() != nil {
			store, err := semdb.OpenWithDBContext(rt.ctx, noteStore.DB())
			doneStore(err)
			if err != nil {
				return nil, nil, err
			}
			return store, func() {}, nil
		}
	}
	store, cleanup, err := obsidian.OpenIntelStoreForWriteFromConfigWithOptions(rt.VaultPath, codeCfg, obsidian.IntelStoreOpenOptions{
		Context:            rt.ctx,
		Pool:               liveSQLitePoolOptions(),
		SkipIntegrityCheck: rt.skipIntelIntegrityCheck,
	})
	doneStore(err)
	return store, cleanup, err
}

func (rt *LiveRuntime) publishWarmIntelStoreForRead(codeCfg codeanchor.Config) {
	if rt == nil || rt.intelStore.Load() != nil || strings.TrimSpace(codeCfg.IndexPath) == "" {
		return
	}
	store, err := semdb.OpenReadOnlyExisting(codeCfg.IndexPath, rt.ctx, liveSQLitePoolOptions())
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			rt.recordDiagnostic(slog.LevelInfo, "intel_store.warm_open_failed", err)
		}
		return
	}
	if !rt.intelStore.CompareAndSwap(nil, store) {
		_ = store.Close()
		return
	}
	rt.addCloser(func() {
		if err := store.Close(); err != nil {
			rt.recordDiagnostic(slog.LevelWarn, "intel_store.warm_close_failed", err)
		}
	})
}

func (rt *LiveRuntime) openCodeEmbeddingsStoreForServe(ctx context.Context, indexPath string, provider embeddings.Provider, meta embeddings.IndexMetadata, intelStore *semdb.Store) (*codeemb.Store, error) {
	if codeCfg := rt.codeCfg.Load(); codeCfg != nil && sameSQLitePath(indexPath, codeCfg.IndexPath) && intelStore != nil && intelStore.DB() != nil {
		return codeemb.OpenWithMetadataWithDB(ctx, intelStore.DB(), provider, meta)
	}
	return codeemb.OpenWithMetadataWithOptions(ctx, indexPath, provider, meta, codeemb.OpenOptions{
		Pool: liveSQLitePoolOptions(),
	})
}

func sameSQLitePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// createSyncers creates note and code syncers for leader work.
func (rt *LiveRuntime) createSyncers(intelStore *semdb.Store) {
	policy := semanticruntime.PolicyFor(semanticruntime.IndexRequest{
		Source:       semanticruntime.IndexSourceWatcher,
		LatencyClass: semanticruntime.LatencyAuto,
	})
	runtime := semanticruntime.New()
	// Create code syncer if code embeddings are available
	codeEmbStore := rt.codeEmbStore.Load()
	codeProvider := rt.codeEmbProvider.Load()
	codeEmbCfg := rt.codeEmbCfg.Load()
	var codeSyncer *semantic.Syncer
	if codeEmbStore != nil && codeProvider != nil && codeEmbCfg != nil && intelStore != nil {
		codeSyncer = &semantic.Syncer{
			Index:           codeEmbStore,
			Provider:        *codeProvider,
			ProviderInfo:    codeEmbCfg.ProviderCfg(embeddings.ResolveAPIKeyForProvider(codeEmbCfg.Provider)),
			Intel:           intelStore,
			Calls:           intelStore,
			ChunkWriter:     intelStore,
			EmbeddingWriter: intelStore,
			Root:            rt.VaultPath,
			Budget:          semantic.DefaultChunkBudget(),
			Policy:          semantic.DefaultSynthesisPolicy(semantic.PolicyOptions{Budget: semantic.DefaultChunkBudget()}),
			CodeEmbedPacker: policy.CodeEmbedPacker,
		}
		if codeEmbCfg.BatchSize > 0 {
			codeSyncer.BatchSize = codeEmbCfg.BatchSize
		}
		if codeEmbCfg.MaxConcurrency > 0 {
			codeSyncer.MaxConcurrent = codeEmbCfg.MaxConcurrency
		}
	}

	// Create note syncer if note embeddings are available
	noteIndex := rt.noteIndex.Load()
	noteProvider := rt.noteProvider.Load()
	embCfg := rt.embCfg.Load()
	noteSvc := rt.codeAnchorSvc.Load()
	var noteSyncer *semantic.NoteSyncer
	if noteIndex != nil && noteProvider != nil && embCfg != nil && noteSvc != nil && intelStore != nil {
		noteSyncer = &semantic.NoteSyncer{
			Index:           noteIndex,
			Provider:        *noteProvider,
			ProviderInfo:    embCfg.ProviderCfg(embeddings.ResolveAPIKeyForProvider(embCfg.Provider)),
			Intel:           intelStore,
			ChunkWriter:     intelStore,
			EmbeddingWriter: intelStore,
			NoteReader:      &obsidian.Note{},
			BatchSize:       embCfg.BatchSize,
			MaxConcurrent:   embCfg.MaxConcurrency,
			MaxSectionBytes: embCfg.MaxSectionBytes,
			NoteEmbedPacker: policy.NoteEmbedPacker,
			RawEligibility:  semantic.NewOntologyRawNoteEligibility(intelStore),
		}
	}
	if codeSyncer != nil && noteSyncer != nil {
		sharedMax := max(
			semantic.EffectiveMaxConcurrent(codeSyncer.Provider, codeSyncer.MaxConcurrent),
			semantic.EffectiveMaxConcurrent(noteSyncer.Provider, noteSyncer.MaxConcurrent),
		)
		sharedGate := make(chan struct{}, sharedMax)
		codeSyncer.EmbedGate = sharedGate
		noteSyncer.EmbedGate = sharedGate
	}
	physicalCtx, finishPhysical := rt.ctx, func() {}
	if noteSyncer != nil || codeSyncer != nil {
		physicalCtx, finishPhysical = observePhysicalEmbeddings(rt.ctx)
	}
	if noteSyncer != nil && noteSyncer.Provider != nil {
		opts := semantic.EmbedPackerOptions{}
		if noteSyncer.NoteEmbedPacker != nil {
			opts = *noteSyncer.NoteEmbedPacker
		}
		lane, err := runtime.EnsureLane(physicalCtx, semanticruntime.LaneRequest{
			Kind:          semanticruntime.LaneKindNote,
			Provider:      noteSyncer.Provider,
			ProviderInfo:  noteSyncer.ProviderInfo,
			MaxConcurrent: noteSyncer.MaxConcurrent,
			BatchSize:     noteSyncer.BatchSize,
			EmbedGate:     noteSyncer.EmbedGate,
			Packer:        opts,
		})
		if err != nil {
			if diagnostics.FromContext(rt.ctx) != nil {
				rt.recordDiagnostic(slog.LevelWarn, "note_semantic.lane_unavailable", err)
			}
		} else {
			noteSyncer.NoteEmbedPacker = &lane.Packer
			noteSyncer.EmbeddingNode = lane.Node
		}
	}
	if codeSyncer != nil && codeSyncer.Provider != nil {
		opts := semantic.EmbedPackerOptions{}
		if codeSyncer.CodeEmbedPacker != nil {
			opts = *codeSyncer.CodeEmbedPacker
		}
		lane, err := runtime.EnsureLane(physicalCtx, semanticruntime.LaneRequest{
			Kind:          semanticruntime.LaneKindCode,
			Provider:      codeSyncer.Provider,
			ProviderInfo:  codeSyncer.ProviderInfo,
			MaxConcurrent: codeSyncer.MaxConcurrent,
			BatchSize:     codeSyncer.BatchSize,
			EmbedGate:     codeSyncer.EmbedGate,
			Packer:        opts,
		})
		if err != nil {
			if diagnostics.FromContext(rt.ctx) != nil {
				rt.recordDiagnostic(slog.LevelWarn, "code_semantic.lane_unavailable", err)
			}
		} else {
			codeSyncer.CodeEmbedPacker = &lane.Packer
			codeSyncer.EmbeddingNode = lane.Node
		}
	}
	if codeSyncer != nil && codeSyncer.EmbeddingNode != nil {
		rt.codeSyncer.Store(codeSyncer)
	}
	if noteSyncer != nil && noteSyncer.EmbeddingNode != nil {
		rt.noteSyncer.Store(noteSyncer)
	}
	if (codeSyncer != nil && codeSyncer.EmbeddingNode != nil) || (noteSyncer != nil && noteSyncer.EmbeddingNode != nil) {
		rt.addCloser(func() {
			runtime.Close()
			finishPhysical()
		})
	} else {
		finishPhysical()
	}
}

// observeCapabilityPhase records readiness independently for each async phase.
// It never waits or turns a degraded optional capability into a startup failure.
func (rt *LiveRuntime) observeCapabilityPhase(phase string) func() {
	recorder := diagnostics.FromContext(rt.ctx)
	if recorder == nil {
		return func() {}
	}
	op := diagnostics.NewOperation("runtime.capability", phase)
	parent := diagnostics.OperationFromContext(rt.ctx)
	op.ParentID = parent.ID
	if parent.TraceID != "" {
		op.TraceID = parent.TraceID
	}
	ctx := diagnostics.WithOperation(rt.ctx, op)
	recorder.Event(ctx, slog.LevelInfo, "runtime", "capability.started", "", slog.String("capability", phase))
	return func() {
		snap := rt.Snapshot()
		ready := false
		var err error
		switch phase {
		case "search":
			ready, err = snap.Search.Ready, snap.Search.Err
		case "semantic":
			ready, err = snap.Semantic.Ready, snap.Semantic.Err
		case "code":
			ready, err = snap.Code.Ready, snap.Code.Err
		case "leader":
			ready = snap.Leader
		}
		status, reason := "success", ""
		if rt.ctx.Err() != nil {
			status = "canceled"
			reason = logging.ClassifyError(rt.ctx.Err())
		} else if errors.Is(err, ErrCapabilityNotRequested{}) {
			status = "skipped"
			reason = "capability_not_requested"
		} else if errors.Is(err, ErrEmbeddingsDisabled) {
			status = "skipped"
			reason = "embeddings_disabled"
		} else if phase == "code" && rt.readOnlyCodeIndex && errors.Is(err, os.ErrNotExist) {
			status = "skipped"
			reason = "index_missing"
		} else if logging.ClassifyError(err) == "not_configured" {
			status = "skipped"
			reason = "not_configured"
		} else if err != nil {
			status = "error"
			reason = logging.ClassifyError(err)
		} else if !ready {
			status = "skipped"
			reason = "capability_not_enabled"
		}
		// Readiness transitions are frequent lifecycle facts. Keep their duration
		// and outcome in events without an atomic report for every CLI phase.
		logging.CompleteEventQuiet(ctx, op, status, reason, map[string]any{"capability": phase, "ready": ready, "leader": snap.Leader, "watch_hub": snap.WatchHub != nil, "note_semantic": snap.NoteIndex != nil && snap.NoteProvider != nil, "code_semantic": snap.CodeIndex != nil && snap.CodeProvider != nil})
	}
}

func (rt *LiveRuntime) recordDiagnostic(level slog.Level, name string, err error, attrs ...slog.Attr) {
	recorder := diagnostics.FromContext(rt.ctx)
	if recorder == nil {
		return
	}
	message := ""
	if err != nil {
		attrs = append(attrs, slog.String("reason_code", logging.ClassifyError(err)))
		message = name + ": " + logging.ClassifyError(err)
	}
	recorder.Event(rt.ctx, level, "runtime", name, message, attrs...)
}
