package init

import (
	"fmt"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"io"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/credentials"
	"github.com/atomicobject/rhizome/pkg/llm"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// warnCompressionReadiness prints a warning if compression is enabled but API key is missing.
func warnCompressionReadiness(env vaultconfig.Env, cfg repoconfig.LocalConfig, out io.Writer) {
	if out == nil {
		return
	}
	provider := credentials.ActiveCompressionProvider(&cfg)
	if provider != "" && llm.APIKeyForProvider(env, provider) == "" {
		envVar := providerEnvVar(provider)
		fmt.Fprintf(out, "%s: compression enabled but %s is not set.\n", styleWarn(out, "Warning"), envVar)
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
func isEmptyCompressionConfig(cfg *repoconfig.LocalCompressionConfig) bool {
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
