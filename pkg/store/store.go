package store

import (
	"context"
	"database/sql"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"log"
	"sync"
	"time"
)

// Docs:
// - [Code anchors (Hub)](docs/hubs/Code anchors (Hub).md)
// - [Code anchors - matching + scopes](docs/reference/analysis/code-anchors-matching-scopes.md)
// - [Code Index (Hub)](docs/hubs/Code Index (Hub).md)
// - [Code Intel - Data model](docs/reference/domain/Code Intel - Data model (anchors, sections, edges).md)
// - [Code Index - Unified SQLite DB](docs/reference/analysis/Code Index - Unified SQLite DB.md)
// Store persists unified code, note, ontology, graph, session, and validation data.
type Store struct {
	db        *sql.DB
	closeFn   func() error
	writeMu   sync.Mutex
	sharedMu  *sync.Mutex
	writeInfo writeStats
	vecReady  sync.Map // map[int]struct{} keyed by embedding dimensions
	readOnly  bool
}

type writeStats struct {
	label     string
	mu        sync.Mutex
	count     int64
	totalWait time.Duration
	totalHold time.Duration
	lastLog   time.Time
}

const (
	writeStatsLogEvery    = 200
	writeStatsLogInterval = 10 * time.Second
)

func (w *writeStats) record(wait, hold time.Duration) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.count++
	w.totalWait += wait
	w.totalHold += hold
	shouldLog := w.count%writeStatsLogEvery == 0
	now := time.Now()
	if shouldLog && now.Sub(w.lastLog) >= writeStatsLogInterval {
		avgWait := time.Duration(0)
		avgHold := time.Duration(0)
		if w.count > 0 {
			avgWait = w.totalWait / time.Duration(w.count)
			avgHold = w.totalHold / time.Duration(w.count)
		}
		log.Printf("[index] %s writes=%d wait=%s avg_wait=%s hold=%s avg_hold=%s",
			w.label,
			w.count,
			w.totalWait.Truncate(time.Millisecond),
			avgWait.Truncate(time.Millisecond),
			w.totalHold.Truncate(time.Millisecond),
			avgHold.Truncate(time.Millisecond),
		)
		w.lastLog = now
	}
	w.mu.Unlock()
}

func (s *Store) writeLock() *sync.Mutex {
	if s != nil && s.sharedMu != nil {
		return s.sharedMu
	}
	return &s.writeMu
}

// DB returns the underlying sqlite handle (shared by unified indexes).
func (s *Store) DB() *sql.DB {
	if s == nil {
		return nil
	}
	return s.db
}

// SetWriteMu overrides the default write mutex (used to serialize writes across stores).
func (s *Store) SetWriteMu(mu *sync.Mutex) {
	if s == nil {
		return
	}
	s.sharedMu = mu
}

func (s *Store) withWrite(ctx context.Context, fn func(ctx context.Context, db *sql.DB) error) error {
	mu := s.writeLock()
	waitStart := time.Now()
	mu.Lock()
	wait := time.Since(waitStart)
	holdStart := time.Now()
	err := fn(ctx, s.db)
	hold := time.Since(holdStart)
	mu.Unlock()
	s.writeInfo.record(wait, hold)
	indexingperf.ObserveDBWrite(ctx, s.writeInfo.label, wait, hold)
	return err
}

func (s *Store) withWriteTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	mu := s.writeLock()
	waitStart := time.Now()
	mu.Lock()
	wait := time.Since(waitStart)
	holdStart := time.Now()
	err := sqliteutil.ExecTxWithRetry(ctx, s.db, fn)
	hold := time.Since(holdStart)
	mu.Unlock()
	s.writeInfo.record(wait, hold)
	indexingperf.ObserveDBWrite(ctx, s.writeInfo.label, wait, hold)
	return err
}

var _ codeanchor.Store = (*Store)(nil)
