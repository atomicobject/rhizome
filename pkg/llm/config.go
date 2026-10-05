package llm

// Config stores named model profiles for LLM usage.
type Config struct {
	Profiles map[string]Profile `json:"profiles,omitempty" yaml:"profiles,omitempty"`
}

// Profile captures provider/model defaults for a named profile.
type Profile struct {
	Provider        string          `json:"provider,omitempty" yaml:"provider,omitempty"`
	Model           string          `json:"model,omitempty" yaml:"model,omitempty"`
	ReasoningEffort ReasoningEffort `json:"reasoningEffort,omitempty" yaml:"reasoningEffort,omitempty"`
}

// DefaultProfiles returns the built-in profile defaults.
func DefaultProfiles() map[string]Profile {
	return map[string]Profile{
		"instant": {
			Provider:        "openai",
			Model:           "gpt-5.2",
			ReasoningEffort: ReasoningNone,
		},
		"thinking": {
			Provider:        "openai",
			Model:           "gpt-5.2",
			ReasoningEffort: ReasoningMedium,
		},
	}
}

// Merge overlays override values onto a base profile.
func (p Profile) Merge(override Profile) Profile {
	result := p
	if override.Provider != "" {
		result.Provider = override.Provider
	}
	if override.Model != "" {
		result.Model = override.Model
	}
	if override.ReasoningEffort != "" {
		result.ReasoningEffort = override.ReasoningEffort
	}
	return result
}
