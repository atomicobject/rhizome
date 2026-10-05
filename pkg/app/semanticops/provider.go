package semanticops

import (
	"strings"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

func PrepareProvider(cfg embeddings.Config, apiKey string) (embeddings.Provider, embeddings.ProviderConfig, error) {
	resolved := strings.TrimSpace(apiKey)
	if resolved == "" {
		resolved = embeddings.ResolveAPIKeyForProvider(cfg.Provider)
	}
	return embeddings.NewProviderForConfig(cfg, resolved)
}
