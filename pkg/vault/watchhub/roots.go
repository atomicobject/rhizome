package watchhub

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/paths"
)

type RootKind string

const (
	RootNotes    RootKind = "notes"
	RootCode     RootKind = "code"
	RootInternal RootKind = "internal"
)

type RootOptions struct {
	Kind           RootKind
	Recursive      bool
	IncludeHidden  bool
	WatchForIgnore bool
}

type RootEntry struct {
	Path    string
	RelPath string
	Options RootOptions
}

func (r RootEntry) includesDirectory(path string) bool {
	if r.Options.IncludeHidden {
		return true
	}
	relative := strings.TrimPrefix(paths.NormalizeAbsPathForCompare(path), r.Path)
	for _, segment := range strings.Split(relative, "/") {
		if strings.HasPrefix(segment, ".") {
			return false
		}
	}
	return true
}

type RootRegistry struct {
	mu               sync.RWMutex
	roots            []RootEntry
	watchedDirs      map[string]string
	walkedRoots      map[string]struct{}
	recursiveBackend bool
}

func NewRootRegistry() *RootRegistry {
	return &RootRegistry{
		watchedDirs: make(map[string]string),
		walkedRoots: make(map[string]struct{}),
	}
}

func (r *RootRegistry) Add(path string, opts RootOptions) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	abs := paths.NormalizeAbsPathForCompare(path)
	if abs == "" {
		abs = filepath.ToSlash(filepath.Clean(path))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, root := range r.roots {
		if root.Path == abs {
			return nil
		}
	}
	r.roots = append(r.roots, RootEntry{Path: abs, RelPath: "", Options: opts})
	return nil
}

func (r *RootRegistry) Remove(path string) {
	abs := paths.NormalizeAbsPathForCompare(path)
	if abs == "" {
		abs = filepath.ToSlash(filepath.Clean(path))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []RootEntry
	for _, root := range r.roots {
		if root.Path == abs {
			continue
		}
		out = append(out, root)
	}
	r.roots = out
	delete(r.walkedRoots, abs)
}

func (r *RootRegistry) List() []RootEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RootEntry, len(r.roots))
	copy(out, r.roots)
	return out
}

func (r *RootRegistry) MatchRoot(absPath string) string {
	return r.matchRoot(absPath).Path
}

func (r *RootRegistry) matchRoot(absPath string) RootEntry {
	absPath = paths.NormalizeAbsPathForCompare(absPath)
	r.mu.RLock()
	defer r.mu.RUnlock()
	var match RootEntry
	for _, root := range r.roots {
		if root.Path == "" {
			continue
		}
		if absPath == root.Path || strings.HasPrefix(absPath, root.Path+"/") {
			if len(root.Path) > len(match.Path) {
				match = root
			}
		}
	}
	return match
}

// SetRecursiveBackend sets whether the current backend supports recursive watching.
// Called by Hub after backend creation.
func (r *RootRegistry) SetRecursiveBackend(recursive bool) {
	r.mu.Lock()
	r.recursiveBackend = recursive
	r.mu.Unlock()
}

// IsRecursiveBackend returns true if the current backend supports recursive watching.
// On macOS with FSEvents, this returns true (watches entire trees).
// On Linux/Windows with fsnotify, this returns false (watches individual directories).
func (r *RootRegistry) IsRecursiveBackend() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.recursiveBackend
}

func (r *RootRegistry) TrackWatch(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// Use case-insensitive key on Windows/macOS
	key := paths.CaseKey(filepath.ToSlash(filepath.Clean(path)))
	if _, ok := r.watchedDirs[key]; ok {
		return false
	}
	r.watchedDirs[key] = path
	return true
}

func (r *RootRegistry) UntrackWatch(path string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Use case-insensitive key on Windows/macOS
	key := paths.CaseKey(filepath.ToSlash(filepath.Clean(path)))
	if _, ok := r.watchedDirs[key]; ok {
		delete(r.watchedDirs, key)
		return true
	}
	return false
}

func (r *RootRegistry) UntrackWatchTree(path string) []string {
	key := paths.CaseKey(filepath.ToSlash(filepath.Clean(path)))
	r.mu.Lock()
	defer r.mu.Unlock()
	var removed []string
	for watchedKey, watchedPath := range r.watchedDirs {
		if watchedKey == key || strings.HasPrefix(watchedKey, key+"/") {
			removed = append(removed, watchedPath)
			delete(r.watchedDirs, watchedKey)
		}
	}
	for root := range r.walkedRoots {
		rootKey := paths.CaseKey(root)
		if rootKey == key || strings.HasPrefix(rootKey, key+"/") {
			delete(r.walkedRoots, root)
		}
	}
	return removed
}

func (r *RootRegistry) HasWalked(path string) bool {
	path = paths.NormalizeAbsPathForCompare(path)
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.walkedRoots[path]
	return ok
}

func (r *RootRegistry) MarkWalked(path string) {
	path = paths.NormalizeAbsPathForCompare(path)
	r.mu.Lock()
	r.walkedRoots[path] = struct{}{}
	r.mu.Unlock()
}

func (r *RootRegistry) ResetWalked() {
	r.mu.Lock()
	r.walkedRoots = make(map[string]struct{})
	r.mu.Unlock()
}

func (r *RootRegistry) ResetWatches() {
	r.mu.Lock()
	r.watchedDirs = make(map[string]string)
	r.mu.Unlock()
}
