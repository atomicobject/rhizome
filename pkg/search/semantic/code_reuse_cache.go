package semantic

import (
	"sync"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

type codeReuseCache struct {
	mu     sync.RWMutex
	hits   map[string]embeddings.Embedding
	misses map[string]struct{}
}

func newCodeReuseCache() *codeReuseCache {
	return &codeReuseCache{
		hits:   make(map[string]embeddings.Embedding),
		misses: make(map[string]struct{}),
	}
}

func (c *codeReuseCache) get(hash string) (embeddings.Embedding, bool, bool) {
	if c == nil || hash == "" {
		return nil, false, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if vec, ok := c.hits[hash]; ok {
		return vec, true, true
	}
	_, knownMiss := c.misses[hash]
	return nil, false, knownMiss
}

func (c *codeReuseCache) put(hash string, vec embeddings.Embedding) {
	if c == nil || hash == "" || len(vec) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hits[hash] = vec
	delete(c.misses, hash)
}

func (c *codeReuseCache) noteMiss(hash string) {
	if c == nil || hash == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.hits[hash]; ok {
		return
	}
	c.misses[hash] = struct{}{}
}

func (c *codeReuseCache) warm(rows map[string]embeddings.Embedding, requested []string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, hash := range requested {
		if hash == "" {
			continue
		}
		if vec, ok := rows[hash]; ok && len(vec) > 0 {
			c.hits[hash] = vec
			delete(c.misses, hash)
			continue
		}
		if _, ok := c.hits[hash]; ok {
			continue
		}
		c.misses[hash] = struct{}{}
	}
}
