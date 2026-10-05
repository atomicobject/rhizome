package embeddings

// Docs: [Embeddings - providers + configuration](docs/reference/guides/Embeddings - providers + configuration.md)

import (
	"path/filepath"
	"strings"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
)

const (
	// DefaultBatchSize and DefaultMaxConcurrent are fallbacks when a provider does not
	// supply its own defaults.
	//
	// Most callers should leave batchSize/maxConcurrency unset (0) to use the
	// provider's defaults.
	DefaultBatchSize     = 8
	DefaultMaxConcurrent = 4

	// DefaultOpenAIModel is the default model for note embeddings (higher quality).
	DefaultOpenAIModel = "text-embedding-3-large"
	// DefaultOpenAICodeModel is the default model for code embeddings (faster, cheaper).
	DefaultOpenAICodeModel = "text-embedding-3-small"
	// DefaultOpenAIEndpoint is the default OpenAI embeddings API endpoint.
	DefaultOpenAIEndpoint = "https://api.openai.com/v1/embeddings"
	// DefaultOllamaEndpoint is the default Ollama embeddings API endpoint.
	// Uses /api/embed (batch endpoint) rather than /api/embeddings (single).
	DefaultOllamaEndpoint = "http://localhost:11434/api/embed"
	// DefaultOllamaModel is the recommended Ollama embeddings model when no local config is set.
	DefaultOllamaModel = "nomic-embed-text:latest"
)

// DefaultEndpointForProvider returns the default embeddings endpoint for a provider.
func DefaultEndpointForProvider(provider string) string {
	if strings.EqualFold(strings.TrimSpace(provider), "ollama") {
		return DefaultOllamaEndpoint
	}
	return DefaultOpenAIEndpoint
}

// DefaultModelForProvider returns the default model for a provider and kind (note or code).
func DefaultModelForProvider(provider, kind string) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	if p == "" {
		p = "ollama" // default provider
	}
	if p == "ollama" {
		return DefaultOllamaModel
	}
	// OpenAI
	if kind == "code" {
		return DefaultOpenAICodeModel
	}
	return DefaultOpenAIModel
}

// ApplyProviderDefaults ensures provider-specific defaults are applied.
// This includes fixing the model when it's empty or belongs to a different provider.
func ApplyProviderDefaults(cfg Config) Config {
	return ApplyProviderDefaultsForKind(cfg, "note")
}

// ApplyProviderDefaultsForKind ensures provider-specific defaults for note or code embeddings.
func ApplyProviderDefaultsForKind(cfg Config, kind string) Config {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		provider = "ollama" // default
		cfg.Provider = provider
	}

	switch provider {
	case "ollama":
		// Fix endpoint if unset or pointing to OpenAI/Voyage
		if strings.TrimSpace(cfg.Endpoint) == "" || cfg.Endpoint == DefaultOpenAIEndpoint || cfg.Endpoint == DefaultVoyageEndpoint {
			cfg.Endpoint = DefaultOllamaEndpoint
		}
		cfg.Endpoint = NormalizeOllamaEndpoint(cfg.Endpoint)

		// Fix model if unset or using an OpenAI/Voyage model
		if cfg.Model == "" || isOpenAIModel(cfg.Model) || isVoyageModel(cfg.Model) {
			cfg.Model = DefaultOllamaModel
		}
	case "openai":
		// Fix endpoint if unset or pointing to Ollama/Voyage
		if strings.TrimSpace(cfg.Endpoint) == "" || isOllamaEndpoint(cfg.Endpoint) || cfg.Endpoint == DefaultVoyageEndpoint {
			cfg.Endpoint = DefaultOpenAIEndpoint
		}

		// Fix model if unset or using an Ollama/Voyage model
		if cfg.Model == "" || isOllamaModel(cfg.Model) || isVoyageModel(cfg.Model) {
			if kind == "code" {
				cfg.Model = DefaultOpenAICodeModel
			} else {
				cfg.Model = DefaultOpenAIModel
			}
		}
	case "voyage":
		// Fix endpoint if unset or pointing to Ollama/OpenAI
		if strings.TrimSpace(cfg.Endpoint) == "" || isOllamaEndpoint(cfg.Endpoint) || cfg.Endpoint == DefaultOpenAIEndpoint {
			cfg.Endpoint = DefaultVoyageEndpoint
		}

		// Fix model if unset or using an Ollama/OpenAI model
		if cfg.Model == "" || isOllamaModel(cfg.Model) || isOpenAIModel(cfg.Model) {
			if kind == "code" {
				cfg.Model = DefaultVoyageCodeModel
			} else {
				cfg.Model = DefaultVoyageModel
			}
		}
	}
	return cfg
}

// isOpenAIModel returns true if the model name is a known OpenAI embedding model.
func isOpenAIModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(m, "text-embedding-")
}

// isOllamaModel returns true if the model name looks like an Ollama model.
func isOllamaModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	// Common Ollama embedding models
	return strings.Contains(m, "nomic-embed") ||
		strings.Contains(m, "mxbai-embed") ||
		strings.Contains(m, "all-minilm") ||
		strings.Contains(m, "snowflake-arctic") ||
		strings.HasSuffix(m, ":latest") // Ollama tag convention
}

// isVoyageModel returns true if the model name is a known Voyage AI embedding model.
func isVoyageModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(m, "voyage-")
}

// isOllamaEndpoint returns true if the endpoint looks like an Ollama endpoint.
func isOllamaEndpoint(endpoint string) bool {
	e := strings.ToLower(strings.TrimSpace(endpoint))
	return strings.Contains(e, "localhost:11434") ||
		strings.Contains(e, "127.0.0.1:11434") ||
		strings.Contains(e, "/api/embed")
}

// Config captures user-configurable settings for embeddings.
type Config struct {
	// NOTE: We intentionally include explicit yaml tags because gopkg.in/yaml.v3
	// does not honor json tags. Without yaml:",omitempty" tags, writing configs
	// would emit a lot of default/zero fields.
	//
	// The yaml key names are kept compatible with the historical encoder output
	// (field-name lowercasing), so existing configs don't need to be migrated.
	Enabled        bool   `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	IndexPath      string `json:"indexPath,omitempty" yaml:"indexpath,omitempty"` // ignored when a unified index path is configured at the vault level
	Provider       string `json:"provider,omitempty" yaml:"provider,omitempty"`
	Model          string `json:"model,omitempty" yaml:"model,omitempty"`
	Endpoint       string `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	Dimensions     int    `json:"dimensions,omitempty" yaml:"dimensions,omitempty"`
	BatchSize      int    `json:"batchSize,omitempty" yaml:"batchsize,omitempty"`
	MaxConcurrency int    `json:"maxConcurrency,omitempty" yaml:"maxconcurrency,omitempty"`
	// MaxSectionBytes caps per-section chunk text for note embeddings (0 = default).
	MaxSectionBytes int `json:"maxSectionBytes,omitempty" yaml:"maxsectionbytes,omitempty"`
}

// MarshalYAML keeps provider throughput knobs out of persisted vault config.
// Batch sizing and concurrency are runtime/provider policy decisions; stale
// values here can silently cap full-scan throughput across releases.
func (c Config) MarshalYAML() (any, error) {
	type yamlConfig Config
	c.BatchSize = 0
	c.MaxConcurrency = 0
	return yamlConfig(c), nil
}

// DefaultConfig returns a config populated with sensible defaults for a vault.
// The default provider is Ollama (local embeddings) with nomic-embed-text:latest.
func DefaultConfig(vaultPath string) Config {
	return Config{
		Provider:       "ollama",
		Model:          DefaultOllamaModel,
		Endpoint:       DefaultOllamaEndpoint,
		Dimensions:     0, // auto-detect from provider when unset
		IndexPath:      DefaultIndexPath(vaultPath),
		BatchSize:      0, // provider default
		MaxConcurrency: 0, // provider default
	}
}

// DefaultCodeConfig returns a config for code embeddings with code-appropriate defaults.
// Uses the same model as DefaultConfig (Ollama nomic-embed-text:latest by default).
func DefaultCodeConfig(vaultPath string) Config {
	return DefaultConfig(vaultPath)
}

// DefaultOllamaConfig returns a config for local Ollama embeddings.
func DefaultOllamaConfig(vaultPath string) Config {
	return DefaultConfig(vaultPath) // DefaultConfig already uses Ollama defaults
}

// DefaultOllamaCodeConfig returns a config for local Ollama code embeddings.
func DefaultOllamaCodeConfig(vaultPath string) Config {
	return DefaultOllamaConfig(vaultPath)
}

// DefaultOpenAIConfig returns a config for OpenAI embeddings (note embeddings).
func DefaultOpenAIConfig(vaultPath string) Config {
	return Config{
		Provider:       "openai",
		Model:          DefaultOpenAIModel,
		Endpoint:       DefaultOpenAIEndpoint,
		Dimensions:     0, // auto-detect from provider when unset
		IndexPath:      DefaultIndexPath(vaultPath),
		BatchSize:      0, // provider default
		MaxConcurrency: 0, // provider default
	}
}

// DefaultOpenAICodeConfig returns a config for OpenAI code embeddings.
func DefaultOpenAICodeConfig(vaultPath string) Config {
	cfg := DefaultOpenAIConfig(vaultPath)
	cfg.Model = DefaultOpenAICodeModel
	return cfg
}

// DefaultVoyageConfig returns a config for Voyage AI embeddings (note embeddings).
func DefaultVoyageConfig(vaultPath string) Config {
	return Config{
		Provider:       "voyage",
		Model:          DefaultVoyageModel,
		Endpoint:       DefaultVoyageEndpoint,
		Dimensions:     0, // auto-detect from provider when unset
		IndexPath:      DefaultIndexPath(vaultPath),
		BatchSize:      0, // provider default
		MaxConcurrency: 0, // provider default
	}
}

// DefaultVoyageCodeConfig returns a config for Voyage AI code embeddings.
func DefaultVoyageCodeConfig(vaultPath string) Config {
	cfg := DefaultVoyageConfig(vaultPath)
	cfg.Model = DefaultVoyageCodeModel
	return cfg
}

// DefaultCodeIndexPath returns the default path for the code embeddings index.
//
// Deprecated: code embeddings are stored in the unified SQLite index (DefaultIndexPath).
func DefaultCodeIndexPath(vaultPath string) string {
	return DefaultIndexPath(vaultPath)
}

// Merge applies non-zero/empty fields from override onto base.
func (c Config) Merge(override Config) Config {
	result := c
	if override.IndexPath != "" {
		result.IndexPath = override.IndexPath
	}
	if override.Provider != "" {
		result.Provider = override.Provider
	}
	if override.Model != "" {
		result.Model = override.Model
	}
	if override.Endpoint != "" {
		result.Endpoint = override.Endpoint
	}
	if override.Dimensions > 0 {
		result.Dimensions = override.Dimensions
	}
	if override.MaxSectionBytes > 0 {
		result.MaxSectionBytes = override.MaxSectionBytes
	}
	if override.Enabled {
		result.Enabled = true
	}
	return result
}

// InheritCodeSettings applies semantic embedding settings to a code embeddings config.
// This preserves the code model while inheriting provider, endpoint, dimensions,
// and index path when present. Enabled follows source.
func InheritCodeSettings(codeCfg Config, source Config) Config {
	if source.IndexPath != "" {
		codeCfg.IndexPath = source.IndexPath
	}
	if source.Provider != "" {
		codeCfg.Provider = source.Provider
	}
	if source.Endpoint != "" {
		codeCfg.Endpoint = source.Endpoint
	}
	if source.Dimensions > 0 {
		codeCfg.Dimensions = source.Dimensions
	}
	codeCfg.Enabled = source.Enabled
	return codeCfg
}

// ProviderCfg converts to a ProviderConfig, injecting the API key.
func (c Config) ProviderCfg(apiKey string) ProviderConfig {
	cfg := ProviderConfig{
		Provider:   c.Provider,
		Model:      c.Model,
		APIKey:     apiKey,
		Endpoint:   c.Endpoint,
		Dimensions: c.Dimensions,
	}
	if strings.EqualFold(strings.TrimSpace(c.Provider), "ollama") {
		cfg.Endpoint = NormalizeOllamaEndpoint(c.Endpoint)
	}
	return cfg
}

// DefaultIndexPath returns the default path for the local SQLite index.
func DefaultIndexPath(vaultPath string) string {
	return filepath.Join(vaultPath, ".rhizome", "db.sqlite")
}

// ResolveAPIKey chooses the first non-empty API key from explicit flag, env, or team keys.
func ResolveAPIKey(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if val := vaultconfig.ResolveValue("RHIZOME_OPENAI_API_KEY", "OPENAI_API_KEY"); val != "" {
		return val
	}
	return ""
}
