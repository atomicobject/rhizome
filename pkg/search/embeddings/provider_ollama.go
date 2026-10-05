package embeddings

// Docs: [Embeddings - providers + configuration](docs/reference/guides/Embeddings - providers + configuration.md)

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

const (
	// Ollama can handle higher concurrency than CPU count since GPU inference
	// is the bottleneck, not CPU. These defaults balance throughput vs overload.
	ollamaDefaultMaxConcurrency = 32
	ollamaHardMaxConcurrency    = 128
	ollamaDefaultBatchSize      = 32
	ollamaHardMaxBatchSize      = 128
)

type ollamaProvider struct {
	fingerprint string
	model       string
	endpoint    string
	maxConc     int
	maxBatch    int
	mu          sync.RWMutex
	dims        int
	ctxTokens   int
	ctxChecked  bool
	ctxErr      error
	httpClient  *http.Client
}

// NewOllamaProvider constructs an Ollama embeddings provider.
func NewOllamaProvider(cfg ProviderConfig) (Provider, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("ollama provider requires model")
	}
	endpoint := NormalizeOllamaEndpoint(cfg.Endpoint)

	maxConc := cfg.MaxConcurrency
	if maxConc <= 0 {
		// Default to higher concurrency than GOMAXPROCS since GPU is the bottleneck
		maxConc = ollamaDefaultMaxConcurrency
		if cpus := runtime.GOMAXPROCS(0); cpus > maxConc {
			maxConc = cpus
		}
	}
	if maxConc < 1 {
		maxConc = 1
	}
	if maxConc > ollamaHardMaxConcurrency {
		maxConc = ollamaHardMaxConcurrency
	}

	maxBatch := cfg.BatchSize
	if maxBatch <= 0 {
		maxBatch = ollamaDefaultBatchSize
	}
	if maxBatch > ollamaHardMaxBatchSize {
		maxBatch = ollamaHardMaxBatchSize
	}

	return &ollamaProvider{
		fingerprint: providerFingerprint("ollama", cfg.Model, endpoint, cfg.Dimensions),
		model:       cfg.Model,
		endpoint:    endpoint,
		maxConc:     maxConc,
		maxBatch:    maxBatch,
		dims:        cfg.Dimensions,
		httpClient:  withDefaultTimeout(nil),
	}, nil
}

func (p *ollamaProvider) Dimensions() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.dims
}

func (p *ollamaProvider) ContextTokens(ctx context.Context) (int, error) {
	p.mu.RLock()
	if p.ctxChecked {
		tokens := p.ctxTokens
		err := p.ctxErr
		p.mu.RUnlock()
		return tokens, err
	}
	p.mu.RUnlock()

	tokens, err := OllamaContextTokens(ctx, p.httpClient, p.endpoint, p.model)
	if tokens == 0 {
		if fallback := DefaultOllamaContextTokens(p.model); fallback > 0 {
			tokens = fallback
		}
	}

	p.mu.Lock()
	if !p.ctxChecked {
		p.ctxTokens = tokens
		p.ctxChecked = true
		p.ctxErr = err
	}
	tokens = p.ctxTokens
	err = p.ctxErr
	p.mu.Unlock()

	return tokens, err
}

func (p *ollamaProvider) DefaultBatchSize() int {
	return p.maxBatch
}

func (p *ollamaProvider) DefaultMaxConcurrency() int {
	return p.maxConc
}

func (p *ollamaProvider) EmbedTexts(ctx context.Context, texts []string) (result []Embedding, resultErr error) {
	indexingperf.ObserveProviderFingerprint(ctx, p.fingerprint)
	indexingperf.AddCount(ctx, "provider.logical.ollama", 1)
	defer func() { observeProviderOutcome(ctx, "ollama", resultErr) }()
	if len(texts) == 0 {
		return nil, fmt.Errorf("no texts to embed")
	}

	executor := &BatchExecutor{
		BatchSize:      p.maxBatch,
		MaxConcurrency: p.maxConc,
	}

	return executor.Execute(ctx, texts, p.embedBatch)
}

// embedBatch sends a batch embedding request to Ollama's /api/embed endpoint.
func (p *ollamaProvider) embedBatch(ctx context.Context, texts []string) ([]Embedding, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	var results []Embedding

	if isLegacyOllamaEmbeddingsEndpoint(p.endpoint) {
		out := make([]Embedding, len(texts))
		for i, text := range texts {
			vec, err := p.embedLegacyPrompt(ctx, text)
			if err != nil {
				return nil, err
			}
			out[i] = vec
		}
		results = out
	} else {
		payload := map[string]any{
			"model": p.model,
			"input": texts,
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := doWithRetryProvider(p.httpClient, req, "ollama")
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 300 {
			msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			return nil, fmt.Errorf("ollama embeddings status %d: %s", resp.StatusCode, string(msg))
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}

		var parsed struct {
			// New /api/embed endpoint returns array of embeddings
			Embeddings [][]float32 `json:"embeddings"`
			// Old /api/embeddings endpoint returns single embedding (fallback)
			Embedding []float32 `json:"embedding"`
			// OpenAI-compatible /v1/embeddings returns data array
			Data []struct {
				Embedding []float64 `json:"embedding"`
			} `json:"data"`
			// Error message (some Ollama responses return 200 + error)
			Error string `json:"error"`
		}
		if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
			return nil, err
		}
		if parsed.Error != "" {
			return nil, fmt.Errorf("ollama embeddings error: %s", parsed.Error)
		}

		// Handle both batch response (embeddings) and single response (embedding)
		if len(parsed.Embeddings) > 0 {
			// Batch response from /api/embed
			if len(parsed.Embeddings) != len(texts) {
				return nil, fmt.Errorf("embedding count mismatch: want %d got %d", len(texts), len(parsed.Embeddings))
			}
			results = make([]Embedding, len(parsed.Embeddings))
			for i, emb := range parsed.Embeddings {
				results[i] = Embedding(emb)
			}
		} else if len(parsed.Embedding) > 0 {
			// Single response from /api/embeddings (legacy fallback)
			if len(texts) != 1 {
				return nil, fmt.Errorf("legacy endpoint returned single embedding for %d texts", len(texts))
			}
			results = []Embedding{Embedding(parsed.Embedding)}
		} else if len(parsed.Data) > 0 {
			if len(parsed.Data) != len(texts) {
				return nil, fmt.Errorf("embedding count mismatch: want %d got %d", len(texts), len(parsed.Data))
			}
			results = make([]Embedding, len(parsed.Data))
			for i, item := range parsed.Data {
				results[i] = float64ToEmbedding(item.Embedding)
			}
		} else {
			snippet := string(bodyBytes)
			if len(snippet) > 1024 {
				snippet = snippet[:1024] + "..."
			}
			return nil, fmt.Errorf("no embeddings in response (model=%s endpoint=%s): %s", p.model, p.endpoint, snippet)
		}
	}

	// Update dimensions on first successful response
	if len(results) > 0 && len(results[0]) > 0 {
		p.mu.Lock()
		if p.dims == 0 {
			p.dims = len(results[0])
		}
		p.mu.Unlock()
	}

	return results, nil
}

func float64ToEmbedding(values []float64) Embedding {
	if len(values) == 0 {
		return nil
	}
	out := make(Embedding, len(values))
	for i, v := range values {
		out[i] = float32(v)
	}
	return out
}

func (p *ollamaProvider) embedLegacyPrompt(ctx context.Context, text string) (Embedding, error) {
	payload := map[string]any{
		"model":  p.model,
		"prompt": text,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := doWithRetryProvider(p.httpClient, req, "ollama")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("ollama embeddings status %d: %s", resp.StatusCode, string(msg))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Embedding []float32 `json:"embedding"`
		Error     string    `json:"error"`
	}
	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		return nil, err
	}
	if parsed.Error != "" {
		return nil, fmt.Errorf("ollama embeddings error: %s", parsed.Error)
	}
	if len(parsed.Embedding) == 0 {
		snippet := string(bodyBytes)
		if len(snippet) > 1024 {
			snippet = snippet[:1024] + "..."
		}
		return nil, fmt.Errorf("no embeddings in response (model=%s endpoint=%s): %s", p.model, p.endpoint, snippet)
	}
	return Embedding(parsed.Embedding), nil
}

func isLegacyOllamaEmbeddingsEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil {
		return strings.HasSuffix(endpoint, "/api/embeddings")
	}
	return strings.HasSuffix(u.Path, "/api/embeddings")
}
