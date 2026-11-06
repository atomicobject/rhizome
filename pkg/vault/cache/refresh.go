package cache

// Docs: [[Dirty tracking + Refresh semantics]], [[Watcher design + degraded mode]]

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/assetroots"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Refresh reconciles in-memory state with the filesystem.
//   - If this is the first call, it delegates to EnsureReady().
//   - If stale, it starts one background recrawl and returns immediately.
//   - Otherwise it consumes dirty markers emitted by watchers.
func (s *Service) Refresh(ctx context.Context) error {
	_, err := s.RefreshWithResult(ctx)
	return err
}

// RefreshAndDrainDirty refreshes the cache and returns the set of dirty paths
// that were consumed in this pass. The boolean reports that this pass ran the
// initial crawl.
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
	// Recrawls counts completions, not starts, so watchers force a full
	// reconcile only once the index caught up; no caller consumes it.
	startRecrawl := s.stale && !s.recrawling && s.lifetime.Err() == nil
	if startRecrawl {
		s.stale = false
		s.recrawling = true
		s.attempts++
		s.recrawlWG.Add(1)
	}
	s.recrawlStateLocked(&result)
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
// re-arms stale so the next Refresh starts another; success advances
// RefreshResult.Recrawls. MarkStale during a recrawl also
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
		s.recrawls++
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
	case assetroots.IsAssetPath(rel, assetroots.Ontology) && filepath.Ext(rel) == ".graphql":
		return true
	case assetroots.IsAssetPath(rel, assetroots.Recipes):
		return filepath.Ext(rel) == ".yaml" || filepath.Ext(rel) == ".yml"
	default:
		return false
	}
}

// markDirty records that a path needs revalidation. It implements state
// transitions to handle edge cases like rapid delete+create sequences.
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
