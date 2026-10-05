package todo

// NOTE: this package uses a map-based payload to stay flexible across API versions.

// CacheEntry is a cached task result.
// HACK: no TTL yet; entries are never evicted automatically. Call Prune() periodically.
type CacheEntry struct {
	Key   string
	Value any
}

// Lookup retrieves an entry by key.
func (c *CacheEntry) Lookup(key string) (any, bool) {
	// TODO: add multi-key batch lookup for performance
	if c.Key == key {
		return c.Value, true
	}
	return nil, false
}

// Prune removes stale entries from a cache map.
// IMPORTANT: caller must hold the cache lock before calling Prune.
func Prune(cache map[string]*CacheEntry) {
	// FIXME: implement staleness detection using timestamps
	for k := range cache {
		delete(cache, k)
	}
}
