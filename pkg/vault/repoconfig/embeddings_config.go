package repoconfig

import (
	"errors"
	"strings"

	"github.com/atomicobject/rhizome/pkg/embeddings"
)

// ErrEmbeddingsProviderNotConfigured is returned when embeddings are enabled but no provider is configured.
var ErrEmbeddingsProviderNotConfigured = errors.New("embeddings enabled but provider not configured")

// SaveEmbeddingsConfig updates noteEmbeddings while avoiding persisted defaults.
// Provider is always preserved; model/endpoint are only persisted when non-default
// or already present in the config.
func SaveEmbeddingsConfig(vaultPath string, embCfg embeddings.Config) error {
	localCfg, err := LoadLocalConfig(vaultPath)
	if err != nil {
		return err
	}

	// Update enabled/provider, and persist explicit overrides without writing defaults.
	updated := mergeEmbeddingsForSave(localCfg.NoteEmbeddings, embCfg, "note")
	localCfg.NoteEmbeddings = &updated

	return SaveLocalConfig(vaultPath, *localCfg)
}

func mergeEmbeddingsForSave(existing *embeddings.Config, embCfg embeddings.Config, kind string) embeddings.Config {
	var merged embeddings.Config
	if existing != nil {
		merged = *existing
	}

	merged.Enabled = embCfg.Enabled

	provider := strings.TrimSpace(embCfg.Provider)
	if provider == "" {
		provider = strings.TrimSpace(merged.Provider)
	}
	if provider == "" {
		provider = "ollama"
	}
	provider = strings.ToLower(provider)
	merged.Provider = provider

	if shouldPersistModel(embCfg.Model, provider, kind) {
		merged.Model = embCfg.Model
	}
	if shouldPersistEndpoint(embCfg.Endpoint, provider) {
		merged.Endpoint = embCfg.Endpoint
	}
	if embCfg.Dimensions > 0 {
		merged.Dimensions = embCfg.Dimensions
	}
	merged.BatchSize = 0
	merged.MaxConcurrency = 0
	if embCfg.MaxSectionBytes > 0 {
		merged.MaxSectionBytes = embCfg.MaxSectionBytes
	}

	return merged
}

func shouldPersistModel(model, provider, kind string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	defaultModel := embeddings.DefaultModelForProvider(provider, kind)
	return !strings.EqualFold(model, defaultModel)
}

func shouldPersistEndpoint(endpoint, provider string) bool {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return false
	}
	defaultEndpoint := embeddings.DefaultEndpointForProvider(provider)
	if strings.EqualFold(provider, "ollama") {
		endpoint = embeddings.NormalizeOllamaEndpoint(endpoint)
		defaultEndpoint = embeddings.NormalizeOllamaEndpoint(defaultEndpoint)
	}
	return !strings.EqualFold(endpoint, defaultEndpoint)
}
