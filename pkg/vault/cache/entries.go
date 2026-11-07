package cache

// Docs: [[Vault cache service (Service)]]

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/paths"
	"strings"
)

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
