package embeddings

// Docs: [Embeddings - providers + configuration](docs/reference/guides/Embeddings - providers + configuration.md)

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

const (
	// Provider-local safety caps. These are intentionally generous, but prevent a
	// single run from spawning an extreme amount of concurrency/batch load even on
	// accounts with very high rate limits.
	openAIHardMaxConcurrency = 512
	openAIHardMaxBatchSize   = 2048 // OpenAI allows up to 2048 inputs per request

	// Defaults when the user did not set explicit batch/concurrency settings.
	openAIDefaultMaxConcurrency = 250
	openAIDefaultBatchSize      = 2048

	// Token limit per request: 300K tokens (with 8192 output dimensions).
	// We use a conservative 270K to leave headroom for estimation errors.
	openAIDefaultMaxTokens = 270000
)

type openAIProvider struct {
	fingerprint string
	model       string
	apiKey      string
	endpoint    string
	maxBatch    int
	maxTokens   int
	maxConc     int
	mu          sync.RWMutex
	dims        int
	httpClient  *http.Client

	limiter *openAIRateLimiter
	sleep   func(ctx context.Context, d time.Duration) error
}

// NewOpenAIProvider constructs an OpenAI embeddings provider.
func NewOpenAIProvider(cfg ProviderConfig) (Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("openai provider requires api key")
	}
	model := cfg.Model
	if model == "" {
		model = DefaultOpenAIModel
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = DefaultOpenAIEndpoint
	}
	dims := cfg.Dimensions

	maxBatch := cfg.BatchSize
	if maxBatch <= 0 {
		maxBatch = openAIDefaultBatchSize
	}
	if maxBatch > openAIHardMaxBatchSize {
		maxBatch = openAIHardMaxBatchSize
	}

	maxConc := cfg.MaxConcurrency
	if maxConc <= 0 {
		maxConc = openAIDefaultMaxConcurrency
	}
	if maxConc > openAIHardMaxConcurrency {
		maxConc = openAIHardMaxConcurrency
	}

	p := &openAIProvider{
		fingerprint: providerFingerprint("openai", model, endpoint, dims),
		model:       model,
		apiKey:      cfg.APIKey,
		endpoint:    endpoint,
		maxBatch:    maxBatch,
		maxTokens:   openAIDefaultMaxTokens,
		maxConc:     maxConc,
		dims:        dims,
		httpClient:  withDefaultTimeout(nil),
		limiter:     newOpenAIRateLimiter(),
		sleep:       sleepWithContext,
	}
	return p, nil
}

func (p *openAIProvider) Dimensions() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.dims
}

func (p *openAIProvider) DefaultBatchSize() int {
	return p.maxBatch
}

func (p *openAIProvider) DefaultMaxConcurrency() int {
	return p.maxConc
}

func (p *openAIProvider) EmbedTexts(ctx context.Context, texts []string) (result []Embedding, resultErr error) {
	indexingperf.ObserveProviderFingerprint(ctx, p.fingerprint)
	indexingperf.AddCount(ctx, "provider.logical.openai", 1)
	defer func() { observeProviderOutcome(ctx, "openai", resultErr) }()
	if len(texts) == 0 {
		return nil, fmt.Errorf("no texts to embed")
	}

	// OpenAI allows up to 300K tokens per request. We use token-aware batching
	// to pack requests efficiently within that limit.
	executor := &BatchExecutor{
		BatchSize:         p.maxBatch,
		MaxTokensPerBatch: p.maxTokens,
		MaxConcurrency:    p.maxConc,
	}
	if p.maxConc == 1 {
		snap := p.limiter.snapshot()
		if snap.fromHeaders {
			executor.NextBatchSizeFn = p.nextBatchSizeFromLimiter
		}
	}

	return executor.Execute(ctx, texts, p.embedBatch)
}

func (p *openAIProvider) nextBatchSizeFromLimiter(remaining []string) int {
	if len(remaining) == 0 {
		return 0
	}
	maxItems := p.maxBatch
	if maxItems <= 0 {
		maxItems = openAIDefaultBatchSize
	}

	snap := p.limiter.snapshot()
	if !snap.hasTokens || !snap.hasRequests || snap.remainingRequests <= 0 || snap.remainingTokens <= 0 {
		if maxItems > len(remaining) {
			return len(remaining)
		}
		return maxItems
	}

	perRequestTokens := int(float64(snap.remainingTokens) / float64(snap.remainingRequests) * 0.9)
	if perRequestTokens <= 0 {
		return 1
	}
	if p.maxTokens > 0 && perRequestTokens > p.maxTokens {
		perRequestTokens = p.maxTokens
	}

	tokens := embeddingRequestOverheadTokens
	count := 0
	for _, text := range remaining {
		if count >= maxItems {
			break
		}
		textTokens := estimateTextTokens(text)
		if count > 0 && tokens+textTokens > perRequestTokens {
			break
		}
		if count == 0 && tokens+textTokens > perRequestTokens {
			count = 1
			break
		}
		tokens += textTokens
		count++
	}
	if count == 0 {
		count = 1
	}
	if count > len(remaining) {
		return len(remaining)
	}
	return count
}

func (p *openAIProvider) embedBatch(ctx context.Context, texts []string) ([]Embedding, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	estimatedTokens := estimateEmbeddingTokens(texts)

	// Pace based on API-provided rate-limit budgets.
	limiterStarted := time.Now()
	limiterErr := p.limiter.wait(ctx, estimatedTokens)
	indexingperf.ObserveLatency(ctx, "provider.request.openai.rate_limit_wait", time.Since(limiterStarted))
	if err := limiterErr; err != nil {
		return nil, err
	}

	// Build request payload once; body gets re-read per retry attempt.
	payload := map[string]any{
		"model": p.model,
		"input": texts,
	}
	p.mu.RLock()
	payloadDims := p.dims
	p.mu.RUnlock()
	if payloadDims > 0 {
		payload["dimensions"] = payloadDims
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	indexingperf.AddCount(ctx, "provider.request.openai", 1)
	indexingperf.ObserveSample(ctx, "provider.request.openai.inputs", int64(len(texts)))
	indexingperf.AddBytes(ctx, "provider.request.openai.body", int64(len(body)))

	resp, err := p.doEmbeddingRequestWithRetry(ctx, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Always ingest headers (even on error responses).
	p.limiter.updateFromHeaders(resp.Header)

	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("openai embeddings status %d: %s", resp.StatusCode, string(msg))
	}

	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		indexingperf.AddCount(ctx, "provider.request.openai.protocol.decode_error", 1)
		return nil, err
	}
	if len(parsed.Data) != len(texts) {
		indexingperf.AddCount(ctx, "provider.request.openai.protocol.vector_count_mismatch", 1)
		return nil, fmt.Errorf("embedding count mismatch: want %d got %d", len(texts), len(parsed.Data))
	}

	res := make([]Embedding, len(parsed.Data))
	for i, item := range parsed.Data {
		indexingperf.ObserveSample(ctx, "provider.response.openai.dimensions", int64(len(item.Embedding)))
		res[i] = Embedding(item.Embedding)
		p.mu.Lock()
		if p.dims == 0 {
			p.dims = len(item.Embedding)
		}
		p.mu.Unlock()
	}
	return res, nil
}

func (p *openAIProvider) doEmbeddingRequestWithRetry(ctx context.Context, body []byte) (*http.Response, error) {
	const (
		maxAttempts        = 3
		maxNetworkAttempts = 6 // More attempts for transient network errors
		baseBackoff        = 200 * time.Millisecond
		maxBackoff         = 5 * time.Second
		networkBaseBackoff = 1 * time.Second  // Longer initial backoff for network errors
		networkMaxBackoff  = 30 * time.Second // Longer max backoff for network errors
	)

	var lastErr error
	var networkRetries int
	httpAttempts := 0
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+p.apiKey)

		resp, err := doProviderHTTP(p.httpClient, req, "openai")
		if err != nil {
			lastErr = err
			// For transient network errors, use longer backoff and more retries
			if isTransientNetworkError(err) {
				networkRetries++
				indexingperf.AddCount(ctx, "provider.request.openai.network_retry", 1)
				if networkRetries >= maxNetworkAttempts {
					return nil, fmt.Errorf("network error after %d retries: %w", networkRetries, lastErr)
				}
				delay := networkBaseBackoff * time.Duration(1<<uint(networkRetries-1))
				if delay > networkMaxBackoff {
					delay = networkMaxBackoff
				}
				if err := providerBackoff(ctx, "provider.request.openai", "network", delay, p.sleep); err != nil {
					return nil, err
				}
				continue
			}
			httpAttempts++
			// Non-transient errors: use normal retry count
			if httpAttempts >= maxAttempts {
				return nil, lastErr
			}
		} else if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden || resp.StatusCode >= 500 {
			indexingperf.ObserveSample(ctx, "provider.request.openai.status", int64(resp.StatusCode))
			httpAttempts++
			// Update limiter on throttling so subsequent requests get paced.
			p.limiter.updateFromHeaders(resp.Header)

			// Drain and close body before retrying.
			msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			_ = resp.Body.Close()

			lastErr = fmt.Errorf("http status %d: %s", resp.StatusCode, string(msg))
			if resp.StatusCode == http.StatusForbidden && httpAttempts >= maxAttempts {
				resp.Body = io.NopCloser(bytes.NewReader(msg))
				return resp, nil
			}
			if httpAttempts >= maxAttempts {
				return nil, lastErr
			}
			indexingperf.AddCount(ctx, "provider.request.openai.http_retry", 1)
		} else {
			indexingperf.ObserveSample(ctx, "provider.request.openai.status", int64(resp.StatusCode))
			return resp, nil
		}

		// Prefer header-driven resets when we have no remaining request budget,
		// otherwise fall back to a small fixed backoff.
		delay := baseBackoff * time.Duration(1<<uint(httpAttempts-1))
		if delay > maxBackoff {
			delay = maxBackoff
		}
		if d := p.retryDelayFromSnapshot(); d > delay {
			delay = d
		}
		if err := providerBackoff(ctx, "provider.request.openai", "http", delay, p.sleep); err != nil {
			return nil, err
		}
	}
}

// isTransientNetworkError returns true for network errors that are likely transient
// and worth retrying with longer backoff (connection reset, EOF, timeout, etc.).
func isTransientNetworkError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Connection reset by peer, broken pipe, EOF, and similar transient errors
	transientPatterns := []string{
		"connection reset",
		"broken pipe",
		"EOF",
		"connection refused",
		"no such host",
		"network is unreachable",
		"i/o timeout",
		"TLS handshake timeout",
		"context deadline exceeded",
	}
	for _, pattern := range transientPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}
	return false
}

func (p *openAIProvider) retryDelayFromSnapshot() time.Duration {
	p.limiter.mu.Lock()
	defer p.limiter.mu.Unlock()
	now := p.limiter.now()

	// If we know we’re exhausted on either budget, wait until that budget resets.
	if p.limiter.snap.hasRequests && p.limiter.snap.remainingRequests <= 0 && p.limiter.snap.resetRequests > 0 {
		resetAt := p.limiter.snap.updatedAt.Add(p.limiter.snap.resetRequests)
		if now.Before(resetAt) {
			return resetAt.Sub(now)
		}
	}
	if p.limiter.snap.hasTokens && p.limiter.snap.remainingTokens <= 0 && p.limiter.snap.resetTokens > 0 {
		resetAt := p.limiter.snap.updatedAt.Add(p.limiter.snap.resetTokens)
		if now.Before(resetAt) {
			return resetAt.Sub(now)
		}
	}
	return 0
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func estimateEmbeddingTokens(texts []string) int {
	total := embeddingRequestOverheadTokens
	for _, t := range texts {
		total += estimateTextTokens(t)
	}
	return total
}

const embeddingRequestOverheadTokens = 16

func estimateTextTokens(t string) int {
	// Very rough heuristic: ~4 bytes/token for typical English text; err on the
	// conservative side to reduce 429s when token limits are tight.
	if t == "" {
		return 1
	}
	est := len(t) / 4
	if est < 1 {
		est = 1
	}
	return est
}
