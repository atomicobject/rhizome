package watchhub

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
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

type Filter struct {
	Prefixes      []string
	Extensions    []string
	IncludeDirs   bool
	IncludeFiles  bool
	IncludeHidden bool
	OpMask        Op
}

func (f Filter) Matches(ev WatchEvent) bool {
	if ev.RelPath == "" {
		return false
	}
	if !f.IncludeHidden && isHidden(ev.RelPath) {
		return false
	}
	if f.OpMask != 0 && !ev.Op.Has(f.OpMask) {
		return false
	}
	if !f.IncludeDirs && ev.IsDir {
		return false
	}
	if !f.IncludeFiles && !ev.IsDir {
		return false
	}
	if len(f.Prefixes) > 0 {
		matched := false
		for _, prefix := range f.Prefixes {
			if prefix == "" {
				continue
			}
			if ev.RelPath == prefix || strings.HasPrefix(ev.RelPath, prefix+"/") {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(f.Extensions) > 0 && !ev.IsDir {
		ext := strings.ToLower(filepath.Ext(ev.RelPath))
		matched := false
		for _, allowed := range f.Extensions {
			if strings.ToLower(allowed) == ext {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

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
	flushTimerMu sync.Mutex
	flushTimer   *time.Timer
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

func (h *Hub) EmitHintPaths(paths []string) {
	hints := make([]HintPath, 0, len(paths))
	for _, rel := range paths {
		hints = append(hints, HintPath{Path: rel, Kind: "modified"})
	}
	h.EmitHintPathHints(hints)
}

// HintPath describes a vault-relative leader/follower path hint.
type HintPath struct {
	Path string
	Kind string
}

func (h *Hub) EmitHintPathHints(hints []HintPath) {
	for _, hint := range hints {
		rel := strings.TrimSpace(hint.Path)
		if rel == "" {
			continue
		}
		rel = filepath.ToSlash(rel)
		rel = strings.TrimPrefix(rel, "./")
		if rel == "." {
			continue
		}
		if strings.HasPrefix(rel, rhizomeDirName+"/") || rel == rhizomeDirName {
			continue
		}
		op := hintPathOp(hint.Kind)
		if op == 0 {
			continue
		}
		// Hint records are already vault-relative. Reconstruct AbsPath only for
		// subscribers that need to stat/read; filtering and dedupe stay on RelPath.
		ev := WatchEvent{
			RelPath: rel,
			AbsPath: filepath.Join(h.vaultRoot, filepath.FromSlash(rel)),
			Op:      op,
			IsDir:   false,
			Root:    h.vaultRoot,
			Source:  SourceHint,
		}
		if h.shouldFilter(ev, false) {
			continue
		}
		h.enqueue(ev)
	}
}

func hintPathOp(kind string) Op {
	switch strings.TrimSpace(kind) {
	case "created":
		return OpCreate
	case "removed":
		return OpRemove
	case "renamed":
		return OpRename
	case "recreated":
		return OpRemove | OpCreate
	case "modified", "unknown", "":
		return OpWrite
	default:
		return OpWrite
	}
}

func (h *Hub) EmitHintResync() {
	h.reloadIgnore(nil)
	h.markStale(StaleEvent{Reason: StaleHintResync, Source: SourceHint})
}

func (h *Hub) fsLoop(ctx context.Context) {
	if h.debug {
		h.logger.Printf("watchhub: fsLoop started, waiting for events")
	}
	for {
		backend := h.backendSnapshot()
		if backend == nil {
			select {
			case <-ctx.Done():
				return
			case <-h.pendingStop:
				return
			case <-h.backendCh:
				continue
			case <-time.After(50 * time.Millisecond):
				continue
			}
		}
		select {
		case <-ctx.Done():
			if h.debug {
				h.logger.Printf("watchhub: fsLoop stopping (context done)")
			}
			return
		case <-h.pendingStop:
			if h.debug {
				h.logger.Printf("watchhub: fsLoop stopping (pending stop)")
			}
			return
		case <-h.backendCh:
			continue
		case ev, ok := <-backend.Events():
			if !ok {
				if h.backendSnapshot() != backend {
					continue
				}
				if h.debug {
					h.logger.Printf("watchhub: backend events channel closed")
				}
				h.markStale(StaleEvent{Reason: StaleWatcherError, Source: SourceFSNotify})
				return
			}
			h.handleBackendEvent(ev)
		case err, ok := <-backend.Errors():
			if !ok {
				if h.backendSnapshot() != backend {
					continue
				}
				h.markStale(StaleEvent{Reason: StaleWatcherError, Source: SourceFSNotify})
				return
			}
			if h.backendSnapshot() != backend {
				continue
			}
			h.logger.Printf("watchhub: watcher error: %v", err)
			h.markStale(StaleEvent{Reason: StaleWatcherError, Source: SourceFSNotify})
		}
	}
}

func (h *Hub) handleBackendEvent(ev BackendEvent) {
	if h.debug {
		h.logger.Printf("watchhub: backend event: %s op=%v", ev.Path, ev.Op)
	}

	// Handle MustRescan (FSEvents coalescing) - trigger full stale
	if ev.MustRescan {
		h.markStale(StaleEvent{
			Reason: StaleOverflow,
			Source: SourceFSNotify,
			Path:   ev.Path,
		})
		return
	}

	abs := filepath.Clean(ev.Path)
	if abs == "" {
		if h.debug {
			h.logger.Printf("watchhub: dropping event with empty path")
		}
		return
	}
	if shouldDropBackendPath(abs) {
		if h.debug {
			h.logger.Printf("watchhub: dropping internal path: %s", abs)
		}
		return
	}
	if isIgnoreFile(abs) {
		h.reloadIgnore(nil)
		h.roots.ResetWalked()
		h.AddWatchRoots()
		h.markStale(StaleEvent{Reason: StaleIgnoreChanged, Source: SourceFSNotify, Path: abs})
		return
	}

	rel, err := h.vault.RelStrict(abs)
	if err != nil || rel.String() == "" {
		if h.debug {
			h.logger.Printf("watchhub: dropping event outside vault: %s (err=%v)", abs, err)
		}
		return
	}
	if shouldDropVaultRelPath(rel.String()) {
		if h.debug {
			h.logger.Printf("watchhub: dropping ignored vault path: %s", rel.String())
		}
		return
	}
	relPath := paths.NormalizeRelPathAuto(rel.String())
	root := h.roots.MatchRoot(abs)

	// Use IsDir from backend if known, otherwise stat
	isDir := ev.IsDir
	if !ev.IsDirKnown {
		if info, err := os.Stat(abs); err == nil {
			isDir = info.IsDir()
		}
	}

	wev := WatchEvent{
		RelPath: relPath,
		AbsPath: abs,
		Op:      ev.Op,
		IsDir:   isDir,
		Root:    root,
		Source:  SourceFSNotify,
	}

	if h.shouldFilter(wev, isDir) {
		if h.debug {
			h.logger.Printf("watchhub: dropping ignored path: %s", relPath)
		}
		return
	}

	if isDir && !h.roots.IsRecursiveBackend() && wev.Op.Has(OpCreate) {
		h.beginPendingAdd()
		select {
		case h.pendingAdd <- abs:
		default:
			h.finishPendingAdd()
			h.markStale(StaleEvent{Reason: StaleOverflow, Source: SourceFSNotify, Path: abs})
		}
	}

	if wev.Op.Has(OpRemove) || wev.Op.Has(OpRename) {
		wasDir := h.removeWatch(abs)
		if wasDir && !wev.IsDir {
			wev.IsDir = true
		}
		if wasDir && wev.Op.Has(OpRename) {
			h.markStale(StaleEvent{Reason: StaleDirRenamed, Source: SourceFSNotify, Path: abs})
		}
	}

	if h.debug {
		h.logger.Printf("watchhub: file event: %s op=%v", relPath, wev.Op)
	}
	h.enqueue(wev)
}

func (h *Hub) enqueue(ev WatchEvent) {
	if !h.beginWork() {
		return
	}
	defer h.work.Done()
	h.pendingMu.Lock()
	// Use case-insensitive key on Windows/macOS to avoid duplicate entries for
	// the same file. Merge op bits so a rapid remove+create sequence can be
	// interpreted by subscribers as a recreation.
	key := paths.CaseKey(ev.RelPath)
	if existing, ok := h.pending[key]; ok {
		existing.Op |= ev.Op
		if ev.IsDir {
			existing.IsDir = true
		}
		if existing.AbsPath == "" {
			existing.AbsPath = ev.AbsPath
		}
		existing.Source = ev.Source
		h.pending[key] = existing
	} else {
		h.pending[key] = ev
	}
	h.pendingMu.Unlock()

	h.flushTimerMu.Lock()
	if h.flushTimer == nil {
		h.flushTimer = time.AfterFunc(h.debounce, h.flush)
	} else {
		h.flushTimer.Reset(h.debounce)
	}
	h.flushTimerMu.Unlock()
}

func (h *Hub) flush() {
	if !h.beginWork() {
		return
	}
	defer h.work.Done()
	var batch []WatchEvent
	h.pendingMu.Lock()
	if len(h.pending) == 0 {
		h.pendingMu.Unlock()
		return
	}
	batch = make([]WatchEvent, 0, len(h.pending))
	for _, ev := range h.pending {
		batch = append(batch, ev)
	}
	h.pending = make(map[string]WatchEvent)
	h.pendingMu.Unlock()

	h.dispatch(batch)
}

func (h *Hub) dispatch(events []WatchEvent) {
	if !h.beginWork() {
		return
	}
	defer h.work.Done()
	h.subsMu.RLock()
	subs := make([]*subscription, 0, len(h.subs))
	for _, sub := range h.subs {
		subs = append(subs, sub)
	}
	h.subsMu.RUnlock()

	for _, sub := range subs {
		if sub == nil || sub.handler == nil || !sub.active.Load() {
			continue
		}
		filtered := make([]WatchEvent, 0, len(events))
		for _, ev := range events {
			if sub.filter.Matches(ev) {
				filtered = append(filtered, ev)
			}
		}
		if len(filtered) == 0 {
			continue
		}
		if sub.active.Load() {
			sub.handler(h.context(), filtered)
		}
	}
}

func (h *Hub) markStale(ev StaleEvent) {
	if !h.beginWork() {
		return
	}
	defer h.work.Done()
	if h.shouldSuppressStale(ev) {
		if h.debug {
			h.logger.Printf("watchhub: stale coalesced reason=%s source=%s path=%s", ev.Reason, ev.Source, ev.Path)
		}
		return
	}
	h.subsMu.RLock()
	subs := make([]*subscription, 0, len(h.subs))
	for _, sub := range h.subs {
		subs = append(subs, sub)
	}
	h.subsMu.RUnlock()

	for _, sub := range subs {
		if sub == nil || sub.onStale == nil || !sub.active.Load() {
			continue
		}
		if sub.active.Load() {
			sub.onStale(h.context(), ev)
		}
	}
}

func (h *Hub) shouldSuppressStale(ev StaleEvent) bool {
	cooldown := h.staleCooldownFor(ev.Reason)
	if cooldown <= 0 {
		return false
	}
	now := h.now()
	h.staleMu.Lock()
	defer h.staleMu.Unlock()
	if last, ok := h.staleLast[ev.Reason]; ok && now.Sub(last) < cooldown {
		return true
	}
	h.staleLast[ev.Reason] = now
	return false
}

func (h *Hub) staleCooldownFor(reason StaleReason) time.Duration {
	switch reason {
	case StaleIgnoreChanged, StaleDirRenamed, StaleDirCreated:
		return 0
	default:
		return h.staleMin
	}
}

func (h *Hub) reloadIgnore(userExcludes []string) {
	h.ignoreMu.Lock()
	defer h.ignoreMu.Unlock()
	if userExcludes != nil {
		h.userExcludes = append([]string(nil), userExcludes...)
	}
	h.ignore = ignore.LoadUnifiedMatcher(h.vaultRoot, h.userExcludes)
	h.hardIgnore = ignore.LoadUnifiedMatcher(h.vaultRoot, nil)
}

func (h *Hub) shouldFilter(ev WatchEvent, isDir bool) bool {
	h.ignoreMu.RLock()
	matcher := h.ignore
	hardMatcher := h.hardIgnore
	h.ignoreMu.RUnlock()
	if !isDir && ignore.IsSystemContextPath(ev.RelPath) {
		return ignore.IsDefaultInfrastructurePath(ev.RelPath) || (hardMatcher != nil && hardMatcher.IsIgnored(ev.RelPath, false))
	}
	if matcher == nil || !matcher.IsIgnored(ev.RelPath, isDir) {
		return false
	}
	return !isDir || hardMatcher == nil || hardMatcher.IsIgnored(ev.RelPath, true)
}

func (h *Hub) addWatch(path string) {
	backend := h.backendSnapshot()
	if backend == nil {
		return
	}
	if path == "" {
		return
	}
	clean := filepath.Clean(path)
	if !h.roots.TrackWatch(clean) {
		return
	}
	if err := backend.AddPath(clean); err != nil {
		if h.maybeFallbackBackend(err) {
			return
		}
		h.roots.UntrackWatch(clean)
		h.markStale(StaleEvent{Reason: StaleWatcherError, Source: SourceFSNotify, Path: clean})
		h.logger.Printf("watchhub: add watch %s failed: %v", clean, err)
		return
	}
	h.noteReadyAfter(backend)
	if h.debug {
		h.logger.Printf("watchhub: added watch: %s", clean)
	}
}

func (h *Hub) removeWatch(path string) bool {
	backend := h.backendSnapshot()
	if backend == nil {
		return false
	}
	clean := filepath.Clean(path)
	removed := h.roots.UntrackWatchTree(clean)
	if len(removed) == 0 {
		return false
	}
	for _, watched := range removed {
		_ = backend.RemovePath(watched)
	}
	h.noteReadyAfter(backend)
	return true
}

func (h *Hub) AddWatchRoots() {
	if !h.beginWork() {
		return
	}
	defer h.work.Done()
	h.beginWatchInstall()
	defer h.finishWatchInstall()

	roots := h.roots.List()
	for _, root := range roots {
		if h.isStopping() {
			return
		}
		if root.Path == "" {
			continue
		}
		if root.Options.Kind == RootInternal && !shouldWatchInternalRoot(root.Path) {
			continue
		}
		if h.roots.IsRecursiveBackend() || root.Options.Recursive {
			h.addWatch(root.Path)
			continue
		}
		if h.roots.HasWalked(root.Path) {
			continue
		}
		if err := h.addRecursive(root.Path, root.Options); err != nil {
			h.logger.Printf("watchhub: add root %s failed: %v", root.Path, err)
			continue
		}
		// Only mark as walked if we actually have a backend to add watches to.
		// Otherwise we'd skip re-walking when EnableFSNotify() creates the backend later.
		if h.backendSnapshot() != nil {
			h.roots.MarkWalked(root.Path)
		}
	}
	// Log total watch count for diagnostics
	if backend := h.backendSnapshot(); backend != nil && h.debug {
		h.logger.Printf("watchhub: watching %d paths", len(backend.WatchList()))
	}
}

func (h *Hub) addRecursiveInstall(root string, opts RootOptions) error {
	h.beginWatchInstall()
	defer h.finishWatchInstall()
	return h.addRecursive(root, opts)
}

func (h *Hub) addRecursive(root string, opts RootOptions) error {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil
	}
	if h.debug {
		h.logger.Printf("watchhub: walking directory tree from: %s", root)
	}
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if h.isStopping() {
			return context.Canceled
		}
		if err != nil {
			return nil
		}
		if d.IsDir() {
			entry := h.roots.matchRoot(path)
			if entry.Path == "" {
				entry = RootEntry{Path: paths.NormalizeAbsPathForCompare(root), Options: opts}
			}
			if !entry.includesDirectory(path) {
				if path != root {
					if h.debug {
						h.logger.Printf("watchhub: skipping hidden dir: %s", path)
					}
					return filepath.SkipDir
				}
			}
			rel, relErr := h.vault.RelStrict(path)
			if relErr == nil && rel.String() != "" {
				if h.shouldFilter(WatchEvent{RelPath: paths.NormalizeRelPathAuto(rel.String())}, true) {
					if path != root {
						if h.debug {
							h.logger.Printf("watchhub: skipping ignored dir: %s", path)
						}
						return filepath.SkipDir
					}
				}
			}
			h.addWatch(path)
		}
		return nil
	})
}

func (h *Hub) isStopping() bool {
	if h == nil {
		return true
	}
	if ctx := h.context(); ctx != nil {
		select {
		case <-ctx.Done():
			return true
		default:
		}
	}
	select {
	case <-h.pendingStop:
		return true
	default:
		return false
	}
}

func (h *Hub) addIgnoreRoot(vaultRoot string) {
	if strings.TrimSpace(vaultRoot) == "" {
		return
	}
	internalPath := filepath.Join(vaultRoot, rhizomeDirName)
	if info, err := os.Stat(internalPath); err != nil || !info.IsDir() {
		return
	}
	internalOpts := RootOptions{
		Kind:          RootInternal,
		IncludeHidden: true,
	}
	_ = h.roots.Add(internalPath, internalOpts)
	if h.backendSnapshot() == nil {
		return
	}
	if h.roots.IsRecursiveBackend() {
		h.addWatch(internalPath)
		return
	}
	if h.roots.HasWalked(internalPath) {
		return
	}
	if err := h.addRecursiveInstall(internalPath, internalOpts); err == nil {
		h.roots.MarkWalked(internalPath)
	}
}

func isHidden(path string) bool {
	base := filepath.Base(path)
	return strings.HasPrefix(base, ".")
}

func (h *Hub) beginWatchInstall() {
	h.readyMu.Lock()
	h.watchInstall++
	h.readyMu.Unlock()
}

func (h *Hub) finishWatchInstall() {
	backend := h.backendSnapshot()
	h.readyMu.Lock()
	if h.watchInstall > 0 {
		h.watchInstall--
	}
	h.bumpReadyAfterLocked(backend)
	h.readyMu.Unlock()
}

func (h *Hub) beginPendingAdd() {
	h.readyMu.Lock()
	h.pendingWork++
	h.readyMu.Unlock()
}

func (h *Hub) finishPendingAdd() {
	backend := h.backendSnapshot()
	h.readyMu.Lock()
	if h.pendingWork > 0 {
		h.pendingWork--
	}
	h.bumpReadyAfterLocked(backend)
	h.readyMu.Unlock()
}

func (h *Hub) noteReadyAfter(backend Backend) {
	h.readyMu.Lock()
	h.bumpReadyAfterLocked(backend)
	h.readyMu.Unlock()
}

func (h *Hub) readinessSnapshot() (watchInstall int, pendingWork int, pendingQueued int, readyAfter time.Time) {
	h.readyMu.RLock()
	defer h.readyMu.RUnlock()
	return h.watchInstall, h.pendingWork, len(h.pendingAdd), h.readyAfter
}

func (h *Hub) bumpReadyAfterLocked(backend Backend) {
	readyAt := h.now().Add(backendReadySettle(backend))
	if readyAt.After(h.readyAfter) {
		h.readyAfter = readyAt
	}
}

func backendReadySettle(backend Backend) time.Duration {
	if backendIsFSEvents(backend) {
		return time.Second
	}
	if _, ok := backend.(*fsnotifyBackend); ok {
		return 50 * time.Millisecond
	}
	return 0
}

func isIgnoreFile(path string) bool {
	base := filepath.Base(path)
	dir := filepath.Base(filepath.Dir(path))
	if dir == rhizomeDirName && base == "ignore" {
		return true
	}
	return base == ".obsidianignore" || base == ".gitignore"
}

func shouldWatchInternalRoot(path string) bool {
	return filepath.Base(filepath.Clean(path)) == rhizomeDirName
}
