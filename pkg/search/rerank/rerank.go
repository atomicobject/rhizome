// Package rerank provides an optional cross-encoder reranking stage for search.
//
// A Reranker scores already-retrieved documents against the query text; the
// ranking decorator in pkg/search/relevance blends those scores into the base
// ranking. The stage is opt-in through RHIZOME_RERANK_PROVIDER and degrades to
// the base order whenever it is unavailable.
package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

// Document is one candidate text handed to a reranker. ID identifies the result
// the score belongs to; it is never sent to the provider.
type Document struct {
	ID   string
	Text string
}

// Reranker scores documents against a query and returns one relevance score per
// document, in input order, normalized to 0..1.
type Reranker interface {
	Rerank(ctx context.Context, query string, docs []Document) ([]float64, error)
}

// ProviderConfig identifies the reranker that ran, for evidence and cache keys.
type ProviderConfig struct {
	Provider string
	Model    string
}

const (
	// DefaultVoyageEndpoint is the Voyage AI rerank API endpoint.
	DefaultVoyageEndpoint = "https://api.voyageai.com/v1/rerank"
	// DefaultVoyageModel is the small, fast Voyage cross-encoder.
	DefaultVoyageModel = "rerank-2.5-lite"
	// defaultTimeout keeps the stage inside interactive search latency.
	defaultTimeout = 2 * time.Second
)

// VoyageReranker calls the Voyage AI rerank API.
type VoyageReranker struct {
	APIKey   string
	Model    string
	Endpoint string
	Timeout  time.Duration
	Client   *http.Client
}

type voyageRequest struct {
	Query      string   `json:"query"`
	Documents  []string `json:"documents"`
	Model      string   `json:"model"`
	TopK       int      `json:"top_k"`
	Truncation bool     `json:"truncation"`
}

type voyageResponse struct {
	Data []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"data"`
	Model string `json:"model"`
}

func (r *VoyageReranker) Rerank(ctx context.Context, query string, docs []Document) ([]float64, error) {
	if len(docs) == 0 {
		return nil, nil
	}
	texts := make([]string, len(docs))
	for i, doc := range docs {
		texts[i] = doc.Text
	}
	payload, err := json.Marshal(voyageRequest{
		Query:      query,
		Documents:  texts,
		Model:      r.model(),
		TopK:       len(docs),
		Truncation: true,
	})
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	endpoint := strings.TrimSpace(r.Endpoint)
	if endpoint == "" {
		endpoint = DefaultVoyageEndpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.APIKey)
	req.Header.Set("Content-Type", "application/json")

	client := r.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		snippet := string(body)
		if len(snippet) > 512 {
			snippet = snippet[:512] + "..."
		}
		return nil, fmt.Errorf("voyage rerank %s (model=%s): %s", resp.Status, r.model(), snippet)
	}
	var decoded voyageResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("voyage rerank decode (model=%s): %w", r.model(), err)
	}
	if len(decoded.Data) != len(docs) {
		return nil, fmt.Errorf("voyage rerank returned %d scores for %d documents", len(decoded.Data), len(docs))
	}
	scores := make([]float64, len(docs))
	for _, item := range decoded.Data {
		if item.Index < 0 || item.Index >= len(docs) {
			return nil, fmt.Errorf("voyage rerank returned out-of-range index %d", item.Index)
		}
		scores[item.Index] = clamp01(item.RelevanceScore)
	}
	return scores, nil
}

func (r *VoyageReranker) model() string {
	if model := strings.TrimSpace(r.Model); model != "" {
		return model
	}
	return DefaultVoyageModel
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

var (
	cachedMu      sync.Mutex
	cachedByKey   = map[string]Reranker{}
	cacheDisabled = map[string]struct{}{}
)

// NewFromEnv builds the configured reranker, reporting false when reranking is
// disabled (empty or unsupported RHIZOME_RERANK_PROVIDER) or the API key is
// missing. When RHIZOME_EMBEDDING_CACHE names a file, scores are cached there
// so repeated evaluation runs are deterministic and cheaper; the cache handle is
// memoized so per-request construction does not leak database connections.
func NewFromEnv() (Reranker, ProviderConfig, bool) {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("RHIZOME_RERANK_PROVIDER")))
	if provider != "voyage" {
		return nil, ProviderConfig{}, false
	}
	apiKey := embeddings.ResolveAPIKeyForProvider("voyage")
	if apiKey == "" {
		return nil, ProviderConfig{}, false
	}
	cfg := ProviderConfig{Provider: provider, Model: strings.TrimSpace(os.Getenv("RHIZOME_RERANK_MODEL"))}
	base := &VoyageReranker{APIKey: apiKey, Model: cfg.Model}
	cfg.Model = base.model()

	path := strings.TrimSpace(os.Getenv("RHIZOME_EMBEDDING_CACHE"))
	if path == "" {
		return base, cfg, true
	}
	if cached, ok := memoizedCache(base, cfg, path); ok {
		return cached, cfg, true
	}
	return base, cfg, true
}

func memoizedCache(inner Reranker, cfg ProviderConfig, path string) (Reranker, bool) {
	key := strings.Join([]string{cfg.Provider, cfg.Model, path}, "\x00")
	cachedMu.Lock()
	defer cachedMu.Unlock()
	if existing, ok := cachedByKey[key]; ok {
		return existing, true
	}
	if _, failed := cacheDisabled[key]; failed {
		return nil, false
	}
	cached, err := NewCachedReranker(inner, cfg, path)
	if err != nil {
		// ponytail: one failed open disables the cache for this process; reranking
		// still runs uncached. Retry-on-open only if cache files start appearing late.
		cacheDisabled[key] = struct{}{}
		return nil, false
	}
	cachedByKey[key] = cached
	return cached, true
}
