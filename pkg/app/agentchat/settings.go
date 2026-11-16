package agentchat

import (
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"strings"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// harnessOrder is the order agents are probed and reported, and the order a
// new person's chat falls back through: Claude Code first, so chat starts on
// Opus when Claude Code is ready and on Sol otherwise.
var harnessOrder = []harness.Kind{harness.KindClaude, harness.KindCodex}

func DefaultSettings() Settings {
	return normalizeSettings(Settings{})
}

func LoadSettings(env vaultconfig.Env) (Settings, error) {
	cfg, err := repoconfig.LoadCliConfig(env, true)
	if err != nil {
		return Settings{}, err
	}
	return settingsFrom(cfg.Agent), nil
}

func settingsFrom(agent *repoconfig.AgentUserConfig) Settings {
	if agent == nil {
		return DefaultSettings()
	}
	settings := Settings{Harness: harness.Kind(agent.Harness), Harnesses: make(map[harness.Kind]HarnessSettings)}
	for key, value := range agent.Harnesses {
		settings.Harnesses[harness.Kind(key)] = HarnessSettings{
			Model: value.Model, Effort: value.Effort,
			PermissionMode: harness.PermissionMode(value.PermissionMode), Disabled: value.Disabled,
		}
	}
	return normalizeSettings(settings)
}

// SaveChoice stores the composer's choice: the agent in settings.Harness and
// that agent's model, effort, and permission mode. Every other stored value,
// including whether an agent is turned off, stays as it is, so a page holding
// older settings cannot undo a newer change or turn an agent back on.
func SaveChoice(env vaultconfig.Env, settings Settings) (Settings, error) {
	settings = normalizeSettings(settings)
	return updateAgent(env, func(agent *repoconfig.AgentUserConfig) {
		agent.Harness = string(settings.Harness)
		if settings.Harness == "" {
			return
		}
		chosen := settings.Harnesses[settings.Harness]
		stored := agent.Harnesses[agent.Harness]
		stored.Model, stored.Effort, stored.PermissionMode = chosen.Model, chosen.Effort, string(chosen.PermissionMode)
		agent.Harnesses[agent.Harness] = stored
	})
}

// SetHarnessDisabled turns one agent on or off for chat and changes nothing else.
func SetHarnessDisabled(env vaultconfig.Env, kind harness.Kind, disabled bool) (Settings, error) {
	return updateAgent(env, func(agent *repoconfig.AgentUserConfig) {
		stored := agent.Harnesses[string(kind)]
		stored.Disabled = disabled
		agent.Harnesses[string(kind)] = stored
	})
}

// updateAgent edits the agent section of the current user configuration
// under its file lock, so writers from other windows and processes keep each
// other's changes.
func updateAgent(env vaultconfig.Env, edit func(*repoconfig.AgentUserConfig)) (Settings, error) {
	var settings Settings
	err := repoconfig.UpdateCliConfig(env, func(cfg *repoconfig.CliConfig) error {
		if cfg.Agent == nil {
			cfg.Agent = &repoconfig.AgentUserConfig{}
		}
		if cfg.Agent.Harnesses == nil {
			cfg.Agent.Harnesses = make(map[string]repoconfig.AgentHarnessConfig)
		}
		edit(cfg.Agent)
		settings = settingsFrom(cfg.Agent)
		return nil
	})
	if err != nil {
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
	case harness.PermissionAuto:
		return harness.PermissionAuto
	case harness.PermissionApprovalRequired:
		return harness.PermissionApprovalRequired
	case harness.PermissionAutoAcceptEdits:
		return harness.PermissionAutoAcceptEdits
	case harness.PermissionFullAccess:
		return harness.PermissionFullAccess
	default:
		return harness.PermissionAuto
	}
}
