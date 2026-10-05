package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

type Runtime struct {
	Dimensions atomic.Int64
	VecReady   sync.Map

	writeMu   sync.Mutex
	sharedMu  *sync.Mutex
	writeInfo writeStats
	closeFn   func() error
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

type OpenDBOptions struct {
	TxLockMode string
	Pool       sqliteutil.Options
}

func OpenDB(path string, txLockMode string) (*sql.DB, error) {
	return OpenDBWithOptions(path, OpenDBOptions{TxLockMode: txLockMode})
}

func OpenDBWithOptions(path string, opts OpenDBOptions) (*sql.DB, error) {
	if path == "" {
		return nil, errors.New("sqlite path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create index directory: %w", err)
	}
	return sqliteutil.OpenDSN(sqliteutil.DSNWithOptions(path, sqliteutil.DSNOptions{
		TxLockMode: opts.TxLockMode,
	}), opts.Pool)
}

func NewRuntime(label string, closeFn func() error) Runtime {
	return Runtime{
		writeInfo: writeStats{label: label},
		closeFn:   closeFn,
	}
}

func (r *Runtime) SetWriteMu(mu *sync.Mutex) {
	if r == nil {
		return
	}
	r.sharedMu = mu
}

func (r *Runtime) WriteLock() *sync.Mutex {
	return r.writeLock()
}

func (r *Runtime) WithWrite(ctx context.Context, db *sql.DB, fn func(context.Context, *sql.DB) error) error {
	mu := r.writeLock()
	waitStart := time.Now()
	mu.Lock()
	wait := time.Since(waitStart)
	holdStart := time.Now()
	err := fn(ctx, db)
	hold := time.Since(holdStart)
	mu.Unlock()
	r.writeInfo.record(ctx, wait, hold)
	indexingperf.ObserveDBWrite(ctx, r.writeInfo.label, wait, hold)
	return err
}

func (r *Runtime) WithWriteTx(ctx context.Context, db *sql.DB, exec func(context.Context, *sql.DB, func(*sql.Tx) error) error, fn func(*sql.Tx) error) error {
	mu := r.writeLock()
	waitStart := time.Now()
	mu.Lock()
	wait := time.Since(waitStart)
	holdStart := time.Now()
	err := exec(ctx, db, fn)
	hold := time.Since(holdStart)
	mu.Unlock()
	r.writeInfo.record(ctx, wait, hold)
	indexingperf.ObserveDBWrite(ctx, r.writeInfo.label, wait, hold)
	return err
}

func (r *Runtime) Close(db *sql.DB) error {
	if r == nil {
		return nil
	}
	if r.closeFn != nil {
		return r.closeFn()
	}
	if db == nil {
		return nil
	}
	return db.Close()
}

func (r *Runtime) writeLock() *sync.Mutex {
	if r != nil && r.sharedMu != nil {
		return r.sharedMu
	}
	return &r.writeMu
}

func (w *writeStats) record(ctx context.Context, wait, hold time.Duration) {
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
		if recorder := diagnostics.FromContext(ctx); recorder != nil {
			recorder.Logger("sqlite").InfoContext(ctx, "SQLite write statistics", slog.String("event", "writes.summary"), slog.String("store", w.label), slog.Int64("writes", w.count), slog.Int64("wait_ns", int64(w.totalWait)), slog.Int64("hold_ns", int64(w.totalHold)), slog.Int64("avg_wait_ns", int64(avgWait)), slog.Int64("avg_hold_ns", int64(avgHold)))
		} else {
			log.Printf("[index] %s writes=%d wait=%s avg_wait=%s hold=%s avg_hold=%s",
				w.label,
				w.count,
				w.totalWait.Truncate(time.Millisecond),
				avgWait.Truncate(time.Millisecond),
				w.totalHold.Truncate(time.Millisecond),
				avgHold.Truncate(time.Millisecond),
			)
		}
		w.lastLog = now
	}
	w.mu.Unlock()
}
