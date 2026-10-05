package init

import (
	"fmt"
	"io"
	"strings"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func inferEmbeddingsProvider(cfg *embeddings.Config) string {
	if cfg == nil {
		return ""
	}
	if strings.TrimSpace(cfg.Provider) != "" {
		return cfg.Provider
	}
	model := strings.ToLower(strings.TrimSpace(cfg.Model))
	if model != "" {
		if strings.HasPrefix(model, "text-embedding-") {
			return "openai"
		}
		if strings.Contains(model, "nomic-embed") {
			return "ollama"
		}
		if strings.HasPrefix(model, "voyage-") {
			return "voyage"
		}
	}
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint != "" {
		if strings.EqualFold(endpoint, embeddings.DefaultOllamaEndpoint) {
			return "ollama"
		}
		if strings.Contains(endpoint, "voyageai.com") {
			return "voyage"
		}
	}
	return "openai"
}

func providerDisplayName(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "voyage":
		return "Voyage AI"
	case "openai":
		return "OpenAI"
	case "ollama":
		return "Ollama"
	case "":
		return "unknown"
	default:
		return provider
	}
}

// embeddingsNeedProviderConfig returns true if embeddings are enabled but
// the provider is not explicitly set in the config. This is used to force
// provider selection when upgrading old configs that predate the requirement
// for explicit provider configuration.
func embeddingsNeedProviderConfig(cfg obsidian.LocalConfig) bool {
	if cfg.NoteEmbeddings != nil && cfg.NoteEmbeddings.Enabled {
		if strings.TrimSpace(cfg.NoteEmbeddings.Provider) == "" {
			return true
		}
	}
	if cfg.CodeEmbeddings != nil && cfg.CodeEmbeddings.Enabled {
		if strings.TrimSpace(cfg.CodeEmbeddings.Provider) == "" {
			return true
		}
	}
	return false
}

func embeddingsProviderReady(cfg *embeddings.Config) (bool, string) {
	if cfg == nil {
		return false, ""
	}
	p := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if p == "" {
		p = "openai"
	}
	switch p {
	case "openai":
		if embeddings.ResolveAPIKey("") != "" {
			return true, ""
		}
		return false, "missing OPENAI_API_KEY or RHIZOME_OPENAI_API_KEY"
	case "voyage":
		if embeddings.ResolveVoyageAPIKey("") != "" {
			return true, ""
		}
		return false, "missing VOYAGE_API_KEY or RHIZOME_VOYAGE_API_KEY"
	case "ollama":
		if !embeddings.OllamaEndpointIsLocal(cfg.Endpoint) {
			return true, ""
		}
		if embeddings.OllamaInstalled() {
			return true, ""
		}
		return false, "Ollama not installed"
	case "none":
		return false, "provider set to none"
	default:
		return false, fmt.Sprintf("unknown provider %q", p)
	}
}

func warnEmbeddingsReadiness(cfg obsidian.LocalConfig, out io.Writer) {
	if out == nil {
		return
	}
	if cfg.NoteEmbeddings != nil && cfg.NoteEmbeddings.Enabled {
		if ok, reason := embeddingsProviderReady(cfg.NoteEmbeddings); !ok && reason != "" {
			fmt.Fprintf(out, "%s: note embeddings enabled but %s.\n", styleWarn(out, "Warning"), reason)
			if strings.Contains(strings.ToLower(reason), "ollama") {
				if hints := embeddings.OllamaInstallHint(); hints != "" {
					fmt.Fprintf(out, "  %s\n", styleDim(out, hints))
				}
			}
		}
	}
	if cfg.CodeEmbeddings != nil && cfg.CodeEmbeddings.Enabled {
		if ok, reason := embeddingsProviderReady(cfg.CodeEmbeddings); !ok && reason != "" {
			fmt.Fprintf(out, "%s: code embeddings enabled but %s.\n", styleWarn(out, "Warning"), reason)
			if strings.Contains(strings.ToLower(reason), "ollama") {
				if hints := embeddings.OllamaInstallHint(); hints != "" {
					fmt.Fprintf(out, "  %s\n", styleDim(out, hints))
				}
			}
		}
	}
}
