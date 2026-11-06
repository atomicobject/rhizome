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
	"sync"
	"sync/atomic"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/runtimeview"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/embedindex"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
)

// LiveRuntime owns one vault's watcher, schedulers, and background index.
// Ownership is elected synchronously before its capability phases start.
type LiveRuntime struct {
	resolvedConfig atomic.Pointer[repoconfig.Resolved]
	// Immediately available (set in NewLiveRuntime before it returns)
	Vault     *repoconfig.Vault
	VaultPath string
	VaultDef  obsidian.VaultDefinition
	LocalCfg  *repoconfig.LocalConfig

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
	ctx                 context.Context
	cancelCtx           context.CancelFunc
	debug               bool
	skipCacheWarmup     bool
	disableWatchHub     bool
	disableLeaderWork   bool
	requirements        RuntimeRequirements
	readOnlyCodeIndex   bool
	disableSessionStore bool
	sessionOnlyExisting bool
	queryProvidersOnly  bool

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
	noteIndex     atomic.Pointer[embsqlite.NoteStore]
	noteProvider  atomic.Pointer[embeddings.Provider]
	codeAnchorSvc atomic.Pointer[codeanchor.Service]
	intelStore    atomic.Pointer[semdb.Store]
	sessionStore  atomic.Pointer[sessionStoreHolder]
	sessionReady  chan struct{}

	// Code embeddings (optional, may be nil)
	codeEmbStore    atomic.Pointer[embsqlite.CodeStore]
	codeEmbProvider atomic.Pointer[embeddings.Provider]

	// Syncers for leader work (created when capabilities are ready)
	noteSyncer atomic.Pointer[embedindex.NoteSyncer]
	codeSyncer atomic.Pointer[embedindex.Syncer]

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

func liveSQLitePoolOptions() sqliteutil.Options {
	return sqliteutil.Options{
		MaxOpenConns:    4,
		MaxIdleConns:    1,
		ConnMaxIdleTime: 5 * time.Minute,
	}
}

type sessionStoreHolder struct {
	store semdb.SessionDedupeHandle
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
	Config                 *repoconfig.Resolved
	VaultName              string
	Debug                  bool
	BackgroundIndexer      BackgroundIndexer   // optional callback for background indexing on leader start
	BeforeOwnershipRelease func()              // owner-only finalization after resources drain, before election release
	OnElected              func() error        // owner-only startup barrier; see below
	SkipCacheWarmup        bool                // skip background note-cache warmup for one-shot CLI callers
	DisableWatchHub        bool                // skip watchhub for one-shot CLI callers
	DisableLeaderWork      bool                // skip leader election, watchers, and background work
	Requirements           RuntimeRequirements // requested runtime capabilities (zero value = full runtime)
	ReadOnlyCodeIndex      bool                // query the existing validated index
	DisableSessionStore    bool                // do not expose a session-dedupe handle or own session cleanup
	SessionOnlyExisting    bool                // open only the existing session-dedupe writer, without CodeIndex
	QueryProvidersOnly     bool                // construct query embedders without opening or mutating embedding stores
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
	opts.ReadOnlyCodeIndex = true
	return opts
}

// NewLiveRuntime creates a runtime for a long-running server process.
// Returns immediately after resolving vault config. All heavy initialization
// runs asynchronously. Use WaitFor* methods to await specific capabilities.
//
// Only returns an error for truly fatal conditions (vault doesn't exist).
func NewLiveRuntime(ctx context.Context, opts LiveOptions) (*LiveRuntime, error) {
	ctx = vaultconfig.WithEnv(ctx, vaultconfig.FromContext(ctx))
	runtimeCtx, cancel := context.WithCancel(ctx)
	formats, noteMetadataIndexer, err := NewBuiltinNoteRuntime()
	if err != nil {
		cancel()
		return nil, err
	}
	rt := &LiveRuntime{
		ctx:                    runtimeCtx,
		cancelCtx:              cancel,
		debug:                  opts.Debug,
		skipCacheWarmup:        opts.SkipCacheWarmup,
		disableWatchHub:        opts.DisableWatchHub,
		disableLeaderWork:      opts.DisableLeaderWork,
		requirements:           opts.Requirements,
		readOnlyCodeIndex:      opts.ReadOnlyCodeIndex,
		disableSessionStore:    opts.DisableSessionStore,
		sessionOnlyExisting:    opts.SessionOnlyExisting,
		queryProvidersOnly:     opts.QueryProvidersOnly,
		beforeOwnershipRelease: opts.BeforeOwnershipRelease,
		searchReady:            make(chan struct{}),
		semanticReady:          make(chan struct{}),
		codeReady:              make(chan struct{}),
		leaderCh:               make(chan struct{}),
		noteMetadataIndexer:    noteMetadataIndexer,
		noteFormats:            formats,
		sessionReady:           make(chan struct{}),
	}

	// Capture definition and settings before opening any runtime resources.
	rt.Vault = &repoconfig.Vault{Env: vaultconfig.FromContext(ctx), Name: opts.VaultName}
	vaultDef, resolved, err := rt.Vault.ResolveConfiguration(opts.Config)
	if err != nil {
		cancel()
		return nil, err
	}
	rt.VaultDef = vaultDef
	rt.ownershipVaultDef.Store(&vaultDef)
	rt.VaultPath = vaultDef.BasePath()
	rt.liveHealth = newLiveHealthTracker(rt.VaultPath)
	rt.resolvedConfig.Store(&resolved)
	rt.LocalCfg = resolved.Local

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
