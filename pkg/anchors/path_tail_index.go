package codeanchor

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// PathTailIndex provides conservative “unique path tail” matching.
// It is intended as a best-effort fallback when language-specific module resolution fails.
//
// Keys are stored without file extensions, using POSIX separators.
// Example: "/repo/apps/web/src/components/Button.tsx" yields keys like:
// - "Button"
// - "components/Button"
// - "src/components/Button"
//
// The index is safe for concurrent use.
type PathTailIndex struct {
	mu sync.RWMutex

	// maxSegments caps how deep we index path tails for each file.
	maxSegments int

	// tail -> set(paths)
	tailToPaths map[string]map[string]struct{}

	// path -> tails (for efficient removal)
	pathToTails map[string][]string
}

// TailQuery controls a path-tail lookup.
type TailQuery struct {
	// Tail is a path suffix such as "components/Button" or "Button".
	// It may include an extension; extensions are ignored during lookup.
	Tail string

	// AllowedExts filters candidates by file extension (e.g. ".ts", ".tsx").
	// When empty, no extension filtering is performed.
	AllowedExts []string

	// PreferUnder, when provided, is a list of absolute directory prefixes.
	// If multiple candidates exist, but exactly one candidate is under any of these
	// prefixes, that candidate is returned.
	PreferUnder []string
}

// NewPathTailIndex constructs a new index.
// If maxSegments <= 0, a default of 5 is used.
func NewPathTailIndex(maxSegments int) *PathTailIndex {
	if maxSegments <= 0 {
		maxSegments = 5
	}
	return &PathTailIndex{
		maxSegments: maxSegments,
		tailToPaths: make(map[string]map[string]struct{}),
		pathToTails: make(map[string][]string),
	}
}

// Add indexes a file path. The path is normalized for deterministic behavior.
func (p *PathTailIndex) Add(absPath string) {
	absPath = paths.ResolveSymlinks(absPath).String()
	if absPath == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Re-add: remove old keys first so we don't leak.
	if old := p.pathToTails[absPath]; len(old) > 0 {
		p.removeLocked(absPath)
	}

	tails := buildPathTailKeys(absPath, p.maxSegments)
	if len(tails) == 0 {
		return
	}
	p.pathToTails[absPath] = tails
	for _, tail := range tails {
		set := p.tailToPaths[tail]
		if set == nil {
			set = make(map[string]struct{})
			p.tailToPaths[tail] = set
		}
		set[absPath] = struct{}{}
	}
}

// Remove removes a file path from the index.
func (p *PathTailIndex) Remove(absPath string) {
	absPath = paths.ResolveSymlinks(absPath).String()
	if absPath == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.removeLocked(absPath)
}

func (p *PathTailIndex) removeLocked(absPath string) {
	tails := p.pathToTails[absPath]
	if len(tails) == 0 {
		return
	}
	for _, tail := range tails {
		set := p.tailToPaths[tail]
		if set == nil {
			continue
		}
		delete(set, absPath)
		if len(set) == 0 {
			delete(p.tailToPaths, tail)
		}
	}
	delete(p.pathToTails, absPath)
}

// Query returns a unique plausible match, or ok=false if ambiguous/none.
func (p *PathTailIndex) Query(q TailQuery) (matchPath string, ok bool) {
	tail := normalizeTailKey(q.Tail)
	if tail == "" {
		return "", false
	}

	p.mu.RLock()
	set := p.tailToPaths[tail]
	if len(set) == 0 {
		p.mu.RUnlock()
		return "", false
	}

	// Copy candidates under read lock to avoid holding while filtering.
	candidates := make([]string, 0, len(set))
	for path := range set {
		candidates = append(candidates, path)
	}
	p.mu.RUnlock()

	candidates = filterByExt(candidates, q.AllowedExts)
	if len(candidates) == 1 {
		return candidates[0], true
	}
	if len(candidates) == 0 {
		return "", false
	}

	// PreferUnder tiebreak.
	if len(q.PreferUnder) > 0 {
		preferred := candidates[:0]
		for _, c := range candidates {
			if hasAnyPrefix(c, q.PreferUnder) {
				preferred = append(preferred, c)
			}
		}
		if len(preferred) == 1 {
			return preferred[0], true
		}
	}

	return "", false
}

func hasAnyPrefix(path string, prefixes []string) bool {
	path = filepath.ToSlash(path)
	for _, p := range prefixes {
		p = filepath.ToSlash(paths.ResolveSymlinks(p).String())
		if p == "" {
			continue
		}
		// Ensure directory boundary aware prefix match.
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

func filterByExt(paths []string, allowed []string) []string {
	if len(allowed) == 0 {
		return paths
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, e := range allowed {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		allowedSet[e] = struct{}{}
	}
	if len(allowedSet) == 0 {
		return paths
	}
	out := paths[:0]
	for _, p := range paths {
		ext := strings.ToLower(filepath.Ext(p))
		if _, ok := allowedSet[ext]; ok {
			out = append(out, p)
		}
	}
	return out
}

func buildPathTailKeys(absPath string, maxSegments int) []string {
	if absPath == "" {
		return nil
	}
	// Normalize to POSIX separators for tail keys.
	posix := filepath.ToSlash(absPath)
	parts := strings.Split(posix, "/")
	filtered := parts[:0]
	for _, p := range parts {
		if p == "" {
			continue
		}
		filtered = append(filtered, p)
	}
	if len(filtered) == 0 {
		return nil
	}
	// Strip extension from final segment.
	last := filtered[len(filtered)-1]
	last = strings.TrimSuffix(last, filepath.Ext(last))
	filtered[len(filtered)-1] = last

	if last == "" {
		return nil
	}

	n := len(filtered)
	if maxSegments > 0 && n > maxSegments {
		n = maxSegments
	}
	out := make([]string, 0, n)
	for segs := 1; segs <= n; segs++ {
		start := len(filtered) - segs
		key := strings.Join(filtered[start:], "/")
		if key != "" {
			out = append(out, key)
		}
	}
	return out
}

func normalizeTailKey(tail string) string {
	tail = strings.TrimSpace(tail)
	if tail == "" {
		return ""
	}
	tail = filepath.ToSlash(tail)
	tail = strings.TrimPrefix(tail, "./")
	tail = strings.TrimPrefix(tail, "/")
	// Drop extension from the last segment.
	segs := strings.Split(tail, "/")
	if len(segs) == 0 {
		return ""
	}
	last := segs[len(segs)-1]
	last = strings.TrimSuffix(last, filepath.Ext(last))
	segs[len(segs)-1] = last
	// Remove empty segments.
	out := make([]string, 0, len(segs))
	for _, s := range segs {
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "/")
}
