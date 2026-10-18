package queryrecipe

import (
	"crypto/sha256"
	"encoding/json"
	"sync"
)

// Registry caches parsed sources, never prepared queries. Content hashes also
// detect same-size rewrites whose filesystem timestamps have been preserved.
type Registry struct {
	roots   []string
	mu      sync.Mutex
	digest  [sha256.Size]byte
	loaded  bool
	recipes []Recipe
	issues  []Issue
}

func NewRegistry(roots []string) *Registry {
	return &Registry{roots: append([]string(nil), roots...)}
}

func (r *Registry) Catalog() ([]Recipe, []Issue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	files, issues := scanSources(r.roots, true)
	hash := sha256.New()
	for _, file := range files {
		_ = json.NewEncoder(hash).Encode(file.path)
		_, _ = hash.Write(file.data)
		_, _ = hash.Write([]byte{0})
	}
	_ = json.NewEncoder(hash).Encode(issues)
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	if !r.loaded || digest != r.digest {
		r.recipes, r.issues = loadSources(files, issues)
		r.digest, r.loaded = digest, true
	}
	return append([]Recipe(nil), r.recipes...), append([]Issue(nil), r.issues...)
}
