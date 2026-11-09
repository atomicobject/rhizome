package web

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// Read-cache statistics are recorded as one web.read_cache diagnostics event
// per window (SPEC-0136 R10). Queries are identified only by hash.
const (
	readCacheStatsWindow   = time.Minute
	maxReadCacheStatHashes = 64
	readCacheOtherHash     = "other"
)

// ReadCacheQueryStats is one query hash's counts within a window.
type ReadCacheQueryStats struct {
	Hits        uint64  `json:"hits"`
	Misses      uint64  `json:"misses"`
	Joins       uint64  `json:"joins"`
	Bypasses    uint64  `json:"bypasses"`
	Executions  uint64  `json:"executions"`
	ExecTotalMs float64 `json:"exec_total_ms"`
	ExecMaxMs   float64 `json:"exec_max_ms"`
	StoredBytes int64   `json:"stored_bytes"`
	Mismatches  uint64  `json:"mismatches"`
}

// ReadCacheMetrics is the metrics object of one web.read_cache event.
type ReadCacheMetrics struct {
	Entries int                            `json:"entries"`
	Bytes   int64                          `json:"bytes"`
	Queries map[string]ReadCacheQueryStats `json:"queries"`
}

type readCacheStats struct {
	since   time.Time
	queries map[string]*ReadCacheQueryStats
}

func newReadCacheStats() readCacheStats {
	return readCacheStats{since: time.Now(), queries: map[string]*ReadCacheQueryStats{}}
}

func (s *readCacheStats) query(hash string) *ReadCacheQueryStats {
	if stats, ok := s.queries[hash]; ok {
		return stats
	}
	if len(s.queries) >= maxReadCacheStatHashes {
		hash = readCacheOtherHash
		if stats, ok := s.queries[hash]; ok {
			return stats
		}
	}
	stats := &ReadCacheQueryStats{}
	s.queries[hash] = stats
	return stats
}

func (s *readCacheStats) hit(hash string) { s.query(hash).Hits++ }

func (s *readCacheStats) miss(hash string, joined bool) {
	if joined {
		s.query(hash).Joins++
		return
	}
	s.query(hash).Misses++
}

func (s *readCacheStats) bypass(hash string)   { s.query(hash).Bypasses++ }
func (s *readCacheStats) mismatch(hash string) { s.query(hash).Mismatches++ }

func (s *readCacheStats) executed(hash string, elapsed time.Duration) {
	stats := s.query(hash)
	ms := float64(elapsed.Microseconds()) / 1000
	stats.Executions++
	stats.ExecTotalMs += ms
	stats.ExecMaxMs = max(stats.ExecMaxMs, ms)
}

func (s *readCacheStats) stored(hash string, size int64) { s.query(hash).StoredBytes += size }

// takeMetricsLocked returns the window's counts and starts a new window. It
// reports false when nothing happened, so idle servers record nothing.
func (c *readCache) takeMetricsLocked(force bool) (ReadCacheMetrics, bool) {
	if len(c.stats.queries) == 0 || !force && time.Since(c.stats.since) < readCacheStatsWindow {
		return ReadCacheMetrics{}, false
	}
	metrics := ReadCacheMetrics{Entries: len(c.entries), Bytes: c.bytes, Queries: make(map[string]ReadCacheQueryStats, len(c.stats.queries))}
	for hash, stats := range c.stats.queries {
		metrics.Queries[hash] = *stats
	}
	c.stats = newReadCacheStats()
	return metrics, true
}

// Metrics returns the current window's counts without resetting them.
func (c *readCache) Metrics() ReadCacheMetrics {
	c.mu.Lock()
	defer c.mu.Unlock()
	metrics := ReadCacheMetrics{Entries: len(c.entries), Bytes: c.bytes, Queries: make(map[string]ReadCacheQueryStats, len(c.stats.queries))}
	for hash, stats := range c.stats.queries {
		metrics.Queries[hash] = *stats
	}
	return metrics
}

// readQueryHash names a query in statistics without recording its text.
func readQueryHash(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		hash.Write([]byte(part))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))[:16]
}
