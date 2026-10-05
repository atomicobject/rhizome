package llm

import (
	"errors"
	"fmt"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"strings"
)

// ResolvedProfile is a concrete, validated profile ready for use.
type ResolvedProfile struct {
	Provider        string
	Model           string
	ReasoningEffort ReasoningEffort
	APIKey          string
}

// ModelSpec captures a parsed model override string.
type ModelSpec struct {
	Provider        string
	Model           string
	ReasoningEffort ReasoningEffort
}

// ResolveProfile merges defaults, global, and local config and applies an optional model override.
func ResolveProfile(profileName string, local *Config, global *Config, modelOverride string) (ResolvedProfile, error) {
	normalized := normalizeProfileName(profileName)
	defaults := DefaultProfiles()
	base, ok := defaults[normalized]
	if !ok {
		return ResolvedProfile{}, fmt.Errorf("unknown profile: %s", profileName)
	}

	if global != nil {
		if override, ok := global.Profiles[normalized]; ok {
			base = base.Merge(override)
		}
	}
	if local != nil {
		if override, ok := local.Profiles[normalized]; ok {
			base = base.Merge(override)
		}
	}

	if strings.TrimSpace(modelOverride) != "" {
		spec, err := ParseModelSpec(modelOverride)
		if err != nil {
			return ResolvedProfile{}, err
		}
		if spec.Model != "" {
			base.Model = spec.Model
		}
		if spec.Provider != "" {
			base.Provider = spec.Provider
		}
		if spec.ReasoningEffort != "" {
			base.ReasoningEffort = spec.ReasoningEffort
		}
	}

	provider := strings.TrimSpace(base.Provider)
	if provider == "" {
		provider = InferProvider(base.Model)
	}
	if provider == "" {
		return ResolvedProfile{}, errors.New("provider could not be inferred from model")
	}
	provider = strings.ToLower(provider)
	if !isKnownProvider(provider) {
		return ResolvedProfile{}, fmt.Errorf("unsupported provider: %s", provider)
	}

	apiKey := APIKeyForProvider(provider)
	if provider != "mock" && apiKey == "" {
		return ResolvedProfile{}, fmt.Errorf("missing API key for provider %s", provider)
	}

	return ResolvedProfile{
		Provider:        provider,
		Model:           base.Model,
		ReasoningEffort: base.ReasoningEffort,
		APIKey:          apiKey,
	}, nil
}

// ParseModelSpec parses a model override string such as "openai/gpt-5.2:high".
func ParseModelSpec(raw string) (ModelSpec, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ModelSpec{}, errors.New("model spec cannot be empty")
	}

	provider := ""
	modelPart := trimmed
	if slash := strings.Index(trimmed, "/"); slash > 0 {
		provider = strings.ToLower(strings.TrimSpace(trimmed[:slash]))
		modelPart = strings.TrimSpace(trimmed[slash+1:])
		if provider == "" {
			return ModelSpec{}, errors.New("provider prefix is empty")
		}
		if !isKnownProvider(provider) {
			return ModelSpec{}, fmt.Errorf("unsupported provider: %s", provider)
		}
	}

	effort := ReasoningEffort("")
	if colon := strings.LastIndex(modelPart, ":"); colon > 0 {
		suffix := strings.ToLower(strings.TrimSpace(modelPart[colon+1:]))
		if isReasoningEffort(suffix) {
			effort = ReasoningEffort(suffix)
			modelPart = strings.TrimSpace(modelPart[:colon])
		} else {
			return ModelSpec{}, fmt.Errorf("unsupported reasoning effort: %s", suffix)
		}
	}

	if modelPart == "" {
		return ModelSpec{}, errors.New("model name cannot be empty")
	}

	return ModelSpec{
		Provider:        provider,
		Model:           modelPart,
		ReasoningEffort: effort,
	}, nil
}

// InferProvider infers a provider from a model name when no prefix is supplied.
func InferProvider(model string) string {
	lower := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(lower, "gpt-oss"):
		return "cerebras"
	case strings.HasPrefix(lower, "gpt"):
		return "openai"
	case strings.HasPrefix(lower, "claude"):
		return "anthropic"
	case strings.HasPrefix(lower, "gemini"):
		return "gemini"
	case strings.HasPrefix(lower, "llama"), strings.HasPrefix(lower, "qwen"), strings.HasPrefix(lower, "zai-glm"):
		return "cerebras"
	default:
		return ""
	}
}

func normalizeProfileName(name string) string {
	trimmed := strings.ToLower(strings.TrimSpace(name))
	if trimmed == "" {
		return "instant"
	}
	if trimmed == "fast" {
		return "instant"
	}
	return trimmed
}

func isReasoningEffort(value string) bool {
	switch ReasoningEffort(value) {
	case ReasoningNone, ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningXHigh:
		return true
	default:
		return false
	}
}

func isKnownProvider(provider string) bool {
	switch provider {
	case "openai", "anthropic", "gemini", "cerebras", "mock":
		return true
	default:
		return false
	}
}

// APIKeyForProvider returns the API key for a provider from the environment or team keys.
func APIKeyForProvider(provider string) string {
	switch provider {
	case "openai":
		return vaultconfig.ResolveValue("OPENAI_API_KEY")
	case "anthropic":
		return vaultconfig.ResolveValue("ANTHROPIC_API_KEY")
	case "gemini":
		return vaultconfig.ResolveValue("GEMINI_API_KEY")
	case "cerebras":
		return vaultconfig.ResolveValue("CEREBRAS_API_KEY")
	default:
		return ""
	}
}
