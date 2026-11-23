package tasks

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/app/agentchat"
	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// HarnessAgent runs prompts through the person's configured harness
// (SPEC-0101) in full-access mode, with the person's own MCP servers and
// the task's deny list as disallowed tools. Approvals are denied, because no
// person is present to answer them.
type HarnessAgent struct {
	Drivers  map[harness.Kind]harness.Harness
	Settings func() (agentchat.Settings, error)
	// TaskSettings reads the person's harness, model, and effort for one
	// task on this machine; set fields override Settings for that task only.
	TaskSettings func(task string) (repoconfig.TaskAgentConfig, error)
}

// RunPrompt runs one turn and returns the agent's final message and the
// tokens the harness reported.
func (a HarnessAgent) RunPrompt(ctx context.Context, req AgentRequest) (AgentResult, error) {
	settings, override, kind, err := a.resolve(ctx, req.Task)
	if err != nil {
		return AgentResult{}, err
	}
	var transcript *Transcript
	if req.Transcript != "" {
		if transcript, err = NewTranscript(req.Transcript); err != nil {
			return AgentResult{}, fmt.Errorf("open transcript: %w", err)
		}
		defer transcript.Close()
	}
	prefs := settings.Harnesses[kind]
	session, err := a.Drivers[kind].StartSession(ctx, harness.SessionOptions{
		PermissionMode:  harness.PermissionFullAccess,
		UserMCPConfig:   true,
		DisallowedTools: req.Deny,
		Model:           cmp.Or(override.Model, prefs.Model),
		Effort:          cmp.Or(override.Effort, prefs.Effort),
		Cwd:             req.Dir,
		Env:             req.Env,
	})
	if err != nil {
		return AgentResult{}, err
	}
	var (
		mu        sync.Mutex
		segment   strings.Builder
		final     string
		usage     harness.TokenUsage
		turnError string
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for event := range session.Events() {
			mu.Lock()
			if transcript != nil {
				transcript.Add(event)
			}
			switch event.Kind {
			case harness.EventAssistantTextDelta:
				segment.WriteString(event.Text)
			case harness.EventCommandExecution, harness.EventToolCall, harness.EventFileChange:
				// The final message is the text after the last action.
				segment.Reset()
			case harness.EventApprovalRequested:
				_ = session.Respond(event.RequestID, harness.DecisionDeny)
			case harness.EventTokenUsage:
				usage = event.Usage
			case harness.EventError:
				// A retry notice is not fatal; the harness tries again.
				if event.Status != "retrying" {
					turnError = cmp.Or(event.Text, event.Reason, "harness error")
				}
			case harness.EventTurnCompleted:
				final = event.Text
				failed := event.Phase == harness.PhaseFailed || event.Phase == harness.PhaseDeclined
				if failed && turnError == "" {
					turnError = cmp.Or(event.Reason, event.Text, "turn "+event.Phase)
				}
			}
			mu.Unlock()
		}
	}()
	turnErr := session.SendTurn(ctx, req.Prompt)
	_ = session.Stop()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if turnErr == nil && ctx.Err() != nil {
		turnErr = ctx.Err()
	}
	if turnErr == nil && turnError != "" {
		turnErr = fmt.Errorf("%s: %s", kind, turnError)
	}
	result := AgentResult{Tokens: usage.TotalTokens}
	if turnErr == nil {
		result.Message = strings.TrimSpace(final)
		if result.Message == "" {
			result.Message = strings.TrimSpace(segment.String())
		}
	}
	if transcript != nil {
		// ponytail: a transcript write failure does not fail the run; the run record still holds the outcome.
		_ = transcript.Finish(result.Message)
	}
	return result, turnErr
}

// Harness names the harness and driver a prompt run would use, without
// starting a session.
func (a HarnessAgent) Harness(ctx context.Context, task string) (harness.Kind, harness.Harness, error) {
	_, _, kind, err := a.resolve(ctx, task)
	if err != nil {
		return "", nil, err
	}
	return kind, a.Drivers[kind], nil
}

// resolve reads the chat agent settings and the task's own override, which
// wins for that task only, so prompt runs, requirement checks, and doctor
// agree on the harness.
func (a HarnessAgent) resolve(ctx context.Context, task string) (agentchat.Settings, repoconfig.TaskAgentConfig, harness.Kind, error) {
	var override repoconfig.TaskAgentConfig
	settings, err := a.Settings()
	if err != nil {
		return settings, override, "", fmt.Errorf("read agent settings: %w", err)
	}
	if a.TaskSettings != nil && task != "" {
		if override, err = a.TaskSettings(task); err != nil {
			return settings, override, "", fmt.Errorf("read task settings: %w", err)
		}
	}
	kind, err := agentchat.ResolveHarness(ctx, override.Harness, a.Drivers, func() (harness.Kind, error) { return settings.Harness, nil })
	return settings, override, kind, err
}
