package llm

import "fmt"

// NewProvider returns a provider instance for a resolved profile.
func NewProvider(profile ResolvedProfile) (Provider, error) {
	switch profile.Provider {
	case "openai":
		return NewOpenAIProvider(profile.APIKey), nil
	case "anthropic":
		return NewAnthropicProvider(profile.APIKey), nil
	case "gemini":
		return NewGeminiProvider(profile.APIKey), nil
	case "cerebras":
		return NewCerebrasProvider(profile.APIKey), nil
	case "mock":
		return &MockProvider{}, nil
	default:
		return nil, fmt.Errorf("unsupported provider: %s", profile.Provider)
	}
}
