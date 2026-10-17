package watchhub

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"log"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type Source string

const (
	SourceFSNotify Source = "fsnotify"
	SourceHint     Source = "hint"
)

const rhizomeDirName = ".rhizome"

type Op uint32

const (
	OpCreate Op = 1 << iota
	OpWrite
	OpRemove
	OpRename
	OpChmod
)

func (o Op) Has(mask Op) bool {
	return o&mask != 0
}

type WatchEvent struct {
	RelPath string
	AbsPath string
	Op      Op
	IsDir   bool
	Root    string
	Source  Source
}

type StaleReason string

const (
	StaleWatcherError  StaleReason = "watcher_error"
	StaleOverflow      StaleReason = "overflow"
	StaleIgnoreChanged StaleReason = "ignore_changed"
	StaleDirRenamed    StaleReason = "dir_renamed"
	StaleDirCreated    StaleReason = "dir_created"
	StaleHintResync    StaleReason = "hint_resync"
)

type StaleEvent struct {
	Reason StaleReason
	Source Source
	Path   string
}

type Handler func(context.Context, []WatchEvent)

type StaleHandler func(context.Context, StaleEvent)

type Options struct {
	Debounce         time.Duration
	Deduplicate      bool
	EventBuffer      int
	PendingAddBuffer int
	StaleMinInterval time.Duration // <=0 uses default, <0 disables stale coalescing
	DisableFSNotify  bool
	ForceFSNotify    bool
	UserExcludes     []string
	Logger           *log.Logger
	Now              func() time.Time
	Debug            bool
}

type subscription struct {
	name    string
	filter  Filter
	handler Handler
	onStale StaleHandler
	active  atomic.Bool
}

type Hub struct {
	vault        paths.VaultPaths
	vaultRoot    string
	ignoreMu     sync.RWMutex
	ignore       *ignore.Matcher
	hardIgnore   *ignore.Matcher
	userExcludes []string
	logger       *log.Logger
	debug        bool
	backend      Backend
	backendMu    sync.RWMutex
	backendCh    chan struct{}
	fseventsMu   sync.Mutex
	fseventsTry  int
	fseventsWait bool
	fsEnabled    bool
	eventBuffer  int
	pendingAdd   chan string
	pendingStop  chan struct{}
	readyMu      sync.RWMutex
	readyAfter   time.Time
	watchInstall int
	pendingWork  int
	roots        *RootRegistry
	debounce     time.Duration
	dedupe       bool
	pendingMu    sync.Mutex
	pending      map[string]WatchEvent
	dispatchWake chan struct{}
	subsMu       sync.RWMutex
	subs         map[string]*subscription
	staleMu      sync.Mutex
	staleLast    map[StaleReason]time.Time
	staleMin     time.Duration
	now          func() time.Time
	lifecycleMu  sync.Mutex
	closed       bool
	closeOnce    sync.Once
	closeErr     error
	work         sync.WaitGroup
	ctx          context.Context
	cancel       context.CancelFunc
}

const defaultStaleMinInterval = 30 * time.Second

func NewHub(vaultPath string, opts Options) (*Hub, error) {
	vault, err := paths.NewVaultPaths(vaultPath)
	if err != nil {
		return nil, err
	}
	root := vault.Root()
	if root == "" {
		return nil, fmt.Errorf("watchhub: empty vault root")
	}
	logger := opts.Logger
	if logger == nil {
		logger = log.Default()
	}
	debounce := opts.Debounce
	if debounce <= 0 {
		debounce = 100 * time.Millisecond
	}
	buf := opts.EventBuffer
	if buf <= 0 {
		buf = defaultBackendEventBuffer
	}
	pendingBuf := opts.PendingAddBuffer
	if pendingBuf <= 0 {
		pendingBuf = 256
	}
	staleMin := opts.StaleMinInterval
	if staleMin == 0 {
		staleMin = defaultStaleMinInterval
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	h := &Hub{
		vault:        vault,
		vaultRoot:    root,
		userExcludes: opts.UserExcludes,
		logger:       logger,
		debug:        opts.Debug,
		fsEnabled:    !opts.DisableFSNotify,
		eventBuffer:  buf,
		pendingAdd:   make(chan string, pendingBuf),
		pendingStop:  make(chan struct{}),
		roots:        NewRootRegistry(),
		debounce:     debounce,
		dedupe:       opts.Deduplicate,
		pending:      make(map[string]WatchEvent),
		dispatchWake: make(chan struct{}, 1),
		subs:         make(map[string]*subscription),
		backendCh:    make(chan struct{}, 1),
		staleLast:    make(map[StaleReason]time.Time),
		staleMin:     staleMin,
		now:          now,
	}
	h.reloadIgnore(opts.UserExcludes)

	if h.fsEnabled {
		var backend Backend
		if opts.ForceFSNotify {
			backend, err = newFSNotifyBackend(buf)
		} else {
			backend, err = newBackend(buf)
		}
		if err != nil {
			return nil, err
		}
		h.setBackend(backend)
	}
	h.work.Add(1)
	go h.dispatchLoop()
	return h, nil
}

func (h *Hub) backendSnapshot() Backend {
	h.backendMu.RLock()
	defer h.backendMu.RUnlock()
	return h.backend
}

func (h *Hub) setBackend(backend Backend) {
	h.backendMu.Lock()
	h.backend = backend
	h.backendMu.Unlock()
	if backend != nil {
		h.roots.SetRecursiveBackend(backend.IsRecursive())
	}
	select {
	case h.backendCh <- struct{}{}:
	default:
	}
}

func (h *Hub) IgnoreMatcher() *ignore.Matcher {
	h.ignoreMu.RLock()
	defer h.ignoreMu.RUnlock()
	return h.ignore
}

// ReplaceUserExcludes atomically replaces the configured user exclusions and
// the matcher used to filter later events. Repository ignore sources remain in
// the unified matcher. Existing subscribers receive an ignore-change stale
// signal so they can reconcile paths whose event eligibility changed.
func (h *Hub) ReplaceUserExcludes(userExcludes []string) {
	h.ignoreMu.Lock()
	h.userExcludes = append([]string(nil), userExcludes...)
	h.ignore = ignore.LoadUnifiedMatcher(h.vaultRoot, h.userExcludes)
	h.ignoreMu.Unlock()
	h.markStale(StaleEvent{Reason: StaleIgnoreChanged, Source: SourceHint})
}

func (h *Hub) Subscribe(name string, filter Filter, handler Handler, onStale StaleHandler) func() {
	sub := &subscription{name: name, filter: filter, handler: handler, onStale: onStale}
	sub.active.Store(true)
	h.subsMu.Lock()
	h.subs[name] = sub
	h.subsMu.Unlock()
	return func() {
		h.subsMu.Lock()
		sub.active.Store(false)
		delete(h.subs, name)
		h.subsMu.Unlock()
	}
}

func (h *Hub) AddRoot(path string, opts RootOptions) error {
	if !h.beginWork() {
		return context.Canceled
	}
	defer h.work.Done()
	clean := paths.ResolveSymlinks(path).String()
	if clean == "" {
		clean = filepath.Clean(path)
	}
	if err := h.roots.Add(clean, opts); err != nil {
		return err
	}
	if opts.WatchForIgnore {
		h.addIgnoreRoot(clean)
	}
	if opts.Kind == RootInternal {
		return nil
	}
	if h.backendSnapshot() != nil {
		if h.roots.IsRecursiveBackend() || opts.Recursive {
			// Recursive backends need one watch per root. Non-recursive backends
			// install per-directory watches by walking the tree once below.
			h.addWatch(clean)
		} else if !h.roots.HasWalked(clean) {
			if err := h.addRecursiveInstall(clean, opts); err == nil {
				h.roots.MarkWalked(clean)
			}
		}
	}
	return nil
}

func (h *Hub) RemoveRoot(path string) {
	h.roots.Remove(path)
}
