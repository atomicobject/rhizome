package bootstrap

import (
	"context"
	"sync"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
)

const (
	defaultWatcherTickInterval  = 3 * time.Second
	throughputWatcherBurstCount = 32
)

// unifiedSemanticWatcher coordinates ownership batches, embedding sync, and
// graph scores from file system changes. It decides *what* work is needed and
// debounces it; the runtime's indexing lane decides *when* that work runs and
// owns .rhizome/index.lock (SPEC-0104 US3).
type unifiedSemanticWatcher struct {
	// runCtx is the watcher's lifetime. Work submitted to the lane runs under
	// the lane's own job context instead, so an explicit index or an external
	// priority request can cancel it.
	runCtx              context.Context
	vaultPath           string
	vaultDef            obsidian.VaultDefinition
	noteRuntime         noteformat.Runtime
	codeCfg             codeanchor.Config
	noteMetadataIndexer notemeta.Indexer
	cacheService        *cache.Service
	watchHub            *watchhub.Hub
	noteSvc             *codeanchor.Service
	intelStore          *semdb.Store
	noteSyncer          *semantic.NoteSyncer
	nodeSyncer          *semantic.OntologyNodeSyncer
	codeSyncer          *semantic.Syncer
	lane                lane.Lane
	health              *liveHealthTracker
	debug               bool
	opts                UnifiedSemanticWatcherOptions

	noteSemanticStore bool
	codeSemanticStore bool

	pendingMu    sync.Mutex
	pendingByRel map[string]cache.DirtyKind
	// eventCount numbers filesystem events per path, so a verified source
	// stat stays trusted only until the next event for it (live_sync.go).
	eventCount     map[string]uint64
	pendingResync  bool
	pendingReasons map[string]int64
	ownershipWake  chan struct{}
	watcherDone    chan struct{}
	derived        *derivedScheduler
	stopWatchHub   func()

	validationMu       sync.Mutex
	validationTimer    *time.Timer
	validationDeadline time.Time

	onNoteSelectionChanged func(cache.SelectionPolicy)
	onProcessedPath        func(rel string)
	publishEvent           func(kind string, data any)
	// Test-only failure seam: production leaves this nil. It runs after an
	// effective prepared-projection ownership transition commits, before
	// destinations rebuild.
	afterPreparedOwnershipCommit func(context.Context) error
}

type internalChangeEffect struct {
	reloadVaultDef         bool
	refreshMeta            bool
	refreshOntology        bool
	invalidateQueryRecipes bool
	invalidateCapabilities bool
}

// WatcherDeps holds dependencies for starting the unified semantic watcher.
type WatcherDeps struct {
	VaultDef            obsidian.VaultDefinition
	NoteRuntime         noteformat.Runtime
	CodeConfig          codeanchor.Config
	NoteMetadataIndexer notemeta.Indexer
	CacheService        *cache.Service
	WatchHub            *watchhub.Hub
	NoteSvc             *codeanchor.Service
	IntelStore          *semdb.Store
	NoteSyncer          *semantic.NoteSyncer
	NodeSyncer          *semantic.OntologyNodeSyncer
	CodeSyncer          *semantic.Syncer
	// SemanticStore flags mean the semantic domain is configured enabled. A nil
	// syncer retains recovery debt even if opening the destination store failed.
	NoteSemanticStore bool
	CodeSemanticStore bool
	// Lane serializes every indexing job and owns the index lock. Without one
	// the watcher observes changes but performs no indexing work.
	Lane         lane.Lane
	Health       *liveHealthTracker
	Options      *UnifiedSemanticWatcherOptions
	PublishEvent func(kind string, data any)
	// OnNoteSelectionChanged publishes the same immutable selection policy to
	// read-side consumers after a live include/exclude/config reload.
	OnNoteSelectionChanged func(cache.SelectionPolicy)
}

// UnifiedSemanticWatcherOptions controls watcher loop timing.
// Primarily intended for deterministic tests; production uses defaults.
type UnifiedSemanticWatcherOptions struct {
	TickInterval time.Duration
	Now          func() time.Time
	Tick         <-chan time.Time
}

func (o UnifiedSemanticWatcherOptions) normalize() UnifiedSemanticWatcherOptions {
	out := o
	if out.TickInterval <= 0 {
		out.TickInterval = defaultWatcherTickInterval
	}
	if out.Now == nil {
		out.Now = time.Now
	}
	return out
}

// StartUnifiedSemanticWatcher starts the background watcher for semantic indexing.
// It coordinates embedding sync and graph score computation based on file changes.
func StartUnifiedSemanticWatcher(ctx context.Context, vaultPath string, deps WatcherDeps, debug bool) *unifiedSemanticWatcher {
	opts := UnifiedSemanticWatcherOptions{}
	if deps.Options != nil {
		opts = *deps.Options
	}
	opts = opts.normalize()

	w := &unifiedSemanticWatcher{
		runCtx:                 ctx,
		vaultPath:              vaultPath,
		vaultDef:               deps.VaultDef,
		noteRuntime:            deps.NoteRuntime,
		codeCfg:                deps.CodeConfig,
		noteMetadataIndexer:    deps.NoteMetadataIndexer,
		cacheService:           deps.CacheService,
		watchHub:               deps.WatchHub,
		noteSvc:                deps.NoteSvc,
		intelStore:             deps.IntelStore,
		noteSyncer:             deps.NoteSyncer,
		nodeSyncer:             deps.NodeSyncer,
		codeSyncer:             deps.CodeSyncer,
		lane:                   deps.Lane,
		health:                 deps.Health,
		debug:                  debug,
		opts:                   opts,
		publishEvent:           deps.PublishEvent,
		ownershipWake:          make(chan struct{}, 1),
		watcherDone:            make(chan struct{}),
		onNoteSelectionChanged: deps.OnNoteSelectionChanged,
	}
	w.noteSemanticStore = deps.NoteSemanticStore
	w.codeSemanticStore = deps.CodeSemanticStore

	if w.intelStore != nil && w.lane != nil {
		w.derived = newDerivedScheduler(w)
	}
	w.subscribeWatchHub()
	go w.run()
	return w
}

// schedulerHealth reports active derived work and writer contention.
func (w *unifiedSemanticWatcher) schedulerHealth() LiveSchedulerHealth {
	if w == nil || w.lane == nil {
		return LiveSchedulerHealth{}
	}
	status := w.lane.Status()
	if w.derived == nil {
		return LiveSchedulerHealth{GraphLockContention: status.Held != ""}
	}
	w.derived.mu.Lock()
	running := w.derived.running
	w.derived.mu.Unlock()
	return LiveSchedulerHealth{EmbeddingRunning: running != "" && running != codeanchor.DerivedGraph, GraphRunning: running == codeanchor.DerivedGraph, GraphPending: status.Queued > 0, GraphLockContention: status.Held != ""}
}

func (w *unifiedSemanticWatcher) stopValidationTimer() {
	if w == nil {
		return
	}
	w.validationMu.Lock()
	defer w.validationMu.Unlock()
	if w.validationTimer != nil {
		w.validationTimer.Stop()
		w.validationTimer = nil
	}
	w.validationDeadline = time.Time{}
}

func epochID(epoch *LiveEpoch) int64 {
	if epoch == nil {
		return 0
	}
	return epoch.ID
}
