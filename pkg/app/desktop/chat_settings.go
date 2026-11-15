package desktop

import (
	"context"
	"strings"
	"unicode"

	"github.com/atomicobject/rhizome/pkg/app/agentchat"
	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/claude"
	"github.com/atomicobject/rhizome/pkg/harness/codex"
)

// ChatSettings reports which coding agent chat uses and each agent's status.
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

// SetChatSettings chooses the chat agent and, when a model is given, that
// agent's model; an empty model restores the agent's default. The native
// caller holds its app-wide user-config lock across this operation.
func (s *Service) SetChatSettings(ctx context.Context, req Request) (agentchat.SettingsResponse, error) {
	kind := harness.Kind(req.Harness)
	if kind != harness.KindClaude && kind != harness.KindCodex {
		return agentchat.SettingsResponse{}, problem("invalid_request", "Choose Claude Code or Codex.")
	}
	if req.Model != nil && (len(*req.Model) > 256 || strings.ContainsFunc(*req.Model, unicode.IsControl)) {
		return agentchat.SettingsResponse{}, problem("invalid_request", "Choose a model from the list.")
	}
	settings, err := agentchat.LoadSettings(s.env)
	if err != nil {
		return agentchat.SettingsResponse{}, problem("chat_config", "Cannot read user configuration. Repair it before changing chat settings.")
	}
	settings.Harness = kind
	if req.Model != nil {
		value := settings.Harnesses[kind]
		value.Model = *req.Model
		settings.Harnesses[kind] = value
	}
	if _, err := agentchat.SaveSettings(s.env, settings); err != nil {
		return agentchat.SettingsResponse{}, problem("chat_config", "Cannot save chat settings. Check your user configuration and file permissions.")
	}
	return s.ChatSettings(ctx)
}
