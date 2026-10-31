package embeddings

// Docs: [Embeddings - providers + configuration](docs/reference/guides/Embeddings - providers + configuration.md)

import (
	"context"
	"net/http"
)

// ProviderConfig captures configuration for an embedding provider.
type ProviderConfig struct {
	Provider       string // openai, ollama, none
	Model          string
	APIKey         string
	Endpoint       string
	Dimensions     int
	MaxConcurrency int // optional hint for batching/concurrency
	BatchSize      int // optional hint for batching requests
	// Managed sends Voyage requests through the team API gateway instead of an
	// API key. Only the application's provider-access factory sets it; provider
	// code never reads the session store. Endpoint keeps the configured
	// (logical) Voyage endpoint so index, lane, and cache identity do not change.
	Managed *ManagedAccess
}

// ManagedAccess routes a provider through the team API gateway. Transport adds
// the session credential and returns gateway-originated errors as
// *wire.GatewayError; the provider adds no Authorization header of its own.
type ManagedAccess struct {
	Origin         string
	Transport      http.RoundTripper
	MaxConcurrency int // the gateway's advertised limit; 0 adds no cap
}

// Provider produces embeddings for one or more text inputs.
type Provider interface {
	EmbedTexts(ctx context.Context, texts []string) ([]Embedding, error)
	Dimensions() int
}

// ContextTokenProvider optionally reports the max context length (tokens) for a model.
type ContextTokenProvider interface {
	ContextTokens(ctx context.Context) (int, error)
}
