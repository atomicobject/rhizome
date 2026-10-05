package embeddings

import (
	"fmt"
	"os"
	"strings"
)

// NewProviderForConfig returns a provider and its config, resolving API keys when needed.
// When RHIZOME_EMBEDDING_CACHE names a path, live providers are wrapped in a SQLite
// content-hash cache so repeated runs reuse bit-identical vectors (see NewCachedProvider).
// The caller owns the returned provider and must close it when it implements io.Closer.
func NewProviderForConfig(cfg Config, apiKey string) (Provider, ProviderConfig, error) {
	if apiKey == "" {
		apiKey = ResolveAPIKeyForProvider(cfg.Provider)
	}
	providerCfg := cfg.ProviderCfg(apiKey)
	provider, err := NewProvider(providerCfg)
	if err != nil {
		return nil, providerCfg, err
	}
	if path := strings.TrimSpace(os.Getenv("RHIZOME_EMBEDDING_CACHE")); path != "" && cfg.Provider != "test" {
		provider, err = NewCachedProvider(provider, providerCfg, path)
		if err != nil {
			return nil, providerCfg, err
		}
	}
	return provider, providerCfg, nil
}

// ResolveAPIKeyForProvider returns the appropriate API key for the given provider name.
func ResolveAPIKeyForProvider(provider string) string {
	switch provider {
	case "voyage":
		return ResolveVoyageAPIKey("")
	default:
		return ResolveAPIKey("")
	}
}

// ProviderUnavailableError standardizes provider initialization errors.
func ProviderUnavailableError(cfg Config, err error) error {
	return fmt.Errorf("provider unavailable (provider=%s model=%s): %w", cfg.Provider, cfg.Model, err)
}
