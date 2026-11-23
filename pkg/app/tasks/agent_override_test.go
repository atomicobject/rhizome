package tasks

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/agentchat"
	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/harnesstest"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// Per-task settings override the chat agent's harness, model, and effort
// for that task only.
func TestHarnessAgentUsesPerTaskSettings(t *testing.T) {
	claude, codex := &harnesstest.Harness{}, &harnesstest.Harness{}
	agent := HarnessAgent{
		Drivers: map[harness.Kind]harness.Harness{harness.KindClaude: claude, harness.KindCodex: codex},
		Settings: func() (agentchat.Settings, error) {
			return agentchat.Settings{Harness: harness.KindClaude, Harnesses: map[harness.Kind]agentchat.HarnessSettings{
				harness.KindClaude: {Model: "chat-claude", Effort: "medium"},
				harness.KindCodex:  {Model: "chat-codex", Effort: "low"},
			}}, nil
		},
		TaskSettings: func(task string) (repoconfig.TaskAgentConfig, error) {
			return map[string]repoconfig.TaskAgentConfig{"dream": {Harness: "codex", Model: "task-model"}}[task], nil
		},
	}
	for _, task := range []string{"dream", "other"} {
		if _, err := agent.RunPrompt(context.Background(), AgentRequest{Task: task, Dir: t.TempDir(), Prompt: "go"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(codex.StartedWith) != 1 || codex.StartedWith[0].Model != "task-model" || codex.StartedWith[0].Effort != "low" {
		t.Fatalf("dream ran with %+v", codex.StartedWith)
	}
	if len(claude.StartedWith) != 1 || claude.StartedWith[0].Model != "chat-claude" || claude.StartedWith[0].Effort != "medium" {
		t.Fatalf("other ran with %+v", claude.StartedWith)
	}
}
