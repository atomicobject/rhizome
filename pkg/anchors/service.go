package codeanchor

// Docs:
// - [Code anchors (Hub)](docs/hubs/Code anchors (Hub).md)
// - [Code anchors - matching + scopes](docs/reference/analysis/code-anchors-matching-scopes.md)
// - [Code Index (Hub)](docs/hubs/Code Index (Hub).md)
// - [Indexing pipeline - Live updating (watcher runtime)](docs/reference/analysis/Indexing pipeline - Live updating (watcher runtime).md)

import (
	"container/list"
	"sync"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// Service orchestrates indexing, scope computation, and queries.
type Service struct {
	store    Store
	indexers map[Lang]LanguageIndexer
	basePath string
	vault    paths.VaultPaths
	tailIdx  *PathTailIndex
	linker   CodeDocLinker

	cacheMu         sync.Mutex
	cacheGeneration uint64
	fileCtxCache    map[string]*list.Element
	fileCtxList     *list.List
	cacheLimit      int
	dirtyAnchors    map[int64]bool
	dirtyOverflow   bool
	dirtyMu         sync.Mutex
	cacheHits       uint64
	cacheMisses     uint64

	// recomputeMu serializes RecomputeAnchorScopes to prevent concurrent
	// recomputes and avoid races with anchor modifications.
	recomputeMu sync.Mutex
	// callEdgesRebuilt tracks whether we've attempted a full call-edge rebuild
	// to fill in missing call-site edges after bulk indexing.
	callEdgesRebuilt bool

	scopeProbeOnce sync.Once
	scopeProbeMu   sync.Mutex
	hasAnyScopes   bool

	warmMu      sync.Mutex
	warmRunning bool

	runtimeCodeMu       sync.RWMutex
	runtimeCodeSources  map[string]runtimeCodeIndexSource
	runtimeCallFiles    map[string]map[string]struct{}
	runtimeCodeVersions map[string]uint64
	runtimeCodeSeq      uint64

	// writeAccess enables index updates (scope recompute, call edge rebuild, warming).
	// By default, services are read-only. Use WithWriteAccess() for indexing commands.
	writeAccess bool
}

// NewService constructs a Service with the given store and indexers.
func NewService(store Store, indexers ...LanguageIndexer) *Service {
	return NewServiceWithOptions(store, indexers)
}

// NewServiceWithOptions allows configuring cache warmup and other settings.
func NewServiceWithOptions(store Store, indexers []LanguageIndexer, opts ...ServiceOption) *Service {
	cfg := defaultServiceOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	idxMap := make(map[Lang]LanguageIndexer)
	for _, idx := range indexers {
		if idx == nil {
			continue
		}
		idxMap[idx.Lang()] = idx
	}
	vaultPaths, _ := paths.NewVaultPaths(cfg.basePath)
	basePath := normalizeBasePath(cfg.basePath)
	if vaultPaths.Root() != "" {
		basePath = vaultPaths.Root()
	}
	svc := &Service{
		store:               store,
		indexers:            idxMap,
		basePath:            basePath,
		vault:               vaultPaths,
		tailIdx:             cfg.tailIdx,
		fileCtxCache:        make(map[string]*list.Element),
		fileCtxList:         list.New(),
		cacheLimit:          128,
		dirtyAnchors:        make(map[int64]bool),
		linker:              cfg.linker,
		writeAccess:         cfg.writeAccess,
		runtimeCodeSources:  make(map[string]runtimeCodeIndexSource),
		runtimeCallFiles:    make(map[string]map[string]struct{}),
		runtimeCodeVersions: make(map[string]uint64),
	}
	return svc
}
