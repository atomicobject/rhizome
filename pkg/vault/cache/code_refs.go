package cache

// Docs: [[Code reference scanning in cache]]

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/bmatcuk/doublestar/v4"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

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
		if list := obsidian.AliasListFromFrontmatter(entry.Frontmatter); len(list) > 0 {
			aliases[p] = list
		}
	}
	s.mu.RUnlock()

	cache := obsidian.BuildNotePathCacheWithAliases(paths, aliases)

	s.mu.Lock()
	s.notePathCache = cache
	s.mu.Unlock()
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
			removedNotes = append(removedNotes, normalizedPath)
			contentOnlyChanges = false

		case DirtyCreated, DirtyRecreated:
			newNotes = append(newNotes, normalizedPath)
			contentOnlyChanges = false

		case DirtyModified:
			// Content-only change - no action needed for code refs
			// Wikilinks/mentions are by name, not content
		}
	}

	if contentOnlyChanges {
		return
	}

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

	s.mu.RLock()
	noteCache := s.notePathCache
	s.mu.RUnlock()

	if noteCache == nil {
		return nil // No notes to resolve against
	}

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

	for _, excl := range s.codeRefConfig.Excludes {
		if ok, _ := doublestar.Match(excl, relPath); ok {
			return false
		}
	}

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
