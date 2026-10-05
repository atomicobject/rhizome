package agentchat

import (
	"strings"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

var harnessOrder = []harness.Kind{harness.KindCodex, harness.KindClaude}

func DefaultSettings() Settings {
	return normalizeSettings(Settings{})
}

func LoadSettings() (Settings, error) {
	cfg, err := obsidian.LoadCliConfig(true)
	if err != nil {
		return Settings{}, err
	}
	if cfg.Agent == nil {
		return DefaultSettings(), nil
	}
	settings := Settings{Harness: harness.Kind(cfg.Agent.Harness)}
	for key, value := range cfg.Agent.Harnesses {
		if settings.Harnesses == nil {
			settings.Harnesses = make(map[harness.Kind]HarnessSettings)
		}
		settings.Harnesses[harness.Kind(key)] = HarnessSettings{
			Model: value.Model, Effort: value.Effort,
			PermissionMode: harness.PermissionMode(value.PermissionMode),
		}
	}
	return normalizeSettings(settings), nil
}

func SaveSettings(settings Settings) (Settings, error) {
	settings = normalizeSettings(settings)
	cfg, err := obsidian.LoadCliConfig(true)
	if err != nil {
		return Settings{}, err
	}
	cfg.Agent = &obsidian.AgentUserConfig{
		Harness:   string(settings.Harness),
		Harnesses: make(map[string]obsidian.AgentHarnessConfig, len(settings.Harnesses)),
	}
	for kind, value := range settings.Harnesses {
		cfg.Agent.Harnesses[string(kind)] = obsidian.AgentHarnessConfig{
			Model: value.Model, Effort: value.Effort, PermissionMode: string(value.PermissionMode),
		}
	}
	if err := obsidian.SaveCliConfig(cfg); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

func normalizeSettings(settings Settings) Settings {
	switch harness.Kind(strings.ToLower(strings.TrimSpace(string(settings.Harness)))) {
	case harness.KindCodex:
		settings.Harness = harness.KindCodex
	case harness.KindClaude:
		settings.Harness = harness.KindClaude
	default:
		settings.Harness = ""
	}

	normalized := make(map[harness.Kind]HarnessSettings, len(harnessOrder))
	for _, kind := range harnessOrder {
		value := settings.Harnesses[kind]
		value.Model = strings.TrimSpace(value.Model)
		value.Effort = strings.TrimSpace(value.Effort)
		value.PermissionMode = normalizePermissionMode(value.PermissionMode)
		normalized[kind] = value
	}
	settings.Harnesses = normalized
	return settings
}

func normalizePermissionMode(mode harness.PermissionMode) harness.PermissionMode {
	switch harness.PermissionMode(strings.ToLower(strings.TrimSpace(string(mode)))) {
	case harness.PermissionApprovalRequired:
		return harness.PermissionApprovalRequired
	case harness.PermissionAutoAcceptEdits:
		return harness.PermissionAutoAcceptEdits
	case harness.PermissionFullAccess:
		return harness.PermissionFullAccess
	default:
		return harness.PermissionApprovalRequired
	}
}
