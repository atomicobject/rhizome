package embeddings

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

// cachedProvider memoizes provider embeddings in SQLite so repeated live runs
// over identical text reuse bit-identical vectors instead of re-embedding.
type cachedProvider struct {
	inner       Provider
	db          *sql.DB
	fingerprint string
	prefix      string // provider\x00model\x00endpoint\x00dimensions\x00
	mu          sync.Mutex
}

// NewCachedProvider wraps inner with a content-hash embedding cache stored at path.
func NewCachedProvider(inner Provider, cfg ProviderConfig, path string) (Provider, error) {
	db, err := sqliteutil.OpenDSN(sqliteutil.DSN(path), sqliteutil.Options{})
	if err != nil {
		return nil, fmt.Errorf("open embedding cache %s: %w", path, err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS embedding_cache (
		key TEXT PRIMARY KEY,
		dimensions INTEGER NOT NULL,
		vector BLOB NOT NULL
	)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create embedding cache %s: %w", path, err)
	}
	prefix := strings.Join([]string{
		strings.ToLower(strings.TrimSpace(cfg.Provider)),
		cfg.Model,
		cfg.Endpoint,
		strconv.Itoa(cfg.Dimensions),
		"",
	}, "\x00")
	p := &cachedProvider{inner: inner, db: db, prefix: prefix, fingerprint: providerFingerprint(strings.ToLower(strings.TrimSpace(cfg.Provider)), cfg.Model, cfg.Endpoint, cfg.Dimensions)}
	if _, ok := inner.(ContextTokenProvider); ok {
		return &cachedContextProvider{cachedProvider: p}, nil
	}
	return p, nil
}

func (p *cachedProvider) Dimensions() int { return p.inner.Dimensions() }

func (p *cachedProvider) Close() error { return p.db.Close() }

func (p *cachedProvider) key(text string) string {
	sum := sha256.Sum256([]byte(p.prefix + text))
	return hex.EncodeToString(sum[:])
}

func (p *cachedProvider) EmbedTexts(ctx context.Context, texts []string) ([]Embedding, error) {
	indexingperf.ObserveProviderFingerprint(ctx, p.fingerprint)
	out := make([]Embedding, len(texts))
	missIdx := make([]int, 0, len(texts))
	missTexts := make([]string, 0, len(texts))
	keys := make([]string, len(texts))
	want := p.inner.Dimensions()
	indexingperf.AddCount(ctx, "provider.cache.inputs", int64(len(texts)))
	for i, text := range texts {
		keys[i] = p.key(text)
		vec, ok := p.lookup(ctx, keys[i], want)
		if ok {
			indexingperf.AddCount(ctx, "provider.cache.hit", 1)
			out[i] = vec
			continue
		}
		indexingperf.AddCount(ctx, "provider.cache.miss", 1)
		missIdx = append(missIdx, i)
		missTexts = append(missTexts, text)
	}
	if len(missTexts) == 0 {
		return out, nil
	}
	fresh, err := p.inner.EmbedTexts(ctx, missTexts)
	if err != nil {
		return nil, err
	}
	if len(fresh) != len(missTexts) {
		return nil, fmt.Errorf("embedding cache: provider returned %d vectors for %d texts", len(fresh), len(missTexts))
	}
	for n, i := range missIdx {
		out[i] = fresh[n]
	}
	if err := p.store(ctx, missIdx, keys, fresh); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *cachedProvider) lookup(ctx context.Context, key string, want int) (Embedding, bool) {
	var dims int
	var blob []byte
	if err := p.db.QueryRowContext(ctx, `SELECT dimensions, vector FROM embedding_cache WHERE key = ?`, key).Scan(&dims, &blob); err != nil {
		reason := "read_error"
		if err == sql.ErrNoRows {
			reason = "absent"
		}
		indexingperf.AddCount(ctx, "provider.cache.miss.reason."+reason, 1)
		return nil, false
	}
	if want > 0 && dims != want {
		indexingperf.AddCount(ctx, "provider.cache.miss.reason.dimensions", 1)
		return nil, false
	}
	if len(blob) != dims*4 {
		indexingperf.AddCount(ctx, "provider.cache.miss.reason.invalid_vector", 1)
		return nil, false
	}
	vec := make(Embedding, dims)
	for i := range vec {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(blob[i*4:]))
	}
	return vec, true
}

func (p *cachedProvider) store(ctx context.Context, missIdx []int, keys []string, fresh []Embedding) error {
	// ponytail: one mutex for all writes; per-key locking only if contention shows up.
	p.mu.Lock()
	defer p.mu.Unlock()
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO embedding_cache (key, dimensions, vector) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for n, i := range missIdx {
		vec := fresh[n]
		blob := make([]byte, len(vec)*4)
		for j, v := range vec {
			binary.LittleEndian.PutUint32(blob[j*4:], math.Float32bits(v))
		}
		if _, err := stmt.ExecContext(ctx, keys[i], len(vec), blob); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// cachedContextProvider forwards ContextTokens for inners that report it.
type cachedContextProvider struct {
	*cachedProvider
}

func (p *cachedContextProvider) ContextTokens(ctx context.Context) (int, error) {
	return p.inner.(ContextTokenProvider).ContextTokens(ctx)
}
