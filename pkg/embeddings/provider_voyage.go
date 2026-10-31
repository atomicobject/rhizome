package embeddings

// Docs: [Embeddings - providers + configuration](docs/reference/guides/Embeddings - providers + configuration.md)

import (
	"bytes"
	"context"
	crand "crypto/rand"
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
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
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
	// managed sends requests to the team gateway with an idempotency key and
	// no provider Authorization header; see ProviderConfig.Managed.
	managed bool
}

// NewVoyageProvider constructs a Voyage AI embeddings provider. It requires
// either a direct API key or explicit managed (team gateway) access, not both.
func NewVoyageProvider(cfg ProviderConfig) (Provider, error) {
	if cfg.Managed != nil {
		if cfg.APIKey != "" {
			return nil, fmt.Errorf("voyage provider: an API key and team session access are mutually exclusive")
		}
		if strings.TrimSpace(cfg.Managed.Origin) == "" || cfg.Managed.Transport == nil {
			return nil, fmt.Errorf("voyage provider: team session access requires an origin and transport")
		}
		if cfg.Endpoint != "" && strings.TrimRight(cfg.Endpoint, "/") != DefaultVoyageEndpoint {
			return nil, fmt.Errorf("voyage provider: team_session_endpoint_conflict: embeddings.endpoint %q cannot be used with a team session; remove it or set VOYAGE_API_KEY", cfg.Endpoint)
		}
	} else if cfg.APIKey == "" {
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
	if m := cfg.Managed; m != nil {
		// The fingerprint keeps the logical endpoint; only the request URL moves.
		p.managed = true
		p.endpoint = strings.TrimRight(m.Origin, "/") + wire.RouteVoyageEmbed
		p.httpClient = &http.Client{
			Transport:     m.Transport,
			Timeout:       voyageHTTPTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
		if m.MaxConcurrency > 0 && m.MaxConcurrency < p.maxConc {
			p.maxConc = m.MaxConcurrency
		}
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
	indexingperf.AddCount(ctx, indexingperf.ProviderLogicalVoyage, 1)
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
		indexingperf.AddCount(ctx, indexingperf.ProviderRequestVoyageRecoveryBudgetExhausted, 1)
		return nil, fmt.Errorf("voyage logical batch recovery budget exhausted after %s: %w", voyageRecoveryBudget, batchCtx.Err())
	}
	return vecs, err
}

func (p *voyageProvider) embedBatchAdaptiveWithinBudget(ctx context.Context, texts []string) ([]Embedding, error) {
	vecs, err := p.embedBatch(ctx, texts)
	if err == nil || len(texts) <= 1 || !p.shouldSplit(ctx, err) {
		return vecs, err
	}

	indexingperf.AddCount(ctx, indexingperf.ProviderRequestVoyageSplit, 1)
	indexingperf.AddCount(ctx, indexingperf.VoyageSplitReason.Select(providerErrorClass(err)), 1)
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

// shouldSplit decides adaptive recovery. In managed mode only the gateway's
// definitive pre-admission size rejection splits: a split child is a new
// logical request with its own idempotency key, so splitting after an
// ambiguous or terminal failure could pay for the same inputs twice.
func (p *voyageProvider) shouldSplit(ctx context.Context, err error) bool {
	if !p.managed {
		return shouldSplitVoyageBatchError(ctx, err)
	}
	var gatewayErr *wire.GatewayError
	return ctx.Err() == nil && errors.As(err, &gatewayErr) && gatewayErr.Code == wire.CodeTooLarge
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
	indexingperf.AddCount(ctx, indexingperf.ProviderRequestVoyage, 1)
	indexingperf.ObserveSample(ctx, indexingperf.ProviderRequestVoyageInputs, int64(len(texts)))
	indexingperf.AddBytes(ctx, indexingperf.ProviderRequestVoyageBodyBytes, int64(len(body)))

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
		indexingperf.AddCount(ctx, indexingperf.ProviderRequestVoyageProtocolDecodeError, 1)
		return nil, err
	}
	if len(parsed.Data) != len(texts) {
		indexingperf.AddCount(ctx, indexingperf.ProviderRequestVoyageProtocolVectorCountMismatch, 1)
		return nil, fmt.Errorf("embedding count mismatch: want %d got %d", len(texts), len(parsed.Data))
	}

	// Voyage returns embeddings in order, but we verify using the index field
	res := make([]Embedding, len(parsed.Data))
	for _, item := range parsed.Data {
		indexingperf.ObserveSample(ctx, indexingperf.ProviderResponseVoyageDimensions, int64(len(item.Embedding)))
		if item.Index < 0 || item.Index >= len(texts) {
			indexingperf.AddCount(ctx, indexingperf.ProviderRequestVoyageProtocolInvalidVectorIndex, 1)
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

// doEmbeddingRequestWithRetry sends one physical batch.
//
// In managed mode the gateway dispatches each idempotency key at most once and
// settles it once. The key is allocated before the retry loop and reused only
// when the previous attempt's outcome is ambiguous (a transport failure after
// sending, request_in_progress, or an upstream 5xx passed through), so the
// gateway answers from its receipt instead of paying again. After a definitive
// answer (an upstream 429 passed through, or a retryable gateway rejection
// such as rate_limited) the next attempt gets a fresh key. Split children are
// new physical batches and allocate their own keys.
func (p *voyageProvider) doEmbeddingRequestWithRetry(ctx context.Context, body []byte) (*http.Response, error) {
	const maxAttempts = 3

	var lastErr error
	var idempotencyKey string
	if p.managed {
		idempotencyKey = crand.Text()
	}
	httpAttempts := 0
	for {
		var retryAfter time.Duration
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if p.managed {
			req.Header.Set(wire.HeaderIdempotencyKey, idempotencyKey)
		} else {
			req.Header.Set("Authorization", "Bearer "+p.apiKey)
		}

		resp, err := doProviderHTTP(p.httpClient, req, "voyage")
		if err == nil {
			indexingperf.AddCount(ctx, indexingperf.ProviderRequestVoyageCompleted, 1)
		}
		if err != nil && p.managed {
			lastErr = err
			var gatewayErr *wire.GatewayError
			switch {
			case errors.As(err, &gatewayErr):
				// Terminal codes, including upstream_outcome_unknown and
				// result_unavailable, end this logical request.
				if !gatewayErr.Retryable() {
					return nil, err
				}
				retryAfter = gatewayErr.RetryAfter
				if gatewayErr.Code != wire.CodeInProgress {
					idempotencyKey = crand.Text()
				}
			case isTransientNetworkError(err):
				// Possibly dispatched: keep the key.
				indexingperf.AddCount(ctx, indexingperf.ProviderRequestVoyageNetworkFailures, 1)
			default:
				return nil, err
			}
			httpAttempts++
			if httpAttempts >= maxAttempts {
				return nil, lastErr
			}
			indexingperf.AddCount(ctx, indexingperf.ProviderRequestVoyageHttpRetry, 1)
		} else if err != nil {
			lastErr = err
			// Adaptive recovery splits transient failures immediately. The shared
			// logical-batch budget bounds the resulting request tree.
			if isTransientNetworkError(err) {
				indexingperf.AddCount(ctx, indexingperf.ProviderRequestVoyageNetworkFailures, 1)
				return nil, fmt.Errorf("network error before adaptive recovery: %w", lastErr)
			}
			httpAttempts++
			// Non-transient errors: use normal retry count
			if httpAttempts >= maxAttempts {
				return nil, lastErr
			}
		} else if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			indexingperf.ObserveSample(ctx, indexingperf.ProviderRequestVoyageStatus, int64(resp.StatusCode))
			httpAttempts++
			retryAfter = voyageRetryAfter(resp.Header.Get("Retry-After"), p.now())
			// Drain and close body before retrying
			msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			_ = resp.Body.Close()

			lastErr = fmt.Errorf("http status %d: %s", resp.StatusCode, string(msg))
			if httpAttempts >= maxAttempts {
				return nil, lastErr
			}
			if p.managed && resp.StatusCode == http.StatusTooManyRequests {
				// Upstream rejected the call definitively; the gateway settled this key.
				idempotencyKey = crand.Text()
			}
			indexingperf.AddCount(ctx, indexingperf.ProviderRequestVoyageHttpRetry, 1)
		} else {
			indexingperf.ObserveSample(ctx, indexingperf.ProviderRequestVoyageStatus, int64(resp.StatusCode))
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
		indexingperf.AddCount(ctx, indexingperf.ProviderRequestVoyageBackoffReasonHttp, 1)
		indexingperf.ObserveLatency(ctx, indexingperf.ProviderRequestVoyageBackoffScheduled, delay)
		if err := p.sleepWithMetrics(ctx, delay); err != nil {
			return nil, err
		}
	}
}

func (p *voyageProvider) sleepWithMetrics(ctx context.Context, delay time.Duration) error {
	started := p.now()
	err := p.sleep(ctx, delay)
	indexingperf.ObserveLatency(ctx, indexingperf.ProviderRequestVoyageBackoff, p.now().Sub(started))
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
