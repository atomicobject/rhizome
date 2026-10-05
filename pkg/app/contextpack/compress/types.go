// Package compress provides LLM-based compression for context packing.
//
// Docs: [CONTEXT.md](pkg/app/contextpack/compress/CONTEXT.md)
//
// The compressors in this package implement the contextpack.Compressor interface.
// Compression is an optional boundary after deterministic packing: it may densify
// already-rendered pieces, but it must not fetch missing content, change ordering
// semantics, or become required for a context tool to return useful output.
package compress

import (
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
)

// Re-export types from contextpack for convenience.
// This allows callers to use compress.Request instead of contextpack.CompressRequest.
type (
	Request = contextpack.CompressRequest
	Result  = contextpack.CompressResult
)

// Config holds configuration for creating a compressor.
type Config struct {
	// Provider is the LLM provider to use (cerebras, openai, anthropic, etc.).
	Provider string

	// Model is the provider-specific model to use.
	Model string

	// TimeoutMS is the timeout in milliseconds for compression requests.
	TimeoutMS int

	// MaxInputTokens is the model's input token limit, used to determine
	// how much content to collect for compression. If zero, uses DefaultMaxInputTokens.
	MaxInputTokens int

	// MaxOutputTokens is the maximum tokens to allow in a single compression response.
	// If zero, Cerebras uses an auto-sized limit based on budget; other providers use
	// their defaults unless set explicitly.
	MaxOutputTokens int

	// ReasoningTokenReserve is a token reserve to leave room for internal reasoning.
	// If zero, uses a sensible default for the default model.
	ReasoningTokenReserve int

	// ChunkChars caps the approximate input size (in chars) per LLM request.
	// If zero, a default based on MaxInputTokens and Parallelism is used.
	ChunkChars int

	// Parallelism is the maximum number of concurrent LLM requests.
	// If zero, defaults to a small, safe value.
	Parallelism int

	// ReasoningEffort controls the reasoning effort for providers that support it.
	// Valid values: "none", "low", "medium", "high". If empty, uses DefaultReasoningEffort.
	ReasoningEffort string
}
