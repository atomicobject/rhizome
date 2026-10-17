package compress

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/llm"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
)

// DefaultProvider is the default LLM provider for compression.
const DefaultProvider = "cerebras"

// DefaultModel is the default model for compression.
const DefaultModel = "gpt-oss-120b"

// DefaultTimeoutMS is the default timeout in milliseconds.
// Cerebras is fast, but large inputs to gpt-oss-120b may take 15-30s.
const DefaultTimeoutMS = 30000

// DefaultModelContextTokens is the context window for the default model.
// Update this when DefaultModel changes.
const DefaultModelContextTokens = 128000

// DefaultMaxInputTokens is the default input budget for compression.
// Tuned for the default model; update if DefaultModel changes.
const DefaultMaxInputTokens = 50000

// DefaultCerebrasMaxOutputTokens is a generous ceiling for cerebras output tokens.
const DefaultCerebrasMaxOutputTokens = 40000

// DefaultReasoningEffort is the default reasoning effort for compression.
// Cerebras gpt-oss-120b supports reasoning_effort; low is appropriate for compression tasks.
const DefaultReasoningEffort = "medium"

// DefaultReasoningTokenReserve leaves headroom for model reasoning.
const DefaultReasoningTokenReserve = 20000

const (
	defaultChunkFraction = 0.6
	defaultParallelism   = 4
	minChunkBudgetChars  = 200
)

// LLMCompressor uses an LLM to compress already-packed candidate content based on intent.
// It is deliberately downstream of retrieval/ranking/context assembly: callers pass
// ordered Pieces, and the compressor only rewrites their representation to fit budget.
type LLMCompressor struct {
	provider        llm.Provider
	model           string
	timeout         time.Duration
	maxInputTokens  int
	maxOutputTokens int
	chunkChars      int
	parallelism     int
	reasoningTokens int
	name            string // provider name for metadata
	reasoningEffort llm.ReasoningEffort
}

// NewLLMCompressor creates a new LLM-based compressor.
func NewLLMCompressor(env vaultconfig.Env, cfg Config) (*LLMCompressor, error) {
	// Apply defaults
	provider := cfg.Provider
	if provider == "" {
		provider = DefaultProvider
	}
	model := cfg.Model
	if model == "" {
		model = DefaultModel
	}
	timeoutMS := cfg.TimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = DefaultTimeoutMS
	}
	maxInputTokens := cfg.MaxInputTokens
	if maxInputTokens <= 0 {
		maxInputTokens = DefaultMaxInputTokens
	}
	maxOutputTokens := cfg.MaxOutputTokens
	if maxOutputTokens < 0 {
		maxOutputTokens = 0
	}
	reasoningTokens := cfg.ReasoningTokenReserve
	if reasoningTokens <= 0 {
		reasoningTokens = DefaultReasoningTokenReserve
	}
	chunkChars := cfg.ChunkChars
	if chunkChars <= 0 {
		chunkChars = defaultChunkChars(maxInputTokens)
	}
	parallelism := cfg.Parallelism
	if parallelism <= 0 {
		parallelism = defaultParallelism
	}
	reasoningEffortStr := cfg.ReasoningEffort
	if reasoningEffortStr == "" {
		reasoningEffortStr = DefaultReasoningEffort
	}
	reasoningEffort := llm.ReasoningEffort(reasoningEffortStr)

	// Get API key from environment
	apiKey := llm.APIKeyForProvider(env, provider)
	if apiKey == "" {
		return nil, fmt.Errorf("no API key found for provider %q (set %s_API_KEY environment variable)", provider, strings.ToUpper(provider))
	}

	// Create the LLM provider
	resolvedProfile := llm.ResolvedProfile{
		Provider:        provider,
		Model:           model,
		ReasoningEffort: reasoningEffort,
		APIKey:          apiKey,
	}

	llmProvider, err := llm.NewProvider(resolvedProfile)
	if err != nil {
		return nil, fmt.Errorf("create LLM provider: %w", err)
	}

	return &LLMCompressor{
		provider:        llmProvider,
		model:           model,
		timeout:         time.Duration(timeoutMS) * time.Millisecond,
		maxInputTokens:  maxInputTokens,
		maxOutputTokens: maxOutputTokens,
		chunkChars:      chunkChars,
		parallelism:     parallelism,
		reasoningTokens: reasoningTokens,
		name:            provider,
		reasoningEffort: reasoningEffort,
	}, nil
}

// MaxInputChars returns the collection limit in characters.
// Used by PackWithIntent to collect more source material than the final response
// budget while still leaving provider headroom for prompt, output, and reasoning.
func (c *LLMCompressor) MaxInputChars() int {
	// Estimate ~4 chars per token, use 80% of model limit to leave room for prompt/output
	return int(float64(c.maxInputTokens) * 4 * 0.8)
}

// Compress uses the LLM to compress content based on intent.
// Always compresses for token density, even when content fits budget.
func (c *LLMCompressor) Compress(ctx context.Context, req Request) (Result, error) {
	// Preserve the caller's piece order. Filtering empty pieces is the only
	// structural change before prompt construction; ranking and priority decisions
	// have already happened in contextpack.
	filtered := make([]contextpack.Piece, 0, len(req.Pieces))
	originalSize := 0
	for _, p := range req.Pieces {
		text := strings.TrimSpace(p.Text)
		if text == "" {
			continue
		}
		originalSize += len(text)
		filtered = append(filtered, p)
	}
	if len(filtered) == 0 {
		return Result{Text: "", Compressed: false, Provider: c.name, Model: c.model}, nil
	}

	log.Printf("compression: starting (input=%d chars, budget=%d, pieces=%d, intent=%q)",
		originalSize, req.Budget, len(filtered), truncateIntent(req.Intent))

	chunkChars := c.chunkChars
	if chunkChars <= 0 {
		chunkChars = c.MaxInputChars()
	}
	// Large requested outputs can exceed a provider's per-response ceiling even
	// when the model has enough input context. Shrinking chunk size creates more
	// independently budgeted requests instead of asking one request for too many tokens.
	chunkChars = c.adjustChunkCharsForBudget(chunkChars, originalSize, estimateTokensForChars(req.Budget))
	chunks := splitPieces(filtered, chunkChars)
	budgets := allocateChunkBudgets(chunks, req.Budget)

	if len(chunks) > 1 {
		log.Printf("compression: chunked into %d requests (chunk=%d chars, parallelism=%d)",
			len(chunks), chunkChars, c.parallelism)
	}

	texts := make([]string, len(chunks))
	if err := c.compressChunks(ctx, chunks, budgets, req.Intent, texts); err != nil {
		log.Printf("compression: failed (%v)", err)
		return Result{
			Error: fmt.Errorf("LLM compression failed: %w", err),
		}, err
	}

	compressed := strings.TrimSpace(strings.Join(texts, "\n\n"))
	if compressed == "" {
		log.Printf("compression: failed (empty LLM response)")
		return Result{
			Error: fmt.Errorf("LLM returned empty response"),
		}, fmt.Errorf("empty LLM response")
	}

	var compressionErr error
	logSuffix := ""
	// Treat the character budget as the public contract. The model can overshoot, so
	// enforce the same TrimToBudget behavior callers would get from deterministic packing.
	if len(compressed) > req.Budget+500 {
		compressed = contextpack.TrimToBudget(compressed, req.Budget)
		compressionErr = fmt.Errorf("LLM output exceeded budget, truncated")
		logSuffix = " (truncated)"
	}

	compressedSize := len(compressed)
	ratio := float64(originalSize) / float64(compressedSize)
	savings := originalSize - compressedSize
	savingsPct := float64(savings) / float64(originalSize) * 100
	log.Printf("compression: done%s %d→%d chars (saved %d chars / %.0f%%, ratio %.1fx)",
		logSuffix, originalSize, compressedSize, savings, savingsPct, ratio)

	return Result{
		Text:             compressed,
		Compressed:       true,
		CompressionRatio: ratio,
		Provider:         c.name,
		Model:            c.model,
		Error:            compressionErr,
	}, nil
}

// NewFromLocalConfig creates a compressor from LocalConfig compression settings.
// Returns nil if compression is disabled or no API key is available.
// This is the preferred way to create a compressor for CLI commands.
func NewFromLocalConfig(env vaultconfig.Env, cfg *LocalCompressionConfig, debug bool) *LLMCompressor {
	if cfg == nil || cfg.Enabled == nil || !*cfg.Enabled {
		if debug {
			log.Printf("compression: disabled by config")
		}
		return nil
	}

	provider := cfg.Provider
	if provider == "" {
		provider = DefaultProvider
	}
	if llm.APIKeyForProvider(env, provider) == "" {
		if debug {
			log.Printf("compression: no API key for provider %q, compression disabled", provider)
		}
		return nil
	}

	// NewLLMCompressor owns defaults and normalization for both entry points.
	compressor, err := NewLLMCompressor(env, Config{
		Provider:              provider,
		Model:                 cfg.Model,
		TimeoutMS:             cfg.TimeoutMS,
		MaxInputTokens:        cfg.MaxInputTokens,
		MaxOutputTokens:       cfg.MaxOutputTokens,
		ReasoningTokenReserve: cfg.ReasoningTokenReserve,
		ChunkChars:            cfg.ChunkChars,
		Parallelism:           cfg.Parallelism,
		ReasoningEffort:       cfg.ReasoningEffort,
	})
	if err != nil {
		if debug {
			log.Printf("compression: failed to create compressor: %v", err)
		}
		return nil
	}

	if debug {
		log.Printf("compression: enabled with provider=%s model=%s", compressor.name, compressor.model)
	}
	return compressor
}

// LocalCompressionConfig mirrors the compression config from obsidian.LocalConfig.
// This avoids a circular dependency on the obsidian package.
type LocalCompressionConfig struct {
	Enabled               *bool
	Provider              string
	Model                 string
	TimeoutMS             int
	MaxInputTokens        int
	MaxOutputTokens       int
	ReasoningTokenReserve int
	ChunkChars            int
	Parallelism           int
	ReasoningEffort       string
}
