// Package credentials centralizes API-credential onboarding for rzm commands.
//
// A Session owns all credential state for a single CLI run: keys provided at
// any prompt, persisted skips, and what is already resolvable from the
// process environment, the global CLI config, or the Atomic Object team key.
// Init and non-init commands (such as `rzm index`) share the same Session
// logic so a key or skip recorded once is never asked for again.
//
// Docs: [[init-starter-workflow#^spec-0038-us5]]
package credentials

import (
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// AtomicRhizomeKey is the env var holding the Atomic Object team key that
// unlocks all team-covered providers (voyage, typesafe).
const AtomicRhizomeKey = "ATOMIC_RHIZOME_KEY"

// defaultCompressionProvider applies only to explicitly enabled compression.
const defaultCompressionProvider = "cerebras"

// Need describes one credential requirement derived from vault config.
type Need struct {
	// Key is the canonical env var the credential is stored under.
	Key string
	// AltKeys are alternative env vars that also satisfy this need.
	AltKeys []string
	// Label is the human-readable name shown in prompts.
	Label string
	// Purpose names the feature(s) that need the credential.
	Purpose string
	// AllowTeamKey marks providers covered by the Atomic Object team key.
	AllowTeamKey bool
}

func (n Need) keys() []string {
	return append([]string{n.Key}, n.AltKeys...)
}

// NeedForProvider maps a provider name to its credential need. Providers that
// do not require an API key (e.g. ollama) return ok=false.
func NeedForProvider(provider, purpose string) (Need, bool) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai":
		return Need{Key: "OPENAI_API_KEY", AltKeys: []string{"RHIZOME_OPENAI_API_KEY"}, Label: "OpenAI API Key", Purpose: purpose, AllowTeamKey: false}, true
	case "voyage":
		return Need{Key: "VOYAGE_API_KEY", AltKeys: []string{"RHIZOME_VOYAGE_API_KEY"}, Label: "Voyage API Key", Purpose: purpose, AllowTeamKey: true}, true
	case "typesafe":
		return Need{Key: "TYPESAFE_API_KEY", Label: "TypeSafe API Key", Purpose: purpose, AllowTeamKey: true}, true
	case "cerebras":
		return Need{Key: "CEREBRAS_API_KEY", Label: "Cerebras API Key", Purpose: purpose, AllowTeamKey: false}, true
	case "anthropic":
		return Need{Key: "ANTHROPIC_API_KEY", Label: "Anthropic API Key", Purpose: purpose, AllowTeamKey: false}, true
	default:
		return Need{}, false
	}
}

// IsSupportedKey reports whether key is a credential key used by a supported
// provider. It accepts canonical keys and the existing Rhizome aliases.
func IsSupportedKey(key string) bool {
	for _, provider := range []string{"openai", "voyage", "typesafe", "cerebras", "anthropic"} {
		need, ok := NeedForProvider(provider, "")
		if !ok {
			continue
		}
		for _, candidate := range need.keys() {
			if key == candidate {
				return true
			}
		}
	}
	return false
}

// CollectNeeds gathers credential needs across every enabled feature: note
// embeddings, code embeddings, and compression. Duplicate keys are merged
// with combined purposes.
func CollectNeeds(cfg *obsidian.LocalConfig) []Need {
	if cfg == nil {
		return nil
	}
	var needs []Need
	seen := map[string]int{}
	add := func(provider, purpose string) {
		need, ok := NeedForProvider(provider, purpose)
		if !ok {
			return
		}
		if idx, exists := seen[need.Key]; exists {
			needs[idx].Purpose = mergePurpose(needs[idx].Purpose, purpose)
			return
		}
		seen[need.Key] = len(needs)
		needs = append(needs, need)
	}

	if cfg.NoteEmbeddings != nil && cfg.NoteEmbeddings.Enabled {
		add(cfg.NoteEmbeddings.Provider, "semantic search")
	}
	if cfg.CodeEmbeddings != nil && cfg.CodeEmbeddings.Enabled {
		add(cfg.CodeEmbeddings.Provider, "semantic search")
	}
	if provider := ActiveCompressionProvider(cfg); provider != "" {
		add(provider, "compression")
	}
	return needs
}

// CollectEmbeddingsNeeds gathers needs for enabled note/code embeddings only.
func CollectEmbeddingsNeeds(cfg *obsidian.LocalConfig) []Need {
	if cfg == nil {
		return nil
	}
	var needs []Need
	seen := map[string]struct{}{}
	add := func(provider string) {
		need, ok := NeedForProvider(provider, "semantic search")
		if !ok {
			return
		}
		if _, exists := seen[need.Key]; exists {
			return
		}
		seen[need.Key] = struct{}{}
		needs = append(needs, need)
	}
	if cfg.NoteEmbeddings != nil && cfg.NoteEmbeddings.Enabled {
		add(cfg.NoteEmbeddings.Provider)
	}
	if cfg.CodeEmbeddings != nil && cfg.CodeEmbeddings.Enabled {
		add(cfg.CodeEmbeddings.Provider)
	}
	return needs
}

// CollectCompressionNeeds gathers the need for the active compression
// provider, if any.
func CollectCompressionNeeds(cfg *obsidian.LocalConfig) []Need {
	provider := ActiveCompressionProvider(cfg)
	need, ok := NeedForProvider(provider, "compression")
	if !ok {
		return nil
	}
	return []Need{need}
}

// ActiveCompressionProvider returns the provider compression will use, or ""
// when compression is disabled or would not auto-enable.
func ActiveCompressionProvider(cfg *obsidian.LocalConfig) string {
	if cfg == nil || cfg.Compression == nil || cfg.Compression.Enabled == nil || !*cfg.Compression.Enabled {
		return ""
	}
	provider := strings.ToLower(strings.TrimSpace(cfg.Compression.Provider))
	if provider == "" {
		return defaultCompressionProvider
	}
	return provider
}

func mergePurpose(current, next string) string {
	if current == next || next == "" {
		return current
	}
	if strings.Contains(current, next) {
		return current
	}
	parts := []string{current, next}
	sort.Strings(parts)
	return strings.Join(parts, " and ")
}
