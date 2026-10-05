package embeddings

// Docs: [Embeddings - providers + configuration](docs/reference/guides/Embeddings - providers + configuration.md)

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/teamkeys"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
)

const (
	// DefaultVoyageEndpoint is the Voyage AI embeddings API endpoint.
	DefaultVoyageEndpoint = "https://api.voyageai.com/v1/embeddings"

	// DefaultVoyageModel is the recommended model for note embeddings.
	// voyage-4-lite offers a good balance of quality and speed.
	DefaultVoyageModel = "voyage-4-lite"

	// DefaultVoyageCodeModel is the recommended model for code embeddings.
	// We use voyage-4-lite here too since we embed documentation, symbols, and
	// signatures — not raw code — so a general-purpose model works well.
	DefaultVoyageCodeModel = "voyage-4-lite"

	// Voyage allows up to 1000 texts per request. We use a high default since
	// token-aware batching will pack requests efficiently.
	voyageHardMaxBatchSize = 1000
	voyageDefaultBatchSize = 1000
	// Voyage rate limits: 2000 RPM / 3M TPM for code models. With 32 concurrent
	// requests averaging ~1s each, we use ~1920 RPM worst case.
	voyageHardMaxConcurrency = 32
	voyageDefaultConcurrency = 32
	voyageHTTPTimeout        = 120 * time.Second
	voyageRecoveryBudget     = 2 * voyageHTTPTimeout
	voyageStatusBaseBackoff  = 1 * time.Second
	voyageStatusMaxBackoff   = 10 * time.Second

	// Token limits per request vary by model. Keep a little headroom because
	// local packing estimates tokens as chars/4 instead of using Voyage's tokenizer.
	voyageLiteMaxTokens     = 900000 // docs: 1M for voyage-4-lite / voyage-3.5-lite
	voyageStandardMaxTokens = 300000 // docs: 320K for voyage-4 / voyage-3.5 / voyage-2
	voyageLargeMaxTokens    = 110000 // docs: 120K for large/code/finance/law models
)

type voyageProvider struct {
	fingerprint        string
	model              string
	apiKey             string
	endpoint           string
	maxBatch           int
	maxTokens          int
	maxConc            int
	dims               int
	inputType          string // "document" for indexing, "query" for search
	mu                 sync.RWMutex
	httpClient         *http.Client
	sleep              func(ctx context.Context, d time.Duration) error
	now                func() time.Time
	jitter             func(time.Duration) time.Duration
	withRecoveryBudget func(context.Context, time.Duration) (context.Context, context.CancelFunc)
}

// NewVoyageProvider constructs a Voyage AI embeddings provider.
func NewVoyageProvider(cfg ProviderConfig) (Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("voyage provider requires api key")
	}
	model := cfg.Model
	if model == "" {
		model = DefaultVoyageModel
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = DefaultVoyageEndpoint
	}
	dims := cfg.Dimensions

	maxBatch := cfg.BatchSize
	if maxBatch <= 0 {
		maxBatch = voyageDefaultBatchSize
	}
	if maxBatch > voyageHardMaxBatchSize {
		maxBatch = voyageHardMaxBatchSize
	}

	maxConc := cfg.MaxConcurrency
	if maxConc <= 0 {
		maxConc = voyageDefaultConcurrency
	}
	if maxConc > voyageHardMaxConcurrency {
		maxConc = voyageHardMaxConcurrency
	}

	p := &voyageProvider{
		fingerprint:        providerFingerprint("voyage", model, endpoint, dims),
		model:              model,
		apiKey:             cfg.APIKey,
		endpoint:           endpoint,
		maxBatch:           maxBatch,
		maxTokens:          voyageMaxTokensForModel(model),
		maxConc:            maxConc,
		dims:               dims,
		inputType:          "document", // default to document for indexing
		httpClient:         withHTTPTimeout(nil, voyageHTTPTimeout),
		sleep:              sleepWithContext,
		now:                time.Now,
		jitter:             jitterVoyageBackoff,
		withRecoveryBudget: context.WithTimeout,
	}
	return p, nil
}

func (p *voyageProvider) Dimensions() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.dims
}

func (p *voyageProvider) DefaultBatchSize() int {
	return p.maxBatch
}

func (p *voyageProvider) DefaultMaxConcurrency() int {
	return p.maxConc
}

func (p *voyageProvider) DefaultMaxBatchBytes() int {
	return p.maxTokens * 4
}

func voyageMaxTokensForModel(model string) int {
	switch strings.TrimSpace(strings.ToLower(model)) {
	case "voyage-4-lite", "voyage-3.5-lite":
		return voyageLiteMaxTokens
	case "voyage-4", "voyage-3.5", "voyage-2":
		return voyageStandardMaxTokens
	default:
		return voyageLargeMaxTokens
	}
}

func (p *voyageProvider) EmbedTexts(ctx context.Context, texts []string) (result []Embedding, resultErr error) {
	indexingperf.ObserveProviderFingerprint(ctx, p.fingerprint)
	indexingperf.AddCount(ctx, "provider.logical.voyage", 1)
	defer func() { observeProviderOutcome(ctx, "voyage", resultErr) }()
	if len(texts) == 0 {
		return nil, fmt.Errorf("no texts to embed")
	}

	// Voyage API supports high concurrency. We use token-aware batching to pack
	// requests efficiently within the per-request token limit.
	executor := &BatchExecutor{
		BatchSize:         p.maxBatch,
		MaxTokensPerBatch: p.maxTokens,
		MaxConcurrency:    p.maxConc,
	}

	return executor.Execute(ctx, texts, p.embedBatchAdaptive)
}

func (p *voyageProvider) embedBatchAdaptive(ctx context.Context, texts []string) ([]Embedding, error) {
	batchCtx, cancel := p.withRecoveryBudget(ctx, voyageRecoveryBudget)
	defer cancel()

	vecs, err := p.embedBatchAdaptiveWithinBudget(batchCtx, texts)
	if err != nil && ctx.Err() == nil && batchCtx.Err() != nil {
		indexingperf.AddCount(ctx, "provider.request.voyage.recovery_budget_exhausted", 1)
		return nil, fmt.Errorf("voyage logical batch recovery budget exhausted after %s: %w", voyageRecoveryBudget, batchCtx.Err())
	}
	return vecs, err
}

func (p *voyageProvider) embedBatchAdaptiveWithinBudget(ctx context.Context, texts []string) ([]Embedding, error) {
	vecs, err := p.embedBatch(ctx, texts)
	if err == nil || len(texts) <= 1 || !shouldSplitVoyageBatchError(ctx, err) {
		return vecs, err
	}

	indexingperf.AddCount(ctx, "provider.request.voyage.split", 1)
	indexingperf.AddCount(ctx, "provider.request.voyage.split.reason."+providerErrorClass(err), 1)
	mid := len(texts) / 2
	left, err := p.embedBatchAdaptiveWithinBudget(ctx, texts[:mid])
	if err != nil {
		return nil, err
	}
	right, err := p.embedBatchAdaptiveWithinBudget(ctx, texts[mid:])
	if err != nil {
		return nil, err
	}
	out := make([]Embedding, 0, len(texts))
	out = append(out, left...)
	out = append(out, right...)
	return out, nil
}

func shouldSplitVoyageBatchError(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	var budgetErr *voyageRetryDelayBudgetError
	if errors.As(err, &budgetErr) {
		return false
	}
	return isTransientNetworkError(err)
}

type voyageRetryDelayBudgetError struct {
	delay time.Duration
}

func (e *voyageRetryDelayBudgetError) Error() string {
	return fmt.Sprintf("voyage retry delay %s exceeds remaining logical batch recovery budget", e.delay)
}

func (e *voyageRetryDelayBudgetError) Unwrap() error {
	return context.DeadlineExceeded
}

func (p *voyageProvider) embedBatch(ctx context.Context, texts []string) ([]Embedding, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// Build request payload
	payload := map[string]any{
		"model": p.model,
		"input": texts,
	}

	// Add input_type if set (helps Voyage optimize embeddings)
	if p.inputType != "" {
		payload["input_type"] = p.inputType
	}

	// Add dimensions if explicitly configured
	p.mu.RLock()
	payloadDims := p.dims
	p.mu.RUnlock()
	if payloadDims > 0 {
		payload["output_dimension"] = payloadDims
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	indexingperf.AddCount(ctx, "provider.request.voyage", 1)
	indexingperf.ObserveSample(ctx, "provider.request.voyage.inputs", int64(len(texts)))
	indexingperf.AddBytes(ctx, "provider.request.voyage.body", int64(len(body)))

	resp, err := p.doEmbeddingRequestWithRetry(ctx, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("voyage embeddings status %d: %s", resp.StatusCode, string(msg))
	}

	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		indexingperf.AddCount(ctx, "provider.request.voyage.protocol.decode_error", 1)
		return nil, err
	}
	if len(parsed.Data) != len(texts) {
		indexingperf.AddCount(ctx, "provider.request.voyage.protocol.vector_count_mismatch", 1)
		return nil, fmt.Errorf("embedding count mismatch: want %d got %d", len(texts), len(parsed.Data))
	}

	// Voyage returns embeddings in order, but we verify using the index field
	res := make([]Embedding, len(parsed.Data))
	for _, item := range parsed.Data {
		indexingperf.ObserveSample(ctx, "provider.response.voyage.dimensions", int64(len(item.Embedding)))
		if item.Index < 0 || item.Index >= len(texts) {
			indexingperf.AddCount(ctx, "provider.request.voyage.protocol.invalid_vector_index", 1)
			return nil, fmt.Errorf("invalid embedding index %d for batch size %d", item.Index, len(texts))
		}
		res[item.Index] = Embedding(item.Embedding)
		p.mu.Lock()
		if p.dims == 0 {
			p.dims = len(item.Embedding)
		}
		p.mu.Unlock()
	}
	return res, nil
}

func (p *voyageProvider) doEmbeddingRequestWithRetry(ctx context.Context, body []byte) (*http.Response, error) {
	const maxAttempts = 3

	var lastErr error
	httpAttempts := 0
	for {
		var retryAfter time.Duration
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+p.apiKey)

		resp, err := doProviderHTTP(p.httpClient, req, "voyage")
		if err == nil {
			indexingperf.AddCount(ctx, "provider.request.voyage.completed", 1)
		}
		if err != nil {
			lastErr = err
			// Adaptive recovery splits transient failures immediately. The shared
			// logical-batch budget bounds the resulting request tree.
			if isTransientNetworkError(err) {
				indexingperf.AddCount(ctx, "provider.request.voyage.network_failures", 1)
				return nil, fmt.Errorf("network error before adaptive recovery: %w", lastErr)
			}
			httpAttempts++
			// Non-transient errors: use normal retry count
			if httpAttempts >= maxAttempts {
				return nil, lastErr
			}
		} else if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			indexingperf.ObserveSample(ctx, "provider.request.voyage.status", int64(resp.StatusCode))
			httpAttempts++
			retryAfter = voyageRetryAfter(resp.Header.Get("Retry-After"), p.now())
			// Drain and close body before retrying
			msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			_ = resp.Body.Close()

			lastErr = fmt.Errorf("http status %d: %s", resp.StatusCode, string(msg))
			if httpAttempts >= maxAttempts {
				return nil, lastErr
			}
			indexingperf.AddCount(ctx, "provider.request.voyage.http_retry", 1)
		} else {
			indexingperf.ObserveSample(ctx, "provider.request.voyage.status", int64(resp.StatusCode))
			return resp, nil
		}

		delay := voyageStatusBaseBackoff * time.Duration(1<<uint(httpAttempts-1))
		if delay > voyageStatusMaxBackoff {
			delay = voyageStatusMaxBackoff
		}
		delay = p.jitter(delay)
		if retryAfter > delay {
			delay = retryAfter
		}
		if deadline, ok := ctx.Deadline(); ok && delay > deadline.Sub(p.now()) {
			return nil, &voyageRetryDelayBudgetError{delay: delay}
		}
		indexingperf.AddCount(ctx, "provider.request.voyage.backoff.reason.http", 1)
		indexingperf.ObserveLatency(ctx, "provider.request.voyage.backoff_scheduled", delay)
		if err := p.sleepWithMetrics(ctx, delay); err != nil {
			return nil, err
		}
	}
}

func (p *voyageProvider) sleepWithMetrics(ctx context.Context, delay time.Duration) error {
	started := p.now()
	err := p.sleep(ctx, delay)
	indexingperf.ObserveLatency(ctx, "provider.request.voyage.backoff", p.now().Sub(started))
	return err
}

func jitterVoyageBackoff(delay time.Duration) time.Duration {
	return time.Duration(float64(delay) * (0.8 + rand.Float64()*0.4))
}

func voyageRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	var delay time.Duration
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		if seconds > int64((time.Duration(1<<63-1))/time.Second) {
			return time.Duration(1<<63 - 1)
		}
		delay = time.Duration(seconds) * time.Second
	} else if retryAt, err := http.ParseTime(value); err == nil && retryAt.After(now) {
		delay = retryAt.Sub(now)
	}
	return delay
}

// ResolveVoyageAPIKey returns the Voyage AI API key from env vars or team keys.
func ResolveVoyageAPIKey(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if val := vaultconfig.ResolveValue("RHIZOME_VOYAGE_API_KEY", "VOYAGE_API_KEY"); val != "" {
		return val
	}
	// Fall back to team key if available
	return teamkeys.VoyageKey()
}
