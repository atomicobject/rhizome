package paths

import (
	"os"
	"path/filepath"
	"sync"
)

// SymlinkCache memoises symlink resolution for one read request, so each
// distinct path prefix is resolved once however many paths share it. A link
// can be retargeted at any time, so a cache must never outlive its request,
// and write admission must resolve fresh (a nil cache).
type SymlinkCache struct {
	mu       sync.Mutex
	resolved map[string]symlinkEntry
	lstats   int
}

type symlinkEntry struct {
	path string
	err  error
}

// NewSymlinkCache returns an empty request-scoped cache.
func NewSymlinkCache() *SymlinkCache {
	return &SymlinkCache{resolved: map[string]symlinkEntry{}}
}

// Resolutions reports how many path prefixes the cache has resolved.
func (c *SymlinkCache) Resolutions() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lstats
}

// evalSymlinks is filepath.EvalSymlinks for an absolute path; a nil cache
// resolves fresh.
func (c *SymlinkCache) evalSymlinks(p string) (string, error) {
	if c == nil || !filepath.IsAbs(p) {
		return filepath.EvalSymlinks(p)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.evalLocked(filepath.Clean(p))
}

// evalLocked resolves the parent first, then one Lstat for the last name;
// only an actual link pays for a full resolution of its target.
func (c *SymlinkCache) evalLocked(p string) (string, error) {
	if entry, ok := c.resolved[p]; ok {
		return entry.path, entry.err
	}
	var entry symlinkEntry
	if parent := filepath.Dir(p); parent == p {
		entry.path = p
	} else if resolvedParent, err := c.evalLocked(parent); err != nil {
		entry.err = err
	} else {
		candidate := filepath.Join(resolvedParent, filepath.Base(p))
		c.lstats++
		info, err := os.Lstat(candidate)
		switch {
		case err != nil:
			entry.err = err
		case info.Mode()&os.ModeSymlink != 0:
			entry.path, entry.err = filepath.EvalSymlinks(candidate)
		default:
			entry.path = candidate
		}
	}
	c.resolved[p] = entry
	return entry.path, entry.err
}
