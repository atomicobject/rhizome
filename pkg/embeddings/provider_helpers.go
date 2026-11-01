package embeddings

import (
	"fmt"
	"github.com/atomicobject/rhizome/pkg/teamkeys"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"

	"strings"
)

// NewProviderForConfig returns a provider and its config, resolving API keys when needed.
// When RHIZOME_EMBEDDING_CACHE names a path, live providers are wrapped in a SQLite
// content-hash cache so repeated runs reuse bit-identical vectors (see NewCachedProvider).
// The caller owns the returned provider and must close it when it implements io.Closer.
func NewProviderForConfig(env vaultconfig.Env, cfg Config, apiKey string) (Provider, ProviderConfig, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		apiKey = ResolveAPIKeyForProvider(env, cfg.Provider)
	}
	providerCfg := cfg.ProviderCfg(apiKey)
	provider, err := NewConfiguredProvider(env, providerCfg)
	return provider, providerCfg, err
}

// NewConfiguredProvider builds a provider from an already resolved
// configuration, adding the optional RHIZOME_EMBEDDING_CACHE wrapper. The
// caller owns the returned provider and must close it when it implements io.Closer.
func NewConfiguredProvider(env vaultconfig.Env, providerCfg ProviderConfig) (Provider, error) {
	provider, err := NewProvider(providerCfg)
	if err != nil {
		return nil, err
	}
	if path := strings.TrimSpace(envValue(env, "RHIZOME_EMBEDDING_CACHE")); path != "" && providerCfg.Provider != "test" {
		provider, err = NewCachedProvider(provider, providerCfg, path)
		if err != nil {
			return nil, err
		}
	}
	return provider, nil
}

// ResolveAPIKeyForProvider returns the appropriate API key for the given provider name.
func ResolveAPIKeyForProvider(env vaultconfig.Env, provider string) string {
	switch provider {
	case "voyage":
		return ResolveVoyageAPIKey(env, "")
	default:
		return ResolveAPIKey(env, "")
	}
}

// ResolveVoyageAPIKey returns the Voyage AI API key from env vars or team keys.
func ResolveVoyageAPIKey(env vaultconfig.Env, explicit string) string {
	if explicit != "" {
		return explicit
	}
	if val := vaultconfig.ResolveValue(env, "RHIZOME_VOYAGE_API_KEY", "VOYAGE_API_KEY"); val != "" {
		return val
	}
	// Fall back to team key if available
	return teamkeys.VoyageKey(env)
}

// ProviderUnavailableError standardizes provider initialization errors.
func ProviderUnavailableError(cfg Config, err error) error {
	return fmt.Errorf("provider unavailable (provider=%s model=%s): %w", cfg.Provider, cfg.Model, err)
}

func envValue(env vaultconfig.Env, key string) string { value, _ := env.Lookup(key); return value }
