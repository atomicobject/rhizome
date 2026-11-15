package desktop

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/app/agentchat"
	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/claude"
	"github.com/atomicobject/rhizome/pkg/harness/codex"
)

// ChatSettings reports each chat agent's status and whether it is on.
func (s *Service) ChatSettings(ctx context.Context) (agentchat.SettingsResponse, error) {
	harnesses := map[harness.Kind]harness.Harness{harness.KindCodex: codex.New(), harness.KindClaude: claude.New()}
	if s.chatHarnesses != nil {
		harnesses = s.chatHarnesses()
	}
	response, err := agentchat.Report(ctx, s.env, harnesses)
	if err != nil {
		return response, problem("chat_config", "Cannot read user configuration. Repair it before changing chat settings.")
	}
	return response, nil
}

// SetChatSettings turns a chat agent on or off; an agent that is off is
// never chosen for chat. The native caller holds its app-wide user-config
// lock across this operation.
func (s *Service) SetChatSettings(ctx context.Context, req Request) (agentchat.SettingsResponse, error) {
	kind := harness.Kind(req.Harness)
	if kind != harness.KindClaude && kind != harness.KindCodex || req.Enabled == nil {
		return agentchat.SettingsResponse{}, problem("invalid_request", "Turn Claude Code or Codex on or off.")
	}
	settings, err := agentchat.LoadSettings(s.env)
	if err != nil {
		return agentchat.SettingsResponse{}, problem("chat_config", "Cannot read user configuration. Repair it before changing chat settings.")
	}
	value := settings.Harnesses[kind]
	value.Disabled = !*req.Enabled
	settings.Harnesses[kind] = value
	if _, err := agentchat.SaveSettings(s.env, settings); err != nil {
		return agentchat.SettingsResponse{}, problem("chat_config", "Cannot save chat settings. Check your user configuration and file permissions.")
	}
	return s.ChatSettings(ctx)
}
