package rerank

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

// CachedReranker memoizes rerank scores in SQLite so repeated live runs over
// identical query/document pairs reuse identical scores instead of re-calling
// the provider. It shares the file named by RHIZOME_EMBEDDING_CACHE with the
// embedding cache, in its own table.
type CachedReranker struct {
	inner  Reranker
	db     *sql.DB
	prefix string // provider\x00model\x00
	mu     sync.Mutex
}

// NewCachedReranker wraps inner with a content-hash score cache stored at path.
func NewCachedReranker(inner Reranker, cfg ProviderConfig, path string) (*CachedReranker, error) {
	db, err := sqliteutil.OpenDSN(sqliteutil.DSN(path), sqliteutil.Options{})
	if err != nil {
		return nil, fmt.Errorf("open rerank cache %s: %w", path, err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS rerank_cache (
		key TEXT PRIMARY KEY,
		score REAL NOT NULL
	)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create rerank cache %s: %w", path, err)
	}
	prefix := strings.Join([]string{strings.ToLower(strings.TrimSpace(cfg.Provider)), cfg.Model, ""}, "\x00")
	return &CachedReranker{inner: inner, db: db, prefix: prefix}, nil
}

func (r *CachedReranker) Close() error { return r.db.Close() }

func (r *CachedReranker) key(query, text string) string {
	sum := sha256.Sum256([]byte(r.prefix + query + "\x00" + text))
	return hex.EncodeToString(sum[:])
}

func (r *CachedReranker) Rerank(ctx context.Context, query string, docs []Document) ([]float64, error) {
	if len(docs) == 0 {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	out := make([]float64, len(docs))
	keys := make([]string, len(docs))
	missIdx := make([]int, 0, len(docs))
	missDocs := make([]Document, 0, len(docs))
	for i, doc := range docs {
		keys[i] = r.key(query, doc.Text)
		if score, ok := r.lookup(ctx, keys[i]); ok {
			out[i] = score
			continue
		}
		missIdx = append(missIdx, i)
		missDocs = append(missDocs, doc)
	}
	if len(missDocs) == 0 {
		return out, nil
	}
	fresh, err := r.inner.Rerank(ctx, query, missDocs)
	if err != nil {
		return nil, err
	}
	if len(fresh) != len(missDocs) {
		return nil, fmt.Errorf("rerank cache: provider returned %d scores for %d documents", len(fresh), len(missDocs))
	}
	for n, i := range missIdx {
		out[i] = fresh[n]
	}
	if err := r.store(ctx, missIdx, keys, fresh); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *CachedReranker) lookup(ctx context.Context, key string) (float64, bool) {
	var score float64
	if err := r.db.QueryRowContext(ctx, `SELECT score FROM rerank_cache WHERE key = ?`, key).Scan(&score); err != nil {
		return 0, false
	}
	return score, true
}

func (r *CachedReranker) store(ctx context.Context, missIdx []int, keys []string, fresh []float64) error {
	// ponytail: one mutex for all writes, matching the embedding cache; per-key
	// locking only if contention shows up.
	r.mu.Lock()
	defer r.mu.Unlock()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO rerank_cache (key, score) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for n, i := range missIdx {
		if _, err := stmt.ExecContext(ctx, keys[i], fresh[n]); err != nil {
			return err
		}
	}
	return tx.Commit()
}
