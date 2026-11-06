package cache

// Docs: [[Vault cache service (Service)]], [[Ignore + exclude rules]]

import (
	"context"
	"errors"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// initialCrawl walks the vault and reconciles the cache with it.
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
				if strings.HasPrefix(d.Name(), ".") && path != s.vaultPath {
					return filepath.SkipDir
				}
				if rel != "." && matcher != nil && matcher.IsIgnoredShallow(rel, true) && hardMatcher != nil && hardMatcher.IsIgnoredShallow(rel, true) {
					return filepath.SkipDir
				}
				return nil
			}

			// Skip hidden files and files the current projection policy rejects.
			if strings.HasPrefix(d.Name(), ".") || !s.admitsNotePath(rel) {
				return nil
			}

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

	// Discovery is authoritative. Evict entries it no longer returns
	// (deleted, newly ignored, or rejected by a replaced selection policy)
	// before reading anything, so a canceled crawl still reports them gone.
	// shortcut: an entry deleted and recreated while discovery ran is evicted
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

	// Reads are intentionally serial today:
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

	// Code reference resolution depends on the complete note set, so keep it after markdown
	// discovery rather than interleaving it with note reads.
	if s.codeRefConfig != nil && s.codeRefConfig.Enabled {
		s.rebuildNotePathCache()
		if err := s.scanCodeFiles(ctx); err != nil {
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

		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

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

		if noteCache != nil && s.isCodeRefCandidate(rel) {
			relCode := string(paths.NormalizeCode(rel))
			if s.refreshCodeRef(relCode, noteCache, false) {
				changedPaths[relCode] = struct{}{}
			}
		}
	}
	return changedPaths, nil
}

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
