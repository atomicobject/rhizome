package web

import (
	"bytes"
	"container/list"
	"context"
	"encoding/json"
	"log"
	"log/slog"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// defaultReadCacheBytes bounds one vault's read cache (SPEC-0136 R4).
const defaultReadCacheBytes int64 = 64 << 20

// readCacheEntryOverhead approximates the list element, map slot, and key
// headers so many tiny entries still count against the bound.
const readCacheEntryOverhead = 128

// readRun executes one read and reports whether its body may be stored.
// It must decide storability itself: no error in the body, an unchanged
// generation since the key's generation was read, and a ready index.
type readRun func(ctx context.Context) (body []byte, storable bool, err error)

// readCache is the byte-bounded LRU shared by public GraphQL and view
// execution. Keys start with the generation read before execution, so a
// changed generation simply misses; nothing is invalidated in place.
type readCache struct {
	mu       sync.Mutex
	maxBytes int64
	bytes    int64
	order    *list.List // front is most recently used
	entries  map[string]*list.Element
	flights  map[string]*readFlight
	closed   bool
	stats    readCacheStats
	// verifyRate is the fraction of hits re-executed to detect a stale entry.
	verifyRate float64
	// record receives each statistics window; nil discards them.
	record func(ReadCacheMetrics)

	ctx    context.Context
	cancel context.CancelFunc
	work   sync.WaitGroup
}

type readCacheEntry struct {
	key  string
	body []byte
}

type readFlight struct {
	done chan struct{}
	body []byte
	err  error
}

func newReadCache(maxBytes int64, verifyRate float64) *readCache {
	ctx, cancel := context.WithCancel(context.Background())
	return &readCache{
		maxBytes:   maxBytes,
		order:      list.New(),
		entries:    map[string]*list.Element{},
		flights:    map[string]*readFlight{},
		stats:      newReadCacheStats(),
		verifyRate: verifyRate,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Close cancels detached executions and waits for them and for hit
// verifications, so none outlives the stores it reads.
func (c *readCache) Close() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.cancel()
	c.work.Wait()
	c.flush(true)
}

func (c *readCache) flush(force bool) {
	c.mu.Lock()
	metrics, ok := c.takeMetricsLocked(force)
	c.mu.Unlock()
	if ok && c.record != nil {
		c.record(metrics)
	}
}

// do returns the cached body for key, or joins or starts its one detached
// execution. A caller that started the execution receives its error; callers
// that joined it run on their own instead, so one failure fails one caller.
func (c *readCache) do(ctx context.Context, key, queryHash string, run readRun) ([]byte, error) {
	c.flush(false)
	c.mu.Lock()
	if element, ok := c.entries[key]; ok {
		c.order.MoveToFront(element)
		body := element.Value.(*readCacheEntry).body
		c.stats.hit(queryHash)
		verify := c.verifyRate > 0 && !c.closed && rand.Float64() < c.verifyRate
		if verify {
			c.work.Add(1)
		}
		c.mu.Unlock()
		if verify {
			go c.verify(ctx, queryHash, body, run)
		}
		return body, nil
	}
	flight, joined := c.flights[key]
	if !joined {
		if c.closed {
			c.mu.Unlock()
			body, _, err := run(ctx)
			return body, err
		}
		flight = &readFlight{done: make(chan struct{})}
		c.flights[key] = flight
		c.work.Add(1)
		detached, release := c.detach(ctx)
		go c.execute(detached, release, key, queryHash, flight, run)
	}
	c.stats.miss(queryHash, joined)
	c.mu.Unlock()

	select {
	case <-flight.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if flight.err != nil && joined {
		body, _, err := run(ctx)
		return body, err
	}
	return flight.body, flight.err
}

// detach keeps the caller's values (vault environment, diagnostics identity)
// but not its cancellation; only Close cancels a shared execution.
func (c *readCache) detach(ctx context.Context) (context.Context, func()) {
	detached, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stop := context.AfterFunc(c.ctx, cancel)
	return detached, func() {
		stop()
		cancel()
	}
}

func (c *readCache) execute(ctx context.Context, release func(), key, queryHash string, flight *readFlight, run readRun) {
	defer c.work.Done()
	start := time.Now()
	body, storable, err := run(ctx)
	elapsed := time.Since(start)
	release()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats.executed(queryHash, elapsed)
	if err == nil && storable {
		c.storeLocked(key, queryHash, body)
	}
	flight.body, flight.err = body, err
	delete(c.flights, key)
	close(flight.done)
}

func (c *readCache) storeLocked(key, queryHash string, body []byte) {
	size := entrySize(key, body)
	if size > c.maxBytes {
		return
	}
	if element, ok := c.entries[key]; ok {
		c.removeLocked(element)
	}
	c.entries[key] = c.order.PushFront(&readCacheEntry{key: key, body: body})
	c.bytes += size
	c.stats.stored(queryHash, size)
	for c.bytes > c.maxBytes {
		c.removeLocked(c.order.Back())
	}
}

func (c *readCache) removeLocked(element *list.Element) {
	entry := c.order.Remove(element).(*readCacheEntry)
	delete(c.entries, entry.key)
	c.bytes -= entrySize(entry.key, entry.body)
}

func entrySize(key string, body []byte) int64 {
	return int64(len(key)+len(body)) + readCacheEntryOverhead
}

// verify re-executes a sampled hit after it was served. A body that differs
// while the generation is unchanged (storable) means the generation missed a
// dependency; it is counted and reported, never silently repaired.
func (c *readCache) verify(ctx context.Context, queryHash string, cached []byte, run readRun) {
	defer c.work.Done()
	detached, release := c.detach(ctx)
	body, storable, err := run(detached)
	release()
	if err != nil || !storable || bytes.Equal(body, cached) {
		return
	}
	c.mu.Lock()
	c.stats.mismatch(queryHash)
	c.mu.Unlock()
}

// bypass records a read that never touches the cache.
func (c *readCache) bypass(queryHash string) {
	c.mu.Lock()
	c.stats.bypass(queryHash)
	c.mu.Unlock()
}

// readCacheVerifyEnv sets the fraction of read-cache hits re-executed and
// compared, from 0 (off, the default) to 1 (every hit). A mismatch is counted
// in the web.read_cache diagnostics event and logged as a warning.
const readCacheVerifyEnv = "RZM_READ_CACHE_VERIFY"

func (s *Server) newReadCache() *readCache {
	rate, _ := strconv.ParseFloat(strings.TrimSpace(os.Getenv(readCacheVerifyEnv)), 64)
	cache := newReadCache(defaultReadCacheBytes, min(max(rate, 0), 1))
	cache.record = s.recordReadCacheMetrics
	return cache
}

func (s *Server) recordReadCacheMetrics(metrics ReadCacheMetrics) {
	level := slog.LevelInfo
	for _, query := range metrics.Queries {
		if query.Mismatches > 0 {
			level = slog.LevelWarn
		}
	}
	if level == slog.LevelWarn {
		log.Printf("read cache: a verified hit differed from a fresh read at the same generation; see rzm diagnostics perf")
	}
	if s.diagnostics == nil {
		return
	}
	encoded, err := json.Marshal(metrics)
	if err != nil {
		return
	}
	s.diagnostics.EventMetrics(context.Background(), level, "web", readCacheEvent, "", encoded)
}

// readCacheEvent names the server read-cache statistics event that
// rzm diagnostics perf aggregates.
const readCacheEvent = "web.read_cache"
