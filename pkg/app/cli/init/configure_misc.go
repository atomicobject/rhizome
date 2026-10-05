package init

import (
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// agentFlagModes turns --agents into one mode per surface. The named
// harnesses are on, the others off, and the shared AGENTS.md and
// .agents/skills follow; "none" turns everything off. An empty value
// overrides nothing.
//
// Docs: [[agent-surface-integration-modes#^SPEC-0045-US1-AC4]]
func agentFlagModes(agents string) (map[string]string, error) {
	agents = strings.ToLower(strings.TrimSpace(agents))
	if agents == "" {
		return map[string]string{}, nil
	}
	modes := map[string]string{"cursor": agentModeOff, "claude": agentModeOff, "codex": agentModeOff, "agent-skills": agentModeOn, "agentsmd": agentModeOn}
	if agents == "none" {
		for key := range modes {
			modes[key] = agentModeOff
		}
		return modes, nil
	}
	for _, name := range splitList(agents) {
		switch name {
		case "claude", "codex", "cursor":
			modes[name] = agentModeOn
		default:
			return nil, fmt.Errorf("unknown agent %q for --agents; use claude, codex, cursor, or none", name)
		}
	}
	return modes, nil
}

func applyAgentOverrides(detected AgentHarnesses, prefs *obsidian.AgentPreferences, opts RunOptions) (AgentHarnesses, *obsidian.AgentPreferences, error) {
	outPrefs := &obsidian.AgentPreferences{}
	if prefs != nil {
		*outPrefs = *prefs
	}
	modes, err := agentFlagModes(opts.Agents)
	if err != nil {
		return detected, nil, err
	}

	outPrefs.Cursor, detected.HasCursor, err = applyAgentMode("cursor", outPrefs.Cursor, modes["cursor"], detected.HasCursor)
	if err != nil {
		return detected, nil, err
	}
	outPrefs.Claude, detected.HasClaude, err = applyAgentMode("claude", outPrefs.Claude, modes["claude"], detected.HasClaude)
	if err != nil {
		return detected, nil, err
	}
	outPrefs.Codex, detected.HasCodex, err = applyAgentMode("codex", outPrefs.Codex, modes["codex"], detected.HasCodex)
	if err != nil {
		return detected, nil, err
	}
	outPrefs.AgentSkills, detected.HasAgentSkills, err = applyAgentMode("agent-skills", outPrefs.AgentSkills, modes["agent-skills"], detected.HasAgentSkills)
	if err != nil {
		return detected, nil, err
	}
	outPrefs.AgentsMd, detected.HasAgents, err = applyAgentMode("agentsmd", outPrefs.AgentsMd, modes["agentsmd"], detected.HasAgents)
	if err != nil {
		return detected, nil, err
	}
	// Track explicit disabling so we don't fall back to creating AGENTS.md
	if strings.ToLower(strings.TrimSpace(outPrefs.AgentsMd)) == agentModeOff {
		detected.AgentsMdDisabled = true
	}
	if strings.ToLower(strings.TrimSpace(outPrefs.AgentSkills)) != agentModeOff && anyAgentSurfaceEnabled(detected) {
		detected.HasAgentSkills = true
		if strings.TrimSpace(outPrefs.AgentSkills) == "" {
			outPrefs.AgentSkills = agentModeOn
		}
	}
	if !detected.AgentsMdDisabled && anyAgentSurfaceEnabled(detected) {
		detected.HasAgents = true
		if strings.TrimSpace(outPrefs.AgentsMd) == "" {
			outPrefs.AgentsMd = agentModeOn
		}
	}

	if outPrefs.Empty() {
		return detected, nil, nil
	}
	return detected, outPrefs, nil
}

func anyAgentSurfaceEnabled(h AgentHarnesses) bool {
	return h.HasCursor || h.HasClaude || h.HasCodex || h.HasAgentSkills || h.HasAgents
}

func resolveAgentMode(flagName string, mode string, detected bool) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", agentModeAuto:
		return detected, nil
	case agentModeOn:
		return true, nil
	case agentModeOff:
		return false, nil
	default:
		return detected, fmt.Errorf("invalid --%s value %q (use auto|on|off)", flagName, mode)
	}
}

func applyAgentMode(flagName, prefMode, flagMode string, detected bool) (string, bool, error) {
	mode := strings.TrimSpace(prefMode)
	current := detected

	if mode != "" {
		var err error
		current, err = resolveAgentMode(flagName, mode, current)
		if err != nil {
			return mode, detected, err
		}
	}

	// Only override the stored preference if the flag is explicitly "on" or "off".
	// "auto" (or empty) means "use stored preference or detection".
	if isExplicitAgentMode(flagMode) {
		var err error
		current, err = resolveAgentMode(flagName, flagMode, current)
		if err != nil {
			return flagMode, detected, err
		}
		mode = flagMode
	}

	return mode, current, nil
}

func isExplicitAgentMode(mode string) bool {
	mode = strings.ToLower(strings.TrimSpace(mode))
	return mode != "" && mode != agentModeAuto
}
