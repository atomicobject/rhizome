package cache

import (
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// discoverCodeFiles returns vault-relative paths of code files matching config patterns.
func (s *Service) discoverCodeFiles() ([]string, error) {
	if s.codeRefConfig == nil {
		return nil, nil
	}

	return s.discoverCodeFilesIn(os.DirFS(s.vaultPath))
}

func (s *Service) discoverCodeFilesIn(fsys fs.FS) ([]string, error) {
	fsys = codeDiscoveryFS{FS: fsys, skipDirectory: s.skipCodeDiscoveryDirectory}
	var codeFiles []string
	seen := make(map[string]struct{})

	for _, pattern := range s.codeRefConfig.Includes {
		// Use doublestar for glob matching
		matches, err := doublestar.Glob(fsys, pattern)
		if err != nil {
			continue // Skip invalid patterns
		}

		for _, match := range matches {
			// Check excludes
			excluded := false
			for _, excl := range s.codeRefConfig.Excludes {
				if ok, _ := doublestar.Match(excl, match); ok {
					excluded = true
					break
				}
			}
			// Also honor vault-level excludes (.obsidianignore, user excludes)
			if !excluded && s.shouldExclude(match) {
				excluded = true
			}
			if excluded {
				continue
			}

			// Skip if already seen
			if _, ok := seen[match]; ok {
				continue
			}
			seen[match] = struct{}{}

			// Skip directories
			absPath := s.absPath(match)
			if absPath == "" {
				continue
			}
			info, err := fs.Stat(fsys, match)
			if err != nil || info.IsDir() {
				continue
			}

			codeFiles = append(codeFiles, match)
		}
	}

	return codeFiles, nil
}

// Keep glob and symlink behavior, but prune excluded trees before Glob reads
// them. Filtering the final matches makes every extension walk build caches
// and dependencies before the first note workspace can use the vault cache.
type codeDiscoveryFS struct {
	fs.FS
	skipDirectory func(string) bool
}

func (f codeDiscoveryFS) Stat(name string) (fs.FileInfo, error) {
	return fs.Stat(f.FS, name)
}

func (f codeDiscoveryFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if f.skipDirectory(name) {
		return nil, nil
	}
	entries, err := fs.ReadDir(f.FS, name)
	if err != nil {
		return nil, err
	}
	kept := make([]fs.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !f.skipDirectory(path.Join(name, entry.Name())) {
			kept = append(kept, entry)
		}
	}
	return kept, nil
}

func (s *Service) skipCodeDiscoveryDirectory(name string) bool {
	if name == "." {
		return false
	}
	s.mu.RLock()
	matcher, hardMatcher := s.ignoreMatcher, s.hardIgnoreMatcher
	s.mu.RUnlock()
	// Ordinary note excludes cannot prune a possible system CONTEXT.md.
	if matcher != nil && hardMatcher != nil && matcher.IsIgnored(name, true) && hardMatcher.IsIgnored(name, true) {
		return true
	}
	for _, pattern := range s.codeRefConfig.Excludes {
		// Only subtree globs prove that every descendant is excluded. A glob
		// matching a directory name alone may still admit files inside it.
		if strings.HasSuffix(pattern, "/**") {
			if match, _ := doublestar.Match(pattern, name+"/"); match {
				return true
			}
		}
	}
	return false
}
