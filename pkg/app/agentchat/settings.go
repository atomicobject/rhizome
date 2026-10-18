package agentchat

import (
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"strings"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

var harnessOrder = []harness.Kind{harness.KindCodex, harness.KindClaude}

func DefaultSettings() Settings {
	return normalizeSettings(Settings{})
}

func LoadSettings(env vaultconfig.Env) (Settings, error) {
	cfg, err := repoconfig.LoadCliConfig(env, true)
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

func SaveSettings(env vaultconfig.Env, settings Settings) (Settings, error) {
	settings = normalizeSettings(settings)
	cfg, err := repoconfig.LoadCliConfig(env, true)
	if err != nil {
		return Settings{}, err
	}
	cfg.Agent = &repoconfig.AgentUserConfig{
		Harness:   string(settings.Harness),
		Harnesses: make(map[string]repoconfig.AgentHarnessConfig, len(settings.Harnesses)),
	}
	for kind, value := range settings.Harnesses {
		cfg.Agent.Harnesses[string(kind)] = repoconfig.AgentHarnessConfig{
			Model: value.Model, Effort: value.Effort, PermissionMode: string(value.PermissionMode),
		}
	}
	if err := repoconfig.SaveCliConfig(env, cfg); err != nil {
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
