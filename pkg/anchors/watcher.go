package codeanchor

// Docs:
// - [Code anchors (Hub)](docs/hubs/Code anchors (Hub).md)
// - [Code anchors - watcher + incremental updates](docs/reference/analysis/code-anchors-watcher-incremental-updates.md)

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/codefile"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/bmatcuk/doublestar/v4"
	"github.com/fsnotify/fsnotify"
)

var defaultLangExts = map[Lang][]string{
	LangPy:  {".py"},
	LangGo:  {".go"},
	LangTS:  codefile.TypeScriptJavaScriptExtensions(),
	LangCs:  {".cs"},
	LangPhp: {".php"},
}

var defaultExcludeDirs = ignore.DefaultIgnoreDirnames()

type watcherOptions struct {
	excludeDirs       map[string]struct{}
	excludeGlobs      []string
	ignoreMatcher     *ignore.Matcher
	debounceDelay     time.Duration
	reconcileInterval time.Duration // 0 = default (15s); <0 = disabled
	forceReconcile    bool          // force reconciliation on all platforms (for testing)
}

// NoteSourceFactory constructs the canonical raw-note snapshot consumed by
// anchor ingestion. The producer lives outside this package to avoid an import
// cycle with notemeta.
type NoteSourceFactory func(path string, content []byte, mtime int64) NoteSource

type WatcherOption func(*watcherOptions)

// WithWatcherExcludeGlobs adds doublestar glob patterns to skip (in addition to defaults).
// Patterns are matched against:
//   - the absolute path with any leading slash removed (POSIX separators), and
//   - the path relative to each watched root (noteRoots + codeRoots), when possible.
func WithWatcherExcludeGlobs(globs []string) WatcherOption {
	return func(o *watcherOptions) {
		for _, g := range globs {
			g = strings.TrimSpace(g)
			if g == "" {
				continue
			}
			o.excludeGlobs = append(o.excludeGlobs, g)
		}
	}
}

// WithWatcherIgnoreMatcher sets a gitignore-style matcher used to skip paths.
// This is evaluated in addition to excludeDirs and excludeGlobs.
func WithWatcherIgnoreMatcher(m *ignore.Matcher) WatcherOption {
	return func(o *watcherOptions) {
		o.ignoreMatcher = m
	}
}

// WithWatcherDebounce sets the debounce delay for coalescing events.
// Use 0 to process events immediately (no extra debounce).
func WithWatcherDebounce(delay time.Duration) WatcherOption {
	return func(o *watcherOptions) {
		o.debounceDelay = delay
	}
}

// WithWatcherReconcileInterval sets the interval for deletion reconciliation.
// Default is 15 seconds. Use a negative value to disable reconciliation.
func WithWatcherReconcileInterval(interval time.Duration) WatcherOption {
	return func(o *watcherOptions) {
		o.reconcileInterval = interval
	}
}

// WithWatcherForceReconcile forces deletion reconciliation on all platforms,
// including macOS where it's normally not needed. Useful for testing.
func WithWatcherForceReconcile() WatcherOption {
	return func(o *watcherOptions) {
		o.forceReconcile = true
	}
}

func defaultWatcherOptions() watcherOptions {
	opts := watcherOptions{
		excludeDirs:   make(map[string]struct{}),
		debounceDelay: 100 * time.Millisecond,
	}
	for _, ex := range defaultExcludeDirs {
		opts.excludeDirs[ex] = struct{}{}
	}
	return opts
}

// Watcher wires watch events into incremental indexing.
type Watcher struct {
	events            chan fsnotify.Event
	service           *Service
	noteSourceFactory NoteSourceFactory
	ctx               context.Context
	noteRoots         []string
	codeRoots         []string
	noteExts          map[string]struct{}
	langByExt         map[string]Lang
	excludeDirs       map[string]struct{}
	excludeGlobs      []string
	ignoreMu          sync.RWMutex
	ignoreMatcher     *ignore.Matcher
	trackedMu         sync.Mutex
	trackedPaths      map[string]struct{}
	cancelLoop        context.CancelFunc
	drainMu           sync.Mutex
	drainCh           chan struct{}
	pending           bool
	recomputeMu       sync.Mutex
	recomputeTimer    *time.Timer
	recomputeDelay    time.Duration
	debounceDelay     time.Duration
	pendingLimit      int
	reconcileInterval time.Duration // interval for deletion reconciliation; 0 uses default; <0 disables
	forceReconcile    bool          // force reconciliation even on platforms with recursive watching

	// recentlyIndexed tracks paths indexed during directory walks to avoid
	// double-indexing when watch events arrive for the same files.
	recentlyIndexedMu sync.Mutex
	recentlyIndexed   map[string]time.Time

	// failedFiles tracks files that failed to index with retry metadata.
	failedFilesMu sync.Mutex
	failedFiles   map[string]*failedFileEntry

	overflowMu         sync.Mutex
	overflowRescanning bool

	staleRescanMu      sync.Mutex
	staleRescanRunning bool
	staleRescanPending bool
}

type failedFileEntry struct {
	lastAttempt time.Time
	attempts    int
	lastError   string
}

// NewWatcher configures a watcher.
// Roots are absolutized and non-existent roots are skipped.
func NewWatcher(service *Service, noteRoots, codeRoots []string, sourceFactory NoteSourceFactory) (*Watcher, error) {
	return NewWatcherWithOptions(service, noteRoots, codeRoots, sourceFactory)
}

// NewWatcherWithOptions configures a watcher with optional exclusions.
func NewWatcherWithOptions(service *Service, noteRoots, codeRoots []string, sourceFactory NoteSourceFactory, opts ...WatcherOption) (*Watcher, error) {
	return newWatcher(service, noteRoots, codeRoots, sourceFactory, opts...)
}

func newWatcher(service *Service, noteRoots, codeRoots []string, sourceFactory NoteSourceFactory, opts ...WatcherOption) (*Watcher, error) {
	// Absolutize and filter roots
	absNoteRoots := normalizeRoots(noteRoots)
	absCodeRoots := normalizeRoots(codeRoots)
	if len(absNoteRoots) > 0 && sourceFactory == nil {
		return nil, fmt.Errorf("note source factory is required when watching note roots")
	}
	langByExt, skippedExts := supportedLangByExt(service)

	cfg := defaultWatcherOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	w := &Watcher{
		events:            make(chan fsnotify.Event, 4096),
		service:           service,
		noteSourceFactory: sourceFactory,
		noteRoots:         absNoteRoots,
		codeRoots:         absCodeRoots,
		recomputeDelay:    200 * time.Millisecond,
		debounceDelay:     cfg.debounceDelay,
		pendingLimit:      10000,
		reconcileInterval: cfg.reconcileInterval,
		forceReconcile:    cfg.forceReconcile,
		noteExts:          map[string]struct{}{".md": {}},
		langByExt:         langByExt,
		trackedPaths:      make(map[string]struct{}),
		excludeDirs:       cfg.excludeDirs,
		excludeGlobs:      cfg.excludeGlobs,
		ignoreMatcher:     cfg.ignoreMatcher,
		recentlyIndexed:   make(map[string]time.Time),
		failedFiles:       make(map[string]*failedFileEntry),
	}
	if len(skippedExts) > 0 {
		log.Printf("codeanchor watcher: skipping unsupported extensions (no indexer): %s", strings.Join(skippedExts, ", "))
	}

	// Best-effort: populate the path tail index from existing code files.
	// This allows fallback resolution immediately after startup (before any fs events).
	w.populateTailIndex()
	return w, nil
}

func (w *Watcher) handleEventOverflow() {
	if w == nil {
		return
	}
	ctx := w.ctx
	if ctx == nil || ctx.Err() != nil {
		return
	}
	w.overflowMu.Lock()
	if w.overflowRescanning {
		w.overflowMu.Unlock()
		return
	}
	w.overflowRescanning = true
	w.overflowMu.Unlock()

	go func() {
		if err := w.fullRescan(ctx); err != nil && ctx.Err() == nil {
			log.Printf("codeanchor watcher: full rescan after event overflow failed: %v", err)
		}
		w.overflowMu.Lock()
		w.overflowRescanning = false
		w.overflowMu.Unlock()
	}()
}

func (w *Watcher) scheduleStaleRescan(ctx context.Context) {
	if w == nil {
		return
	}
	if ctx == nil {
		ctx = w.ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}

	w.staleRescanMu.Lock()
	if w.staleRescanRunning {
		w.staleRescanPending = true
		w.staleRescanMu.Unlock()
		return
	}
	w.staleRescanRunning = true
	w.staleRescanMu.Unlock()

	go func() {
		for {
			_ = w.fullRescan(ctx)

			w.staleRescanMu.Lock()
			if !w.staleRescanPending {
				w.staleRescanRunning = false
				w.staleRescanMu.Unlock()
				return
			}
			w.staleRescanPending = false
			w.staleRescanMu.Unlock()

			if ctx.Err() != nil {
				w.staleRescanMu.Lock()
				w.staleRescanRunning = false
				w.staleRescanPending = false
				w.staleRescanMu.Unlock()
				return
			}
		}
	}()
}

func (w *Watcher) populateTailIndex() {
	if w == nil || w.service == nil {
		return
	}
	tail := w.service.TailIndex()
	if tail == nil || len(w.codeRoots) == 0 || len(w.langByExt) == 0 {
		return
	}

	for _, root := range w.codeRoots {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if w.shouldSkipDirEntry(path, d) {
				return filepath.SkipDir
			}
			if w.isExcludedPath(path) {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if _, ok := w.langByExt[ext]; ok {
				tail.Add(path)
			}
			return nil
		})
	}
}

// normalizeRoots absolutizes paths and filters out non-existent directories.
func normalizeRoots(roots []string) []string {
	var result []string
	for _, root := range roots {
		abs := paths.ResolveSymlinks(root).String()
		if abs == "" {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			log.Printf("codeanchor watcher: skip missing root %s", root)
			continue
		}
		result = append(result, abs)
	}
	return result
}

// LangForPath returns the language a code file is indexed as, from its
// extension.
func LangForPath(path string) (Lang, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	for lang, exts := range defaultLangExts {
		for _, candidate := range exts {
			if candidate == ext {
				return lang, true
			}
		}
	}
	return "", false
}

func supportedLangByExt(service *Service) (map[string]Lang, []string) {
	langByExt := make(map[string]Lang)
	var skipped []string
	for lang, exts := range defaultLangExts {
		if service != nil && !service.HasIndexer(lang) {
			skipped = append(skipped, exts...)
			continue
		}
		for _, ext := range exts {
			langByExt[ext] = lang
		}
	}
	return langByExt, skipped
}

// UnsupportedExtensions lists file extensions for which no indexer is configured.
func UnsupportedExtensions(service *Service) []string {
	if service == nil {
		return nil
	}
	skipped := make([]string, 0)
	for lang, exts := range defaultLangExts {
		if !service.HasIndexer(lang) {
			skipped = append(skipped, exts...)
		}
	}
	sort.Strings(skipped)
	return skipped
}

// SetIgnoreMatcher updates the ignore matcher used by the watcher.
func (w *Watcher) SetIgnoreMatcher(m *ignore.Matcher) {
	if w == nil {
		return
	}
	w.ignoreMu.Lock()
	w.ignoreMatcher = m
	w.ignoreMu.Unlock()
}

// Start begins watching until context is canceled.
func (w *Watcher) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	w.cancelLoop = cancel
	w.ctx = ctx
	// Initial sync so callers can rely on a populated index.
	// If this is too slow for your use case, call Start in a goroutine.
	if err := w.fullRescan(ctx); err != nil && ctx.Err() == nil {
		log.Printf("codeanchor watcher initial rescan failed (continuing with watch events): %v", err)
	}
	go w.loop(ctx)
	return nil
}

// Close stops the watcher.
func (w *Watcher) Close() error {
	if w.cancelLoop != nil {
		w.cancelLoop()
	}
	w.recomputeMu.Lock()
	if w.recomputeTimer != nil {
		w.recomputeTimer.Stop()
	}
	w.recomputeMu.Unlock()
	return nil
}

func (w *Watcher) loop(ctx context.Context) {
	cleanupTicker := time.NewTicker(30 * time.Second)
	defer cleanupTicker.Stop()

	// Deletion reconciliation ticker: needed on Linux/Windows where fsnotify is non-recursive
	// and directory deletions don't cascade to child files. On macOS (FSEvents), recursive
	// watching handles this natively, so we skip unless forceReconcile is set (for testing).
	var reconcileTicker *time.Ticker
	var reconcileCh <-chan time.Time
	needsReconcile := w.forceReconcile || runtime.GOOS != "darwin"
	if needsReconcile && w.reconcileInterval >= 0 {
		interval := w.reconcileInterval
		if interval == 0 {
			interval = 15 * time.Second
		}
		reconcileTicker = time.NewTicker(interval)
		reconcileCh = reconcileTicker.C
		defer reconcileTicker.Stop()
	}

	debounce := w.debounceDelay
	if debounce < 0 {
		debounce = 100 * time.Millisecond
	}
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	pendingWrite := make(map[string]struct{})
	pendingRemove := make(map[string]struct{})
	defer w.markDrained()

	// checkOverflow replaces an overflowing queue with a full rescan.
	checkOverflow := func() {
		total := len(pendingWrite) + len(pendingRemove)
		if w.pendingLimit > 0 && total > w.pendingLimit {
			log.Printf("codeanchor watcher: pending queue overflow (%d), triggering full rescan", total)
			pendingWrite = make(map[string]struct{})
			pendingRemove = make(map[string]struct{})
			go w.fullRescan(ctx)
		}
	}

	for {
		select {
		case <-cleanupTicker.C:
			w.cleanupRecentlyIndexed()
		case <-reconcileCh:
			go w.reconcileDeleted(ctx)
		case <-ctx.Done():
			return
		case ev, ok := <-w.events:
			if !ok {
				return
			}
			path := filepath.Clean(ev.Name)
			if w.isExcludedPath(path) {
				continue
			}
			if ev.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(path); err == nil && info.IsDir() {
					if w.shouldSkipDir(path, info.Name()) || w.isExcludedPath(path) {
						continue
					}
					go func(dirPath string) {
						_ = filepath.WalkDir(dirPath, func(fpath string, d os.DirEntry, err error) error {
							if err != nil {
								return nil
							}
							if w.shouldSkipDirEntry(fpath, d) {
								return filepath.SkipDir
							}
							if w.isExcludedPath(fpath) {
								return nil
							}
							if d.IsDir() {
								return nil
							}
							w.handlePath(ctx, fpath)
							return nil
						})
						w.scheduleRecompute()
					}(path)
				}
			}
			if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
				pendingRemove[path] = struct{}{}
				delete(pendingWrite, path)
				timer.Reset(debounce)
				w.markPending()
				checkOverflow()
				continue
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write) != 0 {
				pendingWrite[path] = struct{}{}
				delete(pendingRemove, path)
				timer.Reset(debounce)
				w.markPending()
				checkOverflow()
			}
		case <-timer.C:
			hadRemovals := len(pendingRemove) > 0
			// Process removals first
			removedPaths := make([]string, 0, len(pendingRemove))
			for p := range pendingRemove {
				removedPaths = append(removedPaths, p)
			}
			if len(removedPaths) > 0 {
				if len(removedPaths) <= 5 {
					for _, p := range removedPaths {
						log.Printf("watcher: detected remove: %s", p)
					}
				} else {
					log.Printf("watcher: detected removes to %d files", len(removedPaths))
				}
				for _, p := range removedPaths {
					w.handleRemove(ctx, p)
				}
			}
			pendingRemove = make(map[string]struct{})

			// Process writes
			paths := make([]string, 0, len(pendingWrite))
			for p := range pendingWrite {
				paths = append(paths, p)
			}
			if len(paths) > 0 {
				if len(paths) <= 5 {
					for _, p := range paths {
						log.Printf("watcher: detected change: %s", p)
					}
				} else {
					log.Printf("watcher: detected changes to %d files", len(paths))
				}
			}
			pendingWrite = make(map[string]struct{})
			for _, p := range paths {
				w.handlePath(ctx, p)
			}
			if len(paths) > 0 || hadRemovals {
				w.markDrained()
			}
		}
	}
}

func (w *Watcher) fullRescan(ctx context.Context) error {
	current := make(map[string]struct{})
	w.markPending()
	w.service.InvalidateModuleMetadata("")

	walkRoots := append([]string{}, w.noteRoots...)
	walkRoots = append(walkRoots, w.codeRoots...)

	for _, root := range walkRoots {
		if ctx != nil && ctx.Err() != nil {
			w.markDrained()
			return ctx.Err()
		}
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if ctx != nil && ctx.Err() != nil {
				return ctx.Err()
			}
			if w.shouldSkipDirEntry(path, d) {
				return filepath.SkipDir
			}
			if d.IsDir() {
				return nil
			}
			if w.isExcludedPath(path) {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if _, ok := w.noteExts[ext]; ok {
				content, err := os.ReadFile(path)
				if err != nil {
					log.Printf("codeanchor watcher read note %s during rescan: %v", path, err)
					return nil
				}
				source, err := w.noteSource(path, content)
				if err != nil {
					log.Printf("codeanchor watcher resolve note %s during rescan: %v", path, err)
					return nil
				}
				if _, err := w.service.IngestNoteSource(ctx, source); err != nil {
					log.Printf("codeanchor watcher ingest note %s during rescan: %v", path, err)
					return nil
				}
				current[paths.CaseKey(path)] = struct{}{}
				return nil
			}
			if lang, ok := w.langByExt[ext]; ok {
				content, err := os.ReadFile(path)
				if err != nil {
					log.Printf("codeanchor watcher read code %s during rescan: %v", path, err)
					return nil
				}
				ref, err := w.service.codePathRef(path)
				if err != nil {
					log.Printf("codeanchor watcher resolve code path %s during rescan: %v", path, err)
					return nil
				}
				if err := w.service.IndexCodeFileRef(ctx, lang, ref, content); err != nil {
					log.Printf("codeanchor watcher index code %s during rescan: %v", path, err)
					return nil
				}
				current[paths.CaseKey(path)] = struct{}{}
			}
			return nil
		})
		if err != nil && ctx != nil && ctx.Err() != nil {
			w.markDrained()
			return ctx.Err()
		}
	}

	prev := w.snapshotTracked()
	for key := range prev {
		if ctx != nil && ctx.Err() != nil {
			w.markDrained()
			return ctx.Err()
		}
		if _, ok := current[key]; !ok {
			// key is case-normalized; handleRemove normalizes paths internally
			w.handleRemove(ctx, key)
		}
	}
	w.replaceTracked(current)
	// Full rescans are typically performed at startup or after overflow; prefer a
	// synchronous scope recompute so subsequent queries see correct results immediately.
	if err := w.service.RecomputeAnchorScopes(ctx); err != nil && (ctx == nil || ctx.Err() == nil) {
		log.Printf("codeanchor watcher recompute after rescan: %v", err)
	}
	w.markDrained()
	return nil
}

// reconcileDeleted performs a lightweight filesystem walk to detect files that
// were deleted without triggering watch events (e.g., directory deletions on Linux).
// Unlike fullRescan, it only walks to enumerate paths—no file reads or re-indexing.
func (w *Watcher) reconcileDeleted(ctx context.Context) {
	current := make(map[string]struct{})
	walkRoots := append([]string{}, w.noteRoots...)
	walkRoots = append(walkRoots, w.codeRoots...)

	for _, root := range walkRoots {
		if ctx.Err() != nil {
			return
		}
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || ctx.Err() != nil {
				return nil
			}
			if w.shouldSkipDirEntry(path, d) {
				return filepath.SkipDir
			}
			if d.IsDir() || w.isExcludedPath(path) {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if _, ok := w.noteExts[ext]; ok {
				current[paths.CaseKey(path)] = struct{}{}
			} else if _, ok := w.langByExt[ext]; ok {
				current[paths.CaseKey(path)] = struct{}{}
			}
			return nil
		})
	}

	// Find and remove missing paths
	prev := w.snapshotTracked()
	var removed int
	for key := range prev {
		if ctx.Err() != nil {
			return
		}
		if _, ok := current[key]; !ok {
			w.handleRemove(ctx, key)
			removed++
		}
	}
	if removed > 0 {
		log.Printf("codeanchor watcher: reconciliation removed %d stale entries", removed)
	}
}

func (w *Watcher) handlePath(ctx context.Context, path string) {
	if w.isExcludedPath(path) {
		return
	}
	if isModuleMetadataPath(path) {
		w.service.InvalidateModuleMetadata(path)
		return
	}
	// Skip if this path was recently indexed (e.g., during a directory walk)
	// to avoid double-indexing when watch events arrive concurrently.
	if w.wasRecentlyIndexed(path) {
		return
	}
	// Check if this file previously failed and respect backoff
	if !w.shouldRetryFile(path) {
		return
	}
	ext := strings.ToLower(filepath.Ext(path))
	if _, ok := w.noteExts[ext]; ok {
		content, err := os.ReadFile(path)
		if err != nil {
			log.Printf("codeanchor watcher read note %s: %v", path, err)
			w.markFileFailed(path, err)
			return
		}
		source, err := w.noteSource(path, content)
		if err != nil {
			log.Printf("codeanchor watcher resolve note %s: %v", path, err)
			w.markFileFailed(path, err)
			return
		}
		if _, err := w.service.IngestNoteSource(ctx, source); err != nil {
			log.Printf("codeanchor watcher ingest note %s: %v", path, err)
			w.markFileFailed(path, err)
			return
		}
		w.clearFileFailed(path)
		w.markRecentlyIndexed(path)
		w.trackPath(path)
		w.scheduleRecompute()
		return
	}
	if lang, ok := w.langByExt[ext]; ok {
		content, err := os.ReadFile(path)
		if err != nil {
			log.Printf("codeanchor watcher read code %s: %v", path, err)
			w.markFileFailed(path, err)
			return
		}
		ref, err := w.service.codePathRef(path)
		if err != nil {
			log.Printf("codeanchor watcher resolve code %s: %v", path, err)
			w.markFileFailed(path, err)
			return
		}
		if err := w.service.IndexCodeFileRef(ctx, lang, ref, content); err != nil {
			log.Printf("codeanchor watcher index code %s: %v", path, err)
			w.markFileFailed(path, err)
			return
		}
		w.clearFileFailed(path)
		w.markRecentlyIndexed(path)
		w.trackPath(path)
		w.scheduleRecompute()
		return
	}
}

func (w *Watcher) noteSource(path string, content []byte) (NoteSource, error) {
	if w.noteSourceFactory == nil {
		return nil, fmt.Errorf("note source factory is required")
	}
	ref, err := w.service.notePathRef(path)
	if err != nil {
		return nil, err
	}
	mtime := int64(0)
	if info, err := os.Stat(path); err == nil && !info.ModTime().IsZero() {
		mtime = info.ModTime().Unix()
	}
	return w.noteSourceFactory(ref.Rel.String(), content, mtime), nil
}

func (w *Watcher) handleRemove(ctx context.Context, path string) {
	w.clearFileFailed(path)
	w.clearFailedPrefix(path)
	if w.isExcludedPath(path) {
		return
	}
	if isModuleMetadataPath(path) {
		w.service.InvalidateModuleMetadata(path)
		return
	}
	ext := strings.ToLower(filepath.Ext(path))

	// Handle note deletion
	if _, ok := w.noteExts[ext]; ok {
		if err := w.service.DeleteNote(ctx, path); err != nil {
			log.Printf("codeanchor watcher delete note %s (best-effort, remains tracked): %v", path, err)
			return
		}
		w.untrackPath(path)
		w.scheduleRecompute()
		return
	}

	// Handle code file deletion
	if _, ok := w.langByExt[ext]; ok {
		if err := w.service.DeleteFile(ctx, path); err != nil {
			log.Printf("codeanchor watcher delete code %s (best-effort, remains tracked): %v", path, err)
			return
		}
		w.untrackPath(path)
		w.scheduleRecompute()
	}
}

func isModuleMetadataPath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".json")
}

func (w *Watcher) shouldSkipDir(path, name string) bool {
	if isHidden(path) || strings.HasPrefix(name, ".") {
		return true
	}
	_, blocked := w.excludeDirs[name]
	return blocked
}

func (w *Watcher) shouldSkipDirEntry(path string, d os.DirEntry) bool {
	if d == nil || !d.IsDir() {
		return false
	}
	if w.shouldSkipDir(path, d.Name()) {
		return true
	}
	return w.isExcludedPath(path)
}

func (w *Watcher) isExcludedPath(path string) bool {
	clean := filepath.Clean(path)

	w.ignoreMu.RLock()
	matcher := w.ignoreMatcher
	w.ignoreMu.RUnlock()
	if matcher != nil && w.service != nil && w.service.vault.Root() != "" {
		if rel, err := w.service.vault.RelStrict(clean); err == nil && rel.String() != "" {
			if matcher.IsIgnored(rel.String(), false) {
				return true
			}
		}
	}

	parts := strings.Split(clean, string(os.PathSeparator))
	for _, part := range parts {
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, ".") {
			return true
		}
		if _, ok := w.excludeDirs[part]; ok {
			return true
		}
	}

	if len(w.excludeGlobs) > 0 {
		abs := filepath.ToSlash(clean)
		absNoLead := strings.TrimPrefix(abs, "/")
		roots := append(append([]string{}, w.noteRoots...), w.codeRoots...)
		for _, pat := range w.excludeGlobs {
			pat = strings.TrimSpace(pat)
			if pat == "" {
				continue
			}
			if ok, _ := doublestar.Match(pat, absNoLead); ok {
				return true
			}
			for _, root := range roots {
				rootPaths, err := paths.NewVaultPaths(root)
				if err != nil {
					continue
				}
				rel, err := rootPaths.RelStrict(clean)
				if err != nil || rel.String() == "" {
					continue
				}
				if ok, _ := doublestar.Match(pat, filepath.ToSlash(rel.String())); ok {
					return true
				}
			}
		}
	}
	return false
}

func isHidden(path string) bool {
	base := filepath.Base(path)
	return strings.HasPrefix(base, ".")
}

// WaitForDrain blocks until the pending queue drains; if the queue already
// drained before this is called, it returns immediately.
func (w *Watcher) WaitForDrain() {
	w.drainMu.Lock()
	ch := w.drainCh
	w.drainMu.Unlock()
	if ch == nil {
		return
	}
	<-ch
}

func (w *Watcher) markPending() {
	w.drainMu.Lock()
	defer w.drainMu.Unlock()
	if w.drainCh == nil {
		w.drainCh = make(chan struct{})
	}
	w.pending = true
}

func (w *Watcher) markDrained() {
	w.drainMu.Lock()
	defer w.drainMu.Unlock()
	if w.pending && w.drainCh != nil {
		// Channel may already be closed before a waiter arrives; WaitForDrain will return immediately in that case.
		close(w.drainCh)
		w.drainCh = nil
	}
	w.pending = false
}

func (w *Watcher) trackPath(path string) {
	w.trackedMu.Lock()
	// Use case-insensitive key on Windows/macOS
	w.trackedPaths[paths.CaseKey(path)] = struct{}{}
	w.trackedMu.Unlock()
}

func (w *Watcher) untrackPath(path string) {
	w.trackedMu.Lock()
	// Use case-insensitive key on Windows/macOS
	delete(w.trackedPaths, paths.CaseKey(path))
	w.trackedMu.Unlock()
}

func (w *Watcher) snapshotTracked() map[string]struct{} {
	w.trackedMu.Lock()
	defer w.trackedMu.Unlock()
	cp := make(map[string]struct{}, len(w.trackedPaths))
	for p := range w.trackedPaths {
		cp[p] = struct{}{}
	}
	return cp
}

func (w *Watcher) replaceTracked(paths map[string]struct{}) {
	w.trackedMu.Lock()
	w.trackedPaths = paths
	w.trackedMu.Unlock()
}

func (w *Watcher) scheduleRecompute() {
	delay := w.recomputeDelay
	if delay == 0 {
		delay = 200 * time.Millisecond
	}
	w.recomputeMu.Lock()
	if w.recomputeTimer != nil {
		if !w.recomputeTimer.Stop() {
			select {
			case <-w.recomputeTimer.C:
			default:
			}
		}
	}
	w.recomputeTimer = time.AfterFunc(delay, func() {
		ctx := w.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		rcCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := w.service.RecomputeAnchorScopes(rcCtx); err != nil {
			log.Printf("codeanchor watcher recompute: %v", err)
		}
	})
	w.recomputeMu.Unlock()
}

const recentlyIndexedTTL = 500 * time.Millisecond

// markRecentlyIndexed records that a path was just indexed, for deduplication.
func (w *Watcher) markRecentlyIndexed(path string) {
	w.recentlyIndexedMu.Lock()
	w.recentlyIndexed[path] = time.Now()
	w.recentlyIndexedMu.Unlock()
}

// wasRecentlyIndexed returns true if the path was indexed within the TTL.
func (w *Watcher) wasRecentlyIndexed(path string) bool {
	w.recentlyIndexedMu.Lock()
	defer w.recentlyIndexedMu.Unlock()
	if t, ok := w.recentlyIndexed[path]; ok {
		if time.Since(t) < recentlyIndexedTTL {
			return true
		}
		// Expired - clean up
		delete(w.recentlyIndexed, path)
	}
	return false
}

// cleanupRecentlyIndexed removes expired entries from the recently indexed map.
func (w *Watcher) cleanupRecentlyIndexed() {
	w.recentlyIndexedMu.Lock()
	defer w.recentlyIndexedMu.Unlock()
	now := time.Now()
	for path, t := range w.recentlyIndexed {
		if now.Sub(t) >= recentlyIndexedTTL {
			delete(w.recentlyIndexed, path)
		}
	}
}

const (
	maxFailedRetries = 5
	failedRetryBase  = 5 * time.Second
	failedFilesLimit = 1000
)

// markFileFailed records that a file failed to index.
func (w *Watcher) markFileFailed(path string, err error) {
	w.failedFilesMu.Lock()
	defer w.failedFilesMu.Unlock()

	entry, ok := w.failedFiles[path]
	if !ok {
		// Limit the number of tracked failed files
		if len(w.failedFiles) >= failedFilesLimit {
			return
		}
		entry = &failedFileEntry{}
		w.failedFiles[path] = entry
	}
	entry.lastAttempt = time.Now()
	entry.attempts++
	entry.lastError = err.Error()
}

// clearFileFailed removes a file from the failed set (e.g., after successful index).
func (w *Watcher) clearFileFailed(path string) {
	w.failedFilesMu.Lock()
	delete(w.failedFiles, path)
	w.failedFilesMu.Unlock()
}

func (w *Watcher) clearFailedPrefix(path string) {
	w.failedFilesMu.Lock()
	defer w.failedFilesMu.Unlock()
	if len(w.failedFiles) == 0 {
		return
	}
	clean := filepath.Clean(path)
	prefix := clean + string(os.PathSeparator)
	for failedPath := range w.failedFiles {
		if strings.HasPrefix(failedPath, prefix) {
			delete(w.failedFiles, failedPath)
		}
	}
}

// shouldRetryFile returns true if the file should be retried based on backoff.
func (w *Watcher) shouldRetryFile(path string) bool {
	w.failedFilesMu.Lock()
	defer w.failedFilesMu.Unlock()

	entry, ok := w.failedFiles[path]
	if !ok {
		return true // Not in failed set, allow processing
	}
	if entry.attempts >= maxFailedRetries {
		return false // Exceeded max retries
	}
	// Exponential backoff: 5s, 10s, 20s, 40s, 80s
	attempt := entry.attempts
	if attempt < 1 {
		attempt = 1
	}
	backoff := failedRetryBase * time.Duration(1<<(attempt-1))
	return time.Since(entry.lastAttempt) >= backoff
}

// FailedFiles returns a snapshot of currently failed files for diagnostics.
func (w *Watcher) FailedFiles() map[string]string {
	w.failedFilesMu.Lock()
	defer w.failedFilesMu.Unlock()

	result := make(map[string]string, len(w.failedFiles))
	for path, entry := range w.failedFiles {
		result[path] = entry.lastError
	}
	return result
}
