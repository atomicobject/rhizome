package cache

// Docs: [[Cache (Hub)]], [[Vault cache service (Service)]], [[Watcher design + degraded mode]]

import (
	"context"
	"errors"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Service maintains an in-memory cache of vault files, metadata, and tags.
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
	attempts   uint64                         // stale-triggered recrawls started; the in-flight one is number attempts
	recrawls   uint64                         // stale-triggered recrawls completed successfully
	fileIndex  map[string]*Entry              // path → cached entry
	tagIndex   map[string]map[string]struct{} // tag → set of paths
	dirty      map[string]DirtyKind           // paths needing revalidation

	// Unified ignore matcher: defaults + .gitignore + .rhizome/ignore + user excludes
	ignoreMatcher     *ignore.Matcher // computed from the current SelectionPolicy
	hardIgnoreMatcher *ignore.Matcher // excludes ordinary note-selection config

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
