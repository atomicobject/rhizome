package cache

// Docs:
// - [Cache (Hub)](docs/hubs/Cache (Hub).md)
// - [Vault cache service (Service)](docs/reference/analysis/Vault cache service (Service).md)
// - [Dirty tracking + Refresh semantics](docs/reference/analysis/Dirty tracking + Refresh semantics.md)
// - [Watcher design + degraded mode](docs/reference/analysis/Watcher design + degraded mode.md)
// - [Ignore + exclude rules](docs/reference/guides/Ignore + exclude rules.md)
// - [Code reference scanning in cache](docs/reference/analysis/Code reference scanning in cache.md)

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/bmatcuk/doublestar/v4"
)

// ═══════════════════════════════════════════════════════════════════════════════
// TYPES AND DATA STRUCTURES
// ═══════════════════════════════════════════════════════════════════════════════
//
// This section defines the core data structures for the cache service:
//   - Service: the main cache coordinator
//   - Entry: cached metadata for a single markdown file
//   - DirtyKind: why a path needs revalidation
//   - WatchHub subscriber: wiring that maps watch events into dirty markers

// Service maintains an in-memory cache of vault files, metadata, and tags.
//
// Architecture overview:
//
//	┌─────────────────────────────────────────────────────────────────────┐
//	│                           Service                                   │
//	│  ┌──────────────┐  ┌──────────────┐                                │
//	│  │  fileIndex   │  │   tagIndex   │                                │
//	│  │ path→Entry   │  │  tag→paths   │                                │
//	│  └──────────────┘  └──────────────┘                                │
//	│         ▲                                                          │
//	│         │ refreshPath                                               │
//	│         │                                                          │
//	│  ┌──────┴──────────────────────────────────┐                       │
//	│  │                Refresh()                 │                       │
//	│  │  Consumes dirty markers, revalidates     │                       │
//	│  │  stale data                               │                       │
//	│  └──────────────────────▲───────────────────┘                       │
//	│                         │                                           │
//	│         ┌───────────────┴───────────────┐                           │
//	│         │         dirty map             │                           │
//	│         │   path → DirtyKind            │                           │
//	│         └───────────────▲───────────────┘                           │
//	│                         │ markDirty                                 │
//	│                         │                                           │
//	│  ┌──────────────────────┴──────────────────────┐                    │
//	│  │         WatchHub subscribers (external)      │                    │
//	│  │     Translate watch events → dirty markers   │                    │
//	│  └──────────────────────────────────────────────┘                    │
//	└─────────────────────────────────────────────────────────────────────┘
//
// Operational story (read before editing):
//  1. EnsureReady() performs a one-time crawl to populate indices. It is
//     concurrency-safe and uses a simple spin gate.
//  2. Callers (or WatchHub) mark dirty/stale paths for incremental refresh.
//  3. Refresh() is the front door callers hit before reading; it revalidates
//     stale caches and applies dirty markers by re-reading or deleting paths.
//
// The intent is to keep the dataflow legible rather than aggressively abstracted:
// callers see the crawl and refresh step as distinct phases.
type Service struct {
	vaultPath string
	vault     paths.VaultPaths

	// lifetime bounds background recrawls; Close cancels and drains them.
	lifetime  context.Context
	cancel    context.CancelFunc
	recrawlWG sync.WaitGroup

	// Mutex guards all mutable state below. Use RLock for reads, Lock for writes.
	mu         sync.RWMutex
	ready      bool                           // true after initial crawl completes
	crawling   bool                           // guards against concurrent initial crawls
	crawlCh    chan struct{}                  // closed when current crawl finishes
	crawlErr   error                          // last crawl error (if any)
	stale      bool                           // set when consistency is uncertain; the next Refresh starts a recrawl
	recrawling bool                           // a stale-triggered background recrawl is in flight
	resynced   bool                           // a recrawl completed since the last Refresh reported it
	fileIndex  map[string]*Entry              // path → cached entry
	tagIndex   map[string]map[string]struct{} // tag → set of paths
	dirty      map[string]DirtyKind           // paths needing revalidation

	// Unified ignore matcher: defaults + .gitignore + .rhizome/ignore + user excludes
	ignoreMatcher     *ignore.Matcher // computed from the current SelectionPolicy
	hardIgnoreMatcher *ignore.Matcher // excludes ordinary note-selection config

	// Lifecycle management
	version uint64 // monotonic counter, bumped on changes

	// Selection is the provider-authorized boundary for this Markdown cache.
	selection        SelectionPolicy
	selectionVersion uint64
	noteRuntime      *noteformat.Runtime

	// Code reference scanning (optional, for codebases with docs)
	codeRefConfig *coderefs.Config        // nil if disabled
	codeRefIndex  *coderefs.Index         // index of code -> note refs
	notePathCache *obsidian.NotePathCache // for resolving wikilinks in code; rebuilt on note changes
	codeFileMeta  map[string]fileMeta     // code file metadata for change detection

	metrics serviceMetrics
}

type fileMeta struct {
	ModTime time.Time
	Size    int64
}

// Entry represents cached metadata for a single selected note projection.
// All derived fields come from the configured note-format runtime, or from the
// isolated Markdown compatibility adapter when no runtime is configured.
type Entry struct {
	Path        string                 // relative path within vault (normalized)
	ModTime     time.Time              // last modification time from filesystem
	Size        int64                  // file size in bytes
	Tags        []string               // normalized tags (lowercase, no # prefix)
	Frontmatter map[string]interface{} // parsed YAML frontmatter
	InlineProps map[string][]string    // Dataview-style inline properties
	Content     string                 // full file content
	ContentTime time.Time              // derived content timestamp (frontmatter/filename/heading)
}

// DirtyKind captures why a path was marked dirty. The kind affects how
// Refresh() processes the path (e.g., remove vs re-read).
type DirtyKind string

const (
	DirtyUnknown   DirtyKind = "unknown"
	DirtyCreated   DirtyKind = "created"   // new file appeared
	DirtyModified  DirtyKind = "modified"  // existing file changed
	DirtyRemoved   DirtyKind = "removed"   // file was deleted
	DirtyRenamed   DirtyKind = "renamed"   // file was renamed (old path)
	DirtyRecreated DirtyKind = "recreated" // rapid delete+create sequence
)

// FileDiscoveryFunc is a function that returns a list of relative file paths to cache.
// This allows the cache to support different file discovery strategies (e.g., glob-based).
type FileDiscoveryFunc func() ([]string, error)

// Options controls cache behavior.
type Options struct {
	DiscoverFiles FileDiscoveryFunc   // optional custom file discovery (for glob-based vaults)
	AdmitNote     NoteAdmissionFunc   // optional provider-authorized note admission predicate
	UserExcludes  []string            // user-specified exclude patterns (will be merged with .obsidianignore)
	CodeRefConfig *coderefs.Config    // optional code reference scanning config (nil = disabled)
	NoteRuntime   *noteformat.Runtime // optional provider projection runtime
}

// CodeRefProvider exposes code reference data from the cache.
type CodeRefProvider interface {
	CodeRefsByNote() map[string][]coderefs.CodeRef
	CodeRefsByFile() map[string][]coderefs.CodeRef
}

// ═══════════════════════════════════════════════════════════════════════════════
// CONSTRUCTOR AND LIFECYCLE
// ═══════════════════════════════════════════════════════════════════════════════
//
// These functions manage the Service's lifecycle: creation, initialization, and
// cleanup. The service starts "cold" and lazily initializes on first use.

// NewService constructs a cache service for a vault. The service is not yet
// ready; call EnsureReady() to trigger the initial crawl.
func NewService(vaultPath string, opts Options) (*Service, error) {
	if vaultPath == "" {
		return nil, errors.New("vaultPath is required")
	}

	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil {
		return nil, err
	}
	if vaultPaths.Root() != "" {
		vaultPath = vaultPaths.Root()
	}

	// Warn if code ref patterns attempt to escape vault root
	if opts.CodeRefConfig != nil && opts.CodeRefConfig.Enabled {
		for _, p := range append(opts.CodeRefConfig.Includes, opts.CodeRefConfig.Excludes...) {
			if strings.Contains(p, "..") {
				log.Printf("cache: code ref pattern %q contains '..' and may escape the vault root; this is not supported", p)
			}
		}
	}

	lifetime, cancel := context.WithCancel(context.Background())
	svc := &Service{
		lifetime:  lifetime,
		cancel:    cancel,
		vaultPath: vaultPath,
		vault:     vaultPaths,
		fileIndex: make(map[string]*Entry),
		tagIndex:  make(map[string]map[string]struct{}),
		dirty:     make(map[string]DirtyKind),
		selection: normalizeSelectionPolicy(SelectionPolicy{
			DiscoverFiles: opts.DiscoverFiles,
			Admit:         opts.AdmitNote,
			UserExcludes:  opts.UserExcludes,
		}),
		noteRuntime: opts.NoteRuntime,
	}

	// Initialize code ref scanning if enabled
	if opts.CodeRefConfig != nil && opts.CodeRefConfig.Enabled {
		svc.codeRefConfig = opts.CodeRefConfig
		svc.codeRefIndex = coderefs.NewIndex()
		svc.codeFileMeta = make(map[string]fileMeta)
	}

	return svc, nil
}

func (s *Service) relPath(absPath string) (string, error) {
	if strings.TrimSpace(absPath) == "" {
		return "", nil
	}
	if s.vault.Root() == "" {
		rel, err := paths.CleanRelPath(absPath)
		if err != nil {
			return "", err
		}
		return rel.String(), nil
	}
	// Cache state is keyed by strict vault-relative paths. Dropping escaped paths
	// here is safer than allowing a watcher or custom discovery function to add
	// absolute/out-of-root entries to fileIndex.
	rel, err := s.vault.RelStrict(absPath)
	if err != nil {
		return "", err
	}
	relStr := rel.String()
	if relStr == "" {
		relStr = "."
	}
	return relStr, nil
}

func (s *Service) absPath(relPath string) string {
	// Convert at the I/O edge only. The cache itself stores relative keys so
	// downstream graph/backlink/code-ref caches can compare them directly.
	return paths.AbsFromInputWithVaultPaths(s.vault, s.vaultPath, relPath).String()
}

// Close cancels and waits for owned background recrawls. Safe to call multiple times.
// Callers remain responsible for draining synchronous EnsureReady and Refresh calls.
func (s *Service) Close() error {
	// Serialize cancellation with recrawl admission before waiting.
	s.mu.Lock()
	s.cancel()
	s.mu.Unlock()
	s.recrawlWG.Wait()
	return nil
}

// MarkDirty records a vault-relative path as dirty (for external hint sources).
// Callers should follow with Refresh() to apply changes.
func (s *Service) MarkDirty(relPath string, kind DirtyKind) {
	abs := s.absPath(relPath)
	if abs == "" {
		return
	}
	s.markDirty(abs, kind)
}

// MarkStale makes the next Refresh() start a background recrawl of the
// entire cache (single-flight; a MarkStale during a recrawl queues another).
func (s *Service) MarkStale() {
	s.markStale()
}

// EnsureReady performs the initial crawl (once).
// It is safe to call concurrently; only one goroutine will perform the initial crawl.
// Subsequent calls delegate to Refresh().
func (s *Service) EnsureReady(ctx context.Context) error {
	for {
		s.mu.Lock()
		if s.ready {
			s.mu.Unlock()
			return s.Refresh(ctx)
		}

		// Concurrency gate: if another goroutine is already performing the
		// first crawl, wait for that specific crawl to finish, then re-read
		// state. Only the first crawl gates here; stale-triggered recrawls keep
		// ready=true and reconcile the live index in place.
		if s.crawling {
			ch := s.crawlCh
			s.mu.Unlock()
			if ch != nil {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-ch:
				}
			}
			s.mu.RLock()
			ready := s.ready
			crawling := s.crawling
			crawlErr := s.crawlErr
			s.mu.RUnlock()
			if crawlErr != nil {
				return crawlErr
			}
			if ready {
				return s.Refresh(ctx)
			}
			if crawling {
				continue
			}
			return errors.New("cache: crawl finished without readiness")
		}
		s.crawling = true
		s.crawlErr = nil
		s.crawlCh = make(chan struct{})
		s.mu.Unlock()
		break
	}

	if err := s.initialCrawl(ctx); err != nil {
		s.mu.Lock()
		s.crawling = false
		s.crawlErr = err
		if s.crawlCh != nil {
			close(s.crawlCh)
			s.crawlCh = nil
		}
		s.mu.Unlock()
		return err
	}
	s.mu.Lock()
	s.crawling = false
	if s.crawlCh != nil {
		close(s.crawlCh)
		s.crawlCh = nil
	}
	s.mu.Unlock()
	return s.Refresh(ctx)
}

// ═══════════════════════════════════════════════════════════════════════════════
// PUBLIC API (READING)
// ═══════════════════════════════════════════════════════════════════════════════
//
// These methods provide read access to cached data. Most do NOT call Refresh()
// automatically; callers should call Refresh() first to ensure freshness.
// EntriesSnapshot is the exception—it refreshes before returning.

// Paths returns all cached paths.
// Note: Callers should call Refresh() first to ensure freshness.
func (s *Service) Paths() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	paths := make([]string, 0, len(s.fileIndex))
	for p := range s.fileIndex {
		paths = append(paths, p)
	}
	return paths
}

// DirtyPaths returns a snapshot of currently dirty relative paths.
// Callers should typically invoke Refresh() afterward to apply changes.
func (s *Service) DirtyPaths() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	paths := make([]string, 0, len(s.dirty))
	for p := range s.dirty {
		paths = append(paths, p)
	}
	return paths
}

// DirtySnapshot returns a copy of the current dirty map (relative paths → kind).
// Callers should typically invoke Refresh() afterward to apply changes.
func (s *Service) DirtySnapshot() map[string]DirtyKind {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copyMap := make(map[string]DirtyKind, len(s.dirty))
	for k, v := range s.dirty {
		copyMap[k] = v
	}
	return copyMap
}

// Version returns a monotonic counter that increments whenever the cache
// processes dirty changes or is resynced. Callers can use it to invalidate
// derived caches (e.g., backlinks, graph analysis).
func (s *Service) Version() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.version
}

// EntriesSnapshot returns a deep copy of all cached entries after ensuring freshness.
// This is the safest way to get a consistent view of the entire cache.
func (s *Service) EntriesSnapshot(ctx context.Context) ([]Entry, error) {
	if err := s.Refresh(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries := make([]Entry, 0, len(s.fileIndex))
	for _, e := range s.fileIndex {
		entries = append(entries, e.clone())
	}
	return entries, nil
}

// Entry returns a copy of the cached entry for the given path.
// Note: Callers should call Refresh() first to ensure freshness.
func (s *Service) Entry(path string) (Entry, bool) {
	// Entry receives an authored cache identity. It must not infer a Markdown
	// suffix for selected provider paths.
	norm := paths.NormalizeNotePath(path).String()

	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.fileIndex[norm]
	if !ok {
		return Entry{}, false
	}
	// Return a deep copy to prevent callers from mutating the cache.
	return entry.clone(), true
}

// clone returns a deep copy of the Entry, protecting internal slices and maps.
func (e *Entry) clone() Entry {
	out := Entry{
		Path:        e.Path,
		ModTime:     e.ModTime,
		Size:        e.Size,
		Content:     e.Content,
		ContentTime: e.ContentTime,
	}
	if len(e.Tags) > 0 {
		out.Tags = make([]string, len(e.Tags))
		copy(out.Tags, e.Tags)
	}
	if len(e.Frontmatter) > 0 {
		out.Frontmatter = make(map[string]interface{}, len(e.Frontmatter))
		for k, v := range e.Frontmatter {
			out.Frontmatter[k] = v // Note: nested structures still share references
		}
	}
	if len(e.InlineProps) > 0 {
		out.InlineProps = make(map[string][]string, len(e.InlineProps))
		for k, v := range e.InlineProps {
			cp := make([]string, len(v))
			copy(cp, v)
			out.InlineProps[k] = cp
		}
	}
	return out
}

// ═══════════════════════════════════════════════════════════════════════════════
// REFRESH AND RECONCILIATION
// ═══════════════════════════════════════════════════════════════════════════════
//
// Refresh is the "checkpoint" that reconciles in-memory state with the filesystem.
// It consumes dirty markers accumulated by watch events and applies them. If the
// stale flag is set, it starts a background recrawl and keeps serving the
// current index.

// Refresh reconciles in-memory state with the filesystem.
//   - If this is the first call, it delegates to EnsureReady().
//   - If stale, it starts one background recrawl and returns immediately.
//   - Otherwise it consumes dirty markers emitted by watchers.
func (s *Service) Refresh(ctx context.Context) error {
	_, err := s.RefreshWithResult(ctx)
	return err
}

// RefreshAndDrainDirty refreshes the cache and returns the set of dirty paths
// that were consumed in this pass. The boolean indicates whether a full resync
// completed since the previous pass (e.g., ignore rule change), allowing
// callers to react appropriately.
func (s *Service) RefreshAndDrainDirty(ctx context.Context) (map[string]DirtyKind, bool, error) {
	result, err := s.RefreshWithResult(ctx)
	return result.Changed, result.Resynced, err
}

// refresh performs the core refresh logic. A failed batch is requeued so an
// interrupted refresh never drops watcher input before the next retry.
func (s *Service) refresh(ctx context.Context) (result RefreshResult, err error) {
	s.mu.Lock()
	if !s.ready {
		s.mu.Unlock()
		// EnsureReady performs the initial crawl and its nested refresh. Surface
		// that completed crawl as a resync to this caller; otherwise a cold live
		// watcher sees an empty result and exits without ownership discovery.
		err := s.EnsureReady(ctx)
		return RefreshResult{Resynced: err == nil}, err
	}

	// Snapshot mutable state so we can release the lock while touching disk.
	// A stale cache starts one background recrawl (single-flight); this caller
	// keeps serving the current index and handles its dirty batch as usual.
	// Resynced reports the completion of an earlier recrawl, not its start, so
	// watchers force a full reconcile only once the index actually caught up.
	startRecrawl := s.stale && !s.recrawling && s.lifetime.Err() == nil
	if startRecrawl {
		s.stale = false
		s.recrawling = true
		s.recrawlWG.Add(1)
	}
	result.Resynced = s.resynced
	s.resynced = false
	dirty := s.dirty
	s.dirty = make(map[string]DirtyKind)
	s.mu.Unlock()
	result.Drained = cloneDirty(dirty)

	s.metrics.lastDirtyBatch.Store(int64(len(dirty)))

	if startRecrawl {
		go s.recrawl()
	}

	if len(dirty) == 0 {
		return result, nil
	}

	changed := false
	changedPaths := make(map[string]DirtyKind)
	paths := make([]string, 0, len(dirty))
	for path := range dirty {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	// Process each dirty marker according to its kind. Disk I/O happens outside
	// the service mutex; failed paths are re-marked dirty so transient races with
	// editors don't permanently drop freshness work.
	for _, path := range paths {
		kind := dirty[path]
		select {
		case <-ctx.Done():
			s.requeueDirty(dirty)
			return s.completeRefresh(ctx, result, changed, changedPaths), ctx.Err()
		default:
		}

		internalFreshness := isInternalFreshnessPath(path)
		switch kind {
		case DirtyRemoved, DirtyRenamed:
			// Remove the entry (and children if it's a directory).
			if s.removeTree(path) {
				changed = true
				changedPaths[path] = kind
			}
			if s.isCodeRefCandidate(path) {
				if s.removeCodeFile(path) {
					changed = true
					changedPaths[path] = kind
				}
			}
			if kind == DirtyRenamed {
				// Rescan the parent directory to pick up the new name.
				if abs := s.absPath(path); abs != "" {
					parent := filepath.Dir(abs)
					if childChanges, err := s.rescanDir(parent, true); err == nil {
						for child := range childChanges {
							changed = true
							changedPaths[child] = DirtyModified
						}
					} else {
						s.markDirty(parent, DirtyModified)
					}
				}
			}

		case DirtyRecreated:
			// A rapid delete+create sequence. Remove old data first.
			if s.removeTree(path) {
				changed = true
				changedPaths[path] = kind
			}
			if s.isCodeRefCandidate(path) {
				if s.removeCodeFile(path) {
					changed = true
					changedPaths[path] = kind
				}
			}
			fallthrough

		default:
			// Created, Modified, or Recreated: refresh from disk.
			absPath := s.absPath(path)
			if absPath == "" {
				continue
			}
			info, err := os.Stat(absPath)
			if err != nil {
				// If a path marked as modified no longer exists, treat it like
				// a removal so directory children are purged from cache state.
				if os.IsNotExist(err) {
					if s.removeTree(path) {
						changed = true
						changedPaths[path] = DirtyRemoved
					}
					if s.isCodeRefCandidate(path) {
						if s.removeCodeFile(path) {
							changed = true
							changedPaths[path] = DirtyRemoved
						}
					}
					if internalFreshness {
						changed = true
						changedPaths[path] = DirtyRemoved
					}
				} else {
					s.markDirty(absPath, DirtyModified)
				}
				continue
			}
			if info.IsDir() {
				// It's a directory—scan it for files.
				if childChanges, err := s.rescanDir(absPath, true); err != nil {
					s.markDirty(absPath, DirtyModified)
				} else {
					for child := range childChanges {
						changed = true
						changedPaths[child] = DirtyModified
					}
				}
			} else {
				// The admission policy controls projection eligibility. Rejected
				// paths remain visible through result.Drained for other consumers.
				if s.admitsNotePath(path) {
					if didChange, err := s.refreshPath(path, false); err != nil {
						s.markDirty(absPath, DirtyModified)
					} else if didChange {
						changed = true
						changedPaths[path] = kind
					}
				} else if s.isCodeRefCandidate(path) {
					// Code file: refresh its refs
					s.mu.RLock()
					noteCache := s.notePathCache
					s.mu.RUnlock()
					if noteCache != nil {
						if s.refreshCodeRef(path, noteCache, false) {
							changed = true
							changedPaths[path] = kind
						}
					}
				}
			}
		}
		if internalFreshness {
			changed = true
			changedPaths[path] = kind
		}
	}

	return s.completeRefresh(ctx, result, changed, changedPaths), nil
}

func (s *Service) completeRefresh(ctx context.Context, result RefreshResult, changed bool, changedPaths map[string]DirtyKind) RefreshResult {
	if changed && s.codeRefConfig != nil && s.codeRefConfig.Enabled {
		// A partial batch can have already changed note state when its caller
		// cancels. Keep dependent code references aligned before reporting it.
		s.updateCodeRefsIncremental(context.WithoutCancel(ctx), changedPaths)
	}
	if changed {
		s.bumpVersion()
	}
	if len(changedPaths) > 0 {
		result.Changed = changedPaths
	}
	return result
}

// recrawl runs one stale-triggered crawl against the live index. A failure
// re-arms stale so the next Refresh starts another; success is reported to the
// next Refresh through RefreshResult.Resynced. MarkStale during a recrawl also
// re-arms stale, so a follow-up crawl covers whatever this one may have missed.
func (s *Service) recrawl() {
	defer s.recrawlWG.Done()
	start := time.Now()
	err := s.initialCrawl(s.lifetime)
	s.metrics.resyncs.Add(1)
	s.metrics.lastResyncNanos.Store(time.Since(start).Nanoseconds())
	s.mu.Lock()
	s.recrawling = false
	if err != nil {
		s.stale = true
		s.version++ // a partial crawl may already have changed entries
	} else {
		s.resynced = true
	}
	s.mu.Unlock()
}

func isInternalFreshnessPath(rel string) bool {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	switch {
	case rel == ".rhizome/config.yml":
		return true
	case rel == ".rhizome/ignore", rel == ".obsidianignore":
		return true
	case strings.HasPrefix(rel, ".rhizome/ontology/") && filepath.Ext(rel) == ".graphql":
		return true
	case strings.HasPrefix(rel, ".rhizome/query-recipes/"):
		ext := filepath.Ext(rel)
		return ext == ".yaml" || ext == ".yml"
	default:
		return false
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// CRAWLING AND SCANNING
// ═══════════════════════════════════════════════════════════════════════════════
//
// These functions walk the filesystem and populate the cache. initialCrawl runs
// at startup and again on every stale-triggered resync; rescanDir handles
// incremental updates for new directories.

// initialCrawl walks the vault and reconciles the cache with it. It runs in
// four phases:
//  1. Walk the tree and collect file paths.
//  2. Drop entries the walk did not rediscover (deleted, newly ignored, or
//     rejected by a replaced selection policy).
//  3. Read each file and extract metadata.
//  4. Rebuild the note path cache and rescan code references.
//
// Entries are upserted in place, so readers keep serving the last-good index
// while a resync runs; only the very first crawl gates readers (EnsureReady).
// Every file is re-read (the cache is stale precisely because an event may
// have been missed), and a concurrent dirty refresh that committed a newer
// entry wins over the crawl's read (see refreshPath).
// If a custom DiscoverFiles function is set (for glob-based vaults), it uses
// that instead of walking the entire directory tree.
func (s *Service) initialCrawl(ctx context.Context) error {
	indexingperf.AddCount(ctx, indexingperf.AgentStartOpNotePasses, 1)
	indexingperf.AddCount(ctx, indexingperf.AgentStartOpRepoWalks, 1)
	s.loadIgnorePatterns()

	s.mu.RLock()
	before := make([]string, 0, len(s.fileIndex))
	for rel := range s.fileIndex {
		before = append(before, rel)
	}
	s.mu.RUnlock()

	var discoveredPaths []string

	if discover := s.discoveryFunc(); discover != nil {
		// Use custom discovery (glob-based vaults)
		discovered, err := discover()
		if err != nil {
			return fmt.Errorf("discover files: %w", err)
		}
		discoveredPaths = discovered

	} else {
		// Classic directory walk. Use IsIgnoredShallow because WalkDir prunes
		// ignored directories as it goes; checking ancestors every file would add
		// avoidable matcher work on large vaults.
		err := filepath.WalkDir(s.vaultPath, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			rel, err := s.relPath(path)
			if err != nil {
				return err
			}
			if rel == "" {
				rel = "."
			}

			s.mu.RLock()
			matcher := s.ignoreMatcher
			hardMatcher := s.hardIgnoreMatcher
			s.mu.RUnlock()

			if d.IsDir() {
				// Skip hidden directories
				if strings.HasPrefix(d.Name(), ".") && path != s.vaultPath {
					return filepath.SkipDir
				}
				// Skip excluded directories
				if rel != "." && matcher != nil && matcher.IsIgnoredShallow(rel, true) && hardMatcher != nil && hardMatcher.IsIgnoredShallow(rel, true) {
					return filepath.SkipDir
				}
				return nil
			}

			// Skip hidden files and files the current projection policy rejects.
			if strings.HasPrefix(d.Name(), ".") || !s.admitsNotePath(rel) {
				return nil
			}

			// Skip excluded files
			visibilityMatcher := matcher
			if ignore.IsSystemContextPath(rel) {
				if ignore.IsDefaultInfrastructurePath(rel) {
					return nil
				}
				visibilityMatcher = hardMatcher
			}
			if visibilityMatcher != nil && visibilityMatcher.IsIgnoredShallow(rel, false) {
				return nil
			}
			noteRel, err := paths.CleanNotePath(rel)
			if err != nil {
				return nil
			}
			discoveredPaths = append(discoveredPaths, noteRel.String())
			return nil
		})
		if err != nil {
			return err
		}
	}

	// Phase 2: Discovery is authoritative. Evict entries it no longer returns
	// (deleted, newly ignored, or rejected by a replaced selection policy)
	// before reading anything, so a canceled crawl still reports them gone.
	// ponytail: an entry deleted and recreated while discovery ran is evicted
	// until its next watcher event; reads fall back to disk meanwhile.
	discovered := make(map[string]struct{}, len(discoveredPaths))
	for _, relPath := range discoveredPaths {
		discovered[relPath] = struct{}{}
	}
	for _, rel := range before {
		if _, ok := discovered[rel]; !ok {
			s.removePath(rel)
		}
	}

	// Phase 3: Read files and build indices. This is intentionally serial today:
	// refreshPath mutates shared indexes. Parallelizing belongs behind a
	// clearer writer boundary.
	for _, relPath := range discoveredPaths {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		absPath := s.absPath(relPath)
		if absPath == "" {
			continue
		}
		refreshed, err := s.refreshPath(absPath, true)
		if err != nil {
			// Best effort: mark dirty for retry rather than failing the whole crawl.
			s.markDirty(absPath, DirtyModified)
		} else if refreshed {
			indexingperf.AddCount(ctx, indexingperf.AgentStartOpNoteReads, 1)
		}
	}

	// Phase 4: Build note path cache and scan code files (if enabled). Code ref
	// resolution depends on the complete note set, so keep it after markdown
	// discovery rather than interleaving it with note reads.
	if s.codeRefConfig != nil && s.codeRefConfig.Enabled {
		s.rebuildNotePathCache()
		if err := s.scanCodeFiles(ctx); err != nil {
			// Non-fatal: log and continue
			log.Printf("cache: code ref scan failed: %v", err)
		}
	}

	s.mu.Lock()
	s.ready = true
	s.version++
	s.mu.Unlock()
	return nil
}

// refreshPath reads a single file from disk and updates the cache.
// It handles both absolute and relative paths. force (crawls) re-reads even
// when the cached size and mtime match, and yields when a concurrent dirty
// refresh replaces or removes the entry while this read is in flight.
func (s *Service) refreshPath(absPath string, force bool) (bool, error) {
	if abs := s.absPath(absPath); abs != "" {
		absPath = abs
	}

	// Normalize the authored path for consistent map keys without inferring an
	// extension. Admission owns format authorization; this cache does not.
	rel, err := s.relPath(absPath)
	if err != nil {
		return false, err
	}
	if rel == "" || rel == "." {
		return false, nil
	}
	notePath, err := paths.CleanNotePath(rel)
	if err != nil {
		return false, nil
	}
	rel = notePath.String()

	// Capture the exact entry observed before touching disk. A forced crawl must
	// only commit if that entry is still current; comparing entry identity also
	// detects equal-mtime replacements and removals by concurrent dirty refreshes.
	s.mu.RLock()
	prev := s.fileIndex[rel]
	s.mu.RUnlock()

	info, err := os.Stat(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s.removePath(rel), nil
		}
		return false, err
	}

	// Skip directories and descriptor-only formats before reading content. A
	// descriptor-only provider, such as HTML today, has no facts the cache can
	// safely expose and must never fall through to a Markdown parser.
	if info.IsDir() {
		return false, nil
	}
	if strings.HasPrefix(info.Name(), ".") || !s.admitsNotePath(rel) {
		return s.removePath(rel), nil
	}
	if !s.canProjectNotePath(notePath) {
		return s.removePath(rel), nil
	}

	// Skip excluded files (unified glob-based check)
	if s.shouldExclude(rel) {
		// Ensure we drop any stale cache entries when a path becomes excluded.
		return s.removePath(rel), nil
	}

	if !force {
		if prev != nil && prev.Size == info.Size() && prev.ModTime.Equal(info.ModTime()) {
			return false, nil
		}
	}

	// Single disk read; reuse content for all extractors.
	content, err := os.ReadFile(absPath)
	if err != nil {
		return false, err
	}

	entry, current, err := s.projectEntry(notePath, content, info.ModTime())
	if err != nil {
		return false, err
	}
	if !current {
		return s.removePath(rel), nil
	}

	// Update indices atomically. Forced crawls reconcile a snapshot, so any
	// concurrent replacement or removal wins over the snapshot read.
	s.mu.Lock()
	if force && s.fileIndex[rel] != prev {
		s.mu.Unlock()
		return false, nil
	}
	s.removeTagsForPathLocked(rel)
	s.fileIndex[rel] = entry
	s.indexTags(rel, entry.Tags)
	s.mu.Unlock()
	return true, nil
}

// rescanDir refreshes all files in a directory. Used after a new directory
// is created or after a rename to pick up the new contents.
func (s *Service) rescanDir(absDir string, recursive bool) (map[string]struct{}, error) {
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil, err
	}
	changedPaths := map[string]struct{}{}

	// Get note cache for code ref scanning (if enabled)
	var noteCache *obsidian.NotePathCache
	if s.codeRefConfig != nil && s.codeRefConfig.Enabled {
		s.mu.RLock()
		noteCache = s.notePathCache
		s.mu.RUnlock()
	}

	for _, entry := range entries {
		absPath := filepath.Join(absDir, entry.Name())
		rel, err := s.relPath(absPath)
		if err != nil {
			continue
		}
		if rel == "" {
			rel = "."
		}

		// Skip hidden entries
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		// Check exclude patterns
		if s.shouldExclude(rel) {
			continue
		}

		if entry.IsDir() {
			if recursive {
				childChanges, _ := s.rescanDir(absPath, true)
				for child := range childChanges {
					changedPaths[child] = struct{}{}
				}
			}
			continue
		}

		// Handle provider-authorized Markdown projection files.
		if s.admitsNotePath(rel) {
			if didChange, err := s.refreshPath(absPath, false); err != nil {
				s.markDirty(absPath, DirtyModified)
			} else if didChange {
				noteRel, err := s.relPath(absPath)
				if err == nil && noteRel != "" && noteRel != "." {
					changedPaths[noteRel] = struct{}{}
				}
			}
			continue
		}

		// Handle code files (if code ref scanning is enabled)
		if noteCache != nil && s.isCodeRefCandidate(rel) {
			relCode := string(paths.NormalizeCode(rel))
			if s.refreshCodeRef(relCode, noteCache, false) {
				changedPaths[relCode] = struct{}{}
			}
		}
	}
	return changedPaths, nil
}

// loadIgnorePatterns computes unified exclude patterns by merging:
// - Default ignore patterns (node_modules, vendor, etc.)
// - .rhizome/ignore from vault root (if present; legacy .obsidianignore supported)
// - User-specified excludes from Options
//
// All patterns are converted to glob format for consistent matching.
// Changes to .rhizome/ignore trigger a resync that re-calls this function.
func (s *Service) loadIgnorePatterns() {
	s.mu.RLock()
	excludes := append([]string(nil), s.selection.UserExcludes...)
	selectionVersion := s.selectionVersion
	s.mu.RUnlock()
	matcher := ignore.LoadUnifiedMatcher(s.vaultPath, excludes)
	hardMatcher := ignore.LoadUnifiedMatcher(s.vaultPath, nil)
	s.mu.Lock()
	if s.selectionVersion == selectionVersion {
		s.ignoreMatcher = matcher
		s.hardIgnoreMatcher = hardMatcher
	}
	s.mu.Unlock()
}

// ═══════════════════════════════════════════════════════════════════════════════
// INDEX MANAGEMENT
// ═══════════════════════════════════════════════════════════════════════════════
//
// These functions maintain the fileIndex and tagIndex. They handle adding,
// removing, and updating entries while keeping indices consistent.

// removePath removes a single file from the cache.
func (s *Service) removePath(rel string) bool {
	rel = paths.NormalizeNotePath(rel).String()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.fileIndex[rel]; !ok {
		return false
	}
	s.removeTagsForPathLocked(rel)
	delete(s.fileIndex, rel)
	return true
}

func (s *Service) removeCodeFile(rel string) bool {
	rel = string(paths.NormalizeCode(rel))

	removed := false
	if s.codeRefIndex != nil {
		if refs := s.codeRefIndex.RefsByFile(rel); len(refs) > 0 {
			removed = true
		}
		s.codeRefIndex.RemoveFile(rel)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.codeFileMeta != nil {
		if _, ok := s.codeFileMeta[rel]; ok {
			removed = true
			delete(s.codeFileMeta, rel)
		}
	}

	return removed
}

// removeTree removes a path and all children (for directory deletions/renames).
func (s *Service) removeTree(rel string) bool {
	rel = string(paths.Normalize(rel))
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := false
	for path := range s.fileIndex {
		if path == rel || strings.HasPrefix(path, rel+"/") {
			removed = true
			s.removeTagsForPathLocked(path)
			delete(s.fileIndex, path)
		}
	}
	return removed
}

// removeTagsForPathLocked cleans up the tag index when removing a file.
// Caller must hold s.mu.
func (s *Service) removeTagsForPathLocked(rel string) {
	for tag, paths := range s.tagIndex {
		delete(paths, rel)
		if len(paths) == 0 {
			delete(s.tagIndex, tag)
		}
	}
}

// indexTags adds a file's tags to the tag index. Caller must hold s.mu.
func (s *Service) indexTags(path string, tags []string) {
	for _, t := range tags {
		if t == "" {
			continue
		}
		if _, ok := s.tagIndex[t]; !ok {
			s.tagIndex[t] = make(map[string]struct{})
		}
		s.tagIndex[t][path] = struct{}{}
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// DIRTY TRACKING
// ═══════════════════════════════════════════════════════════════════════════════
//
// The dirty map accumulates filesystem changes between Refresh() calls. The
// markDirty function implements a simple state machine to coalesce events
// (e.g., a rapid delete+create becomes "recreated").

// markDirty records that a path needs revalidation. It implements state
// transitions to handle edge cases like rapid delete+create sequences.
//
// State machine:
//
//	              ┌─────────────┐
//	Created ──────│             │────── Modified
//	              │   (path)    │
//	Removed ◄─────│             │──────► Removed (sticky)
//	    │         └─────────────┘
//	    │              ▲
//	    │    Created   │
//	    └──────────────┴───► Recreated
func (s *Service) markDirty(absPath string, kind DirtyKind) {
	rel, err := s.relPath(absPath)
	if err != nil {
		return
	}
	if rel == "" {
		return
	}
	// Admission owns note-format selection. Keep the event key format-neutral
	// so a selected provider is never rewritten as a Markdown path.
	rel = string(paths.Normalize(rel))

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.dirty[rel]; ok {
		// State transitions for edge cases:

		// Created/Modified → Renamed: keep the rename so we rescan the parent dir.
		//
		// This happens in "atomic write" patterns where a temp file is created and
		// then renamed into place, and some platforms only report the rename on
		// the temporary path. If we kept DirtyCreated, we'd ignore the change
		// because the temp file isn't a markdown file.
		if (existing == DirtyCreated || existing == DirtyModified) && kind == DirtyRenamed {
			s.dirty[rel] = DirtyRenamed
			return
		}

		// Removed → Created/Modified = Recreated (rapid delete+create)
		if existing == DirtyRemoved && (kind == DirtyCreated || kind == DirtyModified) {
			s.dirty[rel] = DirtyRecreated
			return
		}

		// Recreated → Removed = Removed (the recreation was also deleted)
		if existing == DirtyRecreated && kind == DirtyRemoved {
			s.dirty[rel] = DirtyRemoved
			return
		}

		// Removed is sticky: once removed, stay removed (until recreation detected).
		if existing == DirtyRemoved || kind == DirtyRemoved {
			s.dirty[rel] = DirtyRemoved
		}
		return
	}
	s.dirty[rel] = kind
}

// bumpVersion increments the version counter. Called after processing changes.
func (s *Service) bumpVersion() {
	s.mu.Lock()
	s.version++
	s.mu.Unlock()
}

// markStale flags the cache as potentially out of sync. The next Refresh()
// starts a background recrawl.
func (s *Service) markStale() {
	s.mu.Lock()
	s.stale = true
	s.mu.Unlock()
	s.metrics.staleFlips.Add(1)
}

// Metrics is a lightweight snapshot of cache observability counters.
type Metrics struct {
	StaleCount          uint64
	ResyncCount         uint64
	LastResyncDuration  time.Duration
	LastDirtyBatchCount int
}

type serviceMetrics struct {
	staleFlips      atomic.Uint64
	resyncs         atomic.Uint64
	lastResyncNanos atomic.Int64
	lastDirtyBatch  atomic.Int64
}

// Metrics returns a snapshot of internal counters for observability.
func (s *Service) Metrics() Metrics {
	return Metrics{
		StaleCount:          s.metrics.staleFlips.Load(),
		ResyncCount:         s.metrics.resyncs.Load(),
		LastResyncDuration:  time.Duration(s.metrics.lastResyncNanos.Load()),
		LastDirtyBatchCount: int(s.metrics.lastDirtyBatch.Load()),
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// UTILITIES
// ═══════════════════════════════════════════════════════════════════════════════
//
// Helper functions for path filtering and tag normalization.

// shouldExclude checks if a relative path should be skipped based on the service's
// unified ignore matcher.
func (s *Service) shouldExclude(relPath string) bool {
	s.mu.RLock()
	matcher := s.ignoreMatcher
	hardMatcher := s.hardIgnoreMatcher
	s.mu.RUnlock()
	if ignore.IsSystemContextPath(relPath) {
		if ignore.IsDefaultInfrastructurePath(relPath) {
			return true
		}
		matcher = hardMatcher
	}
	if matcher == nil {
		return false
	}
	return matcher.IsIgnored(relPath, false)
}

// normalizeTags converts tags to lowercase and removes # prefixes.
func normalizeTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		nt := strings.TrimSpace(strings.TrimPrefix(t, "#"))
		nt = strings.ToLower(nt)
		if nt != "" {
			out = append(out, nt)
		}
	}
	return out
}

// stripHashtagPrefix removes # from tags (used before normalization).
func stripHashtagPrefix(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		out = append(out, strings.TrimPrefix(t, "#"))
	}
	return out
}

// ═══════════════════════════════════════════════════════════════════════════════
// CODE REFERENCE SCANNING
// ═══════════════════════════════════════════════════════════════════════════════
//
// These functions handle scanning source code files for references to vault notes.
// Code refs are opt-in via cache.Options.CodeRefConfig.

// CodeRefConfig returns the code reference configuration, or nil if disabled.
func (s *Service) CodeRefConfig() *coderefs.Config {
	return s.codeRefConfig
}

// CodeRefsByNote returns all code references pointing to notes.
// Returns nil if code ref scanning is disabled.
func (s *Service) CodeRefsByNote() map[string][]coderefs.CodeRef {
	if s.codeRefIndex == nil {
		return nil
	}
	return s.codeRefIndex.AllRefsByNote()
}

// CodeRefsByFile returns all code references grouped by source file.
// Returns nil if code ref scanning is disabled.
func (s *Service) CodeRefsByFile() map[string][]coderefs.CodeRef {
	if s.codeRefIndex == nil {
		return nil
	}
	return s.codeRefIndex.AllRefsByFile()
}

// rebuildNotePathCache builds the NotePathCache from current fileIndex.
// Called after initial crawl and when markdown files change.
//
// Alias metadata: we collect frontmatter `aliases:` lists so identifier-style
// wikilinks such as `[[SPEC-001]]` can resolve after real note path/title
// matches. Duplicate aliases remain validation issues; the cache retains all
// claimants and refuses ambiguous alias resolution.
func (s *Service) rebuildNotePathCache() {
	s.mu.RLock()
	paths := make([]string, 0, len(s.fileIndex))
	aliases := make(map[string][]string, len(s.fileIndex))
	for p, entry := range s.fileIndex {
		paths = append(paths, p)
		if entry == nil {
			continue
		}
		if list := aliasListFromFrontmatter(entry.Frontmatter); len(list) > 0 {
			aliases[p] = list
		}
	}
	s.mu.RUnlock()

	cache := obsidian.BuildNotePathCacheWithAliases(paths, aliases)

	s.mu.Lock()
	s.notePathCache = cache
	s.mu.Unlock()
}

// aliasListFromFrontmatter pulls `aliases:` values out of a parsed
// frontmatter map. Accepts scalar and list values like the metadata
// index's alias projection.
func aliasListFromFrontmatter(fm map[string]interface{}) []string {
	if len(fm) == 0 {
		return nil
	}
	raw, ok := fm["aliases"]
	if !ok {
		return nil
	}
	switch current := raw.(type) {
	case []interface{}:
		out := make([]string, 0, len(current))
		for _, item := range current {
			if s, ok := item.(string); ok {
				if trimmed := strings.TrimSpace(s); trimmed != "" {
					out = append(out, trimmed)
				}
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(current))
		for _, item := range current {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case string:
		if trimmed := strings.TrimSpace(current); trimmed != "" {
			return []string{trimmed}
		}
	}
	return nil
}

func noteBaseNames(paths []string) []string {
	seen := make(map[string]struct{})
	for _, p := range paths {
		noExt := strings.TrimSuffix(p, filepath.Ext(p))
		if noExt != "" {
			seen[strings.ToLower(noExt)] = struct{}{}
		}
		base := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		if base != "" {
			seen[strings.ToLower(base)] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	return out
}

func containsAny(haystack string, needles []string) bool {
	for _, n := range needles {
		if n == "" {
			continue
		}
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

// updateCodeRefsIncremental updates code references based on what changed.
// This is much more efficient than a full rescan:
//   - Content-only changes to notes: No code ref rescan needed (wikilinks are by name, not content)
//   - Deleted/renamed notes: Rescan only code files that referenced them (to remove stale refs)
//   - New notes: Full scan needed (to find new references to the new note name)
func (s *Service) updateCodeRefsIncremental(ctx context.Context, dirty map[string]DirtyKind) {
	// Categorize selected-note changes.
	var removedNotes []string  // Notes that were deleted or renamed away
	var newNotes []string      // Notes that were created
	contentOnlyChanges := true // Track if we only have content modifications

	for path, kind := range dirty {
		if !s.admitsNotePath(path) {
			continue
		}

		normalizedPath := string(paths.Normalize(path))

		switch kind {
		case DirtyRemoved, DirtyRenamed:
			// Note was deleted or renamed (old path)
			removedNotes = append(removedNotes, normalizedPath)
			contentOnlyChanges = false

		case DirtyCreated, DirtyRecreated:
			// New note appeared
			newNotes = append(newNotes, normalizedPath)
			contentOnlyChanges = false

		case DirtyModified:
			// Content-only change - no action needed for code refs
			// Wikilinks/mentions are by name, not content
		}
	}

	// If only content changes, no code ref updates needed at all
	if contentOnlyChanges {
		return
	}

	// Rebuild note path cache first (needed for resolution)
	s.rebuildNotePathCache()

	s.mu.RLock()
	noteCache := s.notePathCache
	codeRefIndex := s.codeRefIndex
	s.mu.RUnlock()

	if noteCache == nil {
		return
	}

	// For removed/renamed notes: rescan code files that referenced them
	// This removes stale references from those files
	if len(removedNotes) > 0 && codeRefIndex != nil {
		impactedFiles := make(map[string]struct{})
		for _, notePath := range removedNotes {
			refs := codeRefIndex.RefsByNote(notePath)
			for _, ref := range refs {
				impactedFiles[ref.SourceFile] = struct{}{}
			}
		}

		for file := range impactedFiles {
			select {
			case <-ctx.Done():
				return
			default:
			}
			s.refreshCodeRef(file, noteCache, true)
		}
	}

	// For new notes: we need to scan all code files to find references
	if len(newNotes) > 0 {
		if err := s.scanCodeFilesForNoteNames(ctx, noteCache, newNotes); err != nil {
			log.Printf("cache: code ref scan for new notes failed: %v", err)
		}
	}
}

// scanCodeFilesForNoteNames limits rescans to code files likely to reference the provided notes.
// It heuristically filters by path substring or content substring on note basenames to avoid a full rescan.
func (s *Service) scanCodeFilesForNoteNames(ctx context.Context, noteCache *obsidian.NotePathCache, notePaths []string) error {
	if s.codeRefConfig == nil || !s.codeRefConfig.Enabled {
		return nil
	}
	names := noteBaseNames(notePaths)
	if len(names) == 0 {
		return nil
	}
	codeFiles, err := s.discoverCodeFiles()
	if err != nil {
		return fmt.Errorf("discover code files: %w", err)
	}

	for _, relPath := range codeFiles {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		absPath := s.absPath(relPath)
		if absPath == "" {
			continue
		}

		// Path-based heuristic
		lowerPath := strings.ToLower(relPath)
		pathHit := containsAny(lowerPath, names)

		content, err := os.ReadFile(absPath)
		if err != nil {
			// If the file disappeared, ensure we drop stale refs
			s.removeCodeFile(relPath)
			continue
		}

		match := pathHit
		if !match {
			// Content-based heuristic (case-insensitive substring)
			lowerContent := strings.ToLower(string(content))
			match = containsAny(lowerContent, names)
		}
		if !match {
			continue
		}

		info, err := os.Stat(absPath)
		if err != nil || info.IsDir() {
			s.removeCodeFile(relPath)
			continue
		}

		refs, err := coderefs.ScanFile(relPath, content, noteCache)
		if err != nil {
			continue
		}
		if s.codeRefIndex != nil {
			s.codeRefIndex.ReplaceFile(relPath, refs)
		}
		s.setCodeFileMeta(relPath, fileMeta{
			ModTime: info.ModTime(),
			Size:    info.Size(),
		})
	}

	return nil
}

// scanCodeFiles scans all code files matching the config patterns.
// Uses a worker pool for parallel scanning.
func (s *Service) scanCodeFiles(ctx context.Context) error {
	if s.codeRefConfig == nil || !s.codeRefConfig.Enabled {
		return nil
	}

	phaseCtx := indexingperf.WithPhase(ctx, indexingperf.AgentStartPhaseCodeRefDiscovery)
	done := indexingperf.StartSpan(phaseCtx, indexingperf.AgentStartPhaseCodeRefDiscovery)
	defer done(nil)

	// Discover code files
	codeFiles, err := s.discoverCodeFiles()
	if err != nil {
		return fmt.Errorf("discover code files: %w", err)
	}
	indexingperf.AddCount(phaseCtx, indexingperf.AgentStartOpRepoWalks, 1)
	indexingperf.AddCount(phaseCtx, indexingperf.AgentStartOpCodeReads, int64(len(codeFiles)))

	// Drop code files a previous crawl indexed that discovery no longer
	// returns; the rest are force-rescanned below.
	discovered := make(map[string]struct{}, len(codeFiles))
	for _, relPath := range codeFiles {
		discovered[string(paths.NormalizeCode(relPath))] = struct{}{}
	}
	s.mu.RLock()
	var vanished []string
	for relPath := range s.codeFileMeta {
		if _, ok := discovered[relPath]; !ok {
			vanished = append(vanished, relPath)
		}
	}
	s.mu.RUnlock()
	for _, relPath := range vanished {
		s.removeCodeFile(relPath)
	}

	if len(codeFiles) == 0 {
		return nil
	}

	// Get note path cache for resolution
	s.mu.RLock()
	noteCache := s.notePathCache
	s.mu.RUnlock()

	if noteCache == nil {
		return nil // No notes to resolve against
	}

	// Parallel scan with worker pool
	workerCount := runtime.NumCPU()
	if workerCount > len(codeFiles) {
		workerCount = len(codeFiles)
	}
	if workerCount < 1 {
		workerCount = 1
	}

	jobs := make(chan string, workerCount)
	var wg sync.WaitGroup

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for relPath := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}
				s.refreshCodeRef(relPath, noteCache, true)
			}
		}()
	}

	for _, relPath := range codeFiles {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		case jobs <- relPath:
		}
	}
	close(jobs)
	wg.Wait()

	return nil
}

// refreshCodeRef scans a single code file and updates the index.
func (s *Service) refreshCodeRef(relPath string, noteCache *obsidian.NotePathCache, force bool) bool {
	ref, err := paths.ResolveCodeRefWithVaultPaths(s.vault, relPath)
	if err != nil || ref.Rel == "" || ref.Abs == "" {
		relPath = string(paths.NormalizeCode(relPath))
		if relPath == "" {
			return false
		}
		return s.removeCodeFile(relPath)
	}
	relPath = ref.Rel.String()
	absPath := ref.Abs.String()

	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		return s.removeCodeFile(relPath)
	}
	meta := fileMeta{ModTime: info.ModTime(), Size: info.Size()}

	if !force {
		s.mu.RLock()
		metaCache := s.codeFileMeta
		prev, ok := fileMeta{}, false
		if metaCache != nil {
			prev, ok = metaCache[relPath]
		}
		s.mu.RUnlock()
		if ok && prev.ModTime.Equal(meta.ModTime) && prev.Size == meta.Size {
			return false
		}
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return s.removeCodeFile(relPath)
	}

	refs, err := coderefs.ScanFile(relPath, content, noteCache)
	if err != nil {
		return false
	}

	if s.codeRefIndex != nil {
		s.codeRefIndex.ReplaceFile(relPath, refs)
	}
	s.setCodeFileMeta(relPath, meta)
	return true
}

// isCodeRefCandidate checks if a path matches code ref include patterns.
func (s *Service) isCodeRefCandidate(relPath string) bool {
	if s.codeRefConfig == nil || !s.codeRefConfig.Enabled {
		return false
	}

	// Honor vault-level excludes (.obsidianignore, user excludes)
	if s.shouldExclude(relPath) {
		return false
	}

	// Check excludes first
	for _, excl := range s.codeRefConfig.Excludes {
		if ok, _ := doublestar.Match(excl, relPath); ok {
			return false
		}
	}

	// Check includes
	for _, incl := range s.codeRefConfig.Includes {
		if ok, _ := doublestar.Match(incl, relPath); ok {
			return true
		}
	}

	return false
}

func (s *Service) setCodeFileMeta(relPath string, meta fileMeta) {
	if s.codeFileMeta == nil {
		return
	}
	relPath = string(paths.NormalizeCode(relPath))
	s.mu.Lock()
	s.codeFileMeta[relPath] = meta
	s.mu.Unlock()
}
