package init

import (
	"fmt"
	"io"
	"strings"

	"github.com/atomicobject/rhizome/pkg/llm"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// Default compression settings
const (
	defaultCompressionProvider = "cerebras"
	defaultCompressionModel    = "gpt-oss-120b"
)

// warnCompressionReadiness prints a warning if compression is enabled but API key is missing.
func warnCompressionReadiness(cfg obsidian.LocalConfig, out io.Writer) {
	if out == nil {
		return
	}
	if cfg.Compression == nil {
		return // Auto mode, no warning needed
	}
	if cfg.Compression.Enabled != nil && !*cfg.Compression.Enabled {
		return // Explicitly disabled
	}
	if cfg.Compression.Enabled != nil && *cfg.Compression.Enabled {
		// Explicitly enabled - check for API key
		provider := strings.ToLower(strings.TrimSpace(cfg.Compression.Provider))
		if provider == "" {
			provider = defaultCompressionProvider
		}
		apiKey := llm.APIKeyForProvider(provider)
		if apiKey == "" {
			envVar := providerEnvVar(provider)
			fmt.Fprintf(out, "%s: compression enabled but %s is not set.\n", styleWarn(out, "Warning"), envVar)
		}
	}
}

func providerEnvVar(provider string) string {
	switch strings.ToLower(provider) {
	case "cerebras":
		return "CEREBRAS_API_KEY"
	case "openai":
		return "OPENAI_API_KEY"
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "gemini":
		return "GEMINI_API_KEY"
	default:
		return strings.ToUpper(provider) + "_API_KEY"
	}
}

// isEmptyCompressionConfig checks if compression config can be omitted from output.
func isEmptyCompressionConfig(cfg *obsidian.LocalCompressionConfig) bool {
	if cfg == nil {
		return true
	}
	return cfg.Enabled == nil &&
		cfg.Provider == "" &&
		cfg.Model == "" &&
		cfg.TimeoutMS == 0 &&
		cfg.MaxInputTokens == 0 &&
		cfg.MaxOutputTokens == 0 &&
		cfg.ReasoningTokenReserve == 0 &&
		cfg.ChunkChars == 0 &&
		cfg.Parallelism == 0
}
